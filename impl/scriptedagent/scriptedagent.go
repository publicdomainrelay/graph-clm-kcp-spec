// Package scriptedagent is the deterministic agent. A scenario file names the
// draft of each context and the file edits a realize applies, so every test of
// the loop runs the same way twice and no test needs a model.
package scriptedagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
)

type Requirement struct {
	ID string `json:"id"`

	Level string `json:"level"`

	Text string `json:"text"`

	CodeRefs []string `json:"codeRefs,omitempty"`
}

type Interface struct {
	Name string `json:"name"`

	Kind string `json:"kind,omitempty"`

	Signature string `json:"signature,omitempty"`

	File string `json:"file,omitempty"`
}

type Draft struct {
	Summary string `json:"summary,omitempty"`

	Intent string `json:"intent"`

	Requirements []Requirement `json:"requirements,omitempty"`

	Interfaces []Interface `json:"interfaces,omitempty"`
}

type Write struct {
	Path string `json:"path"`

	Contents string `json:"contents"`
}

type Patch struct {
	Path string `json:"path"`

	Find string `json:"find"`

	Replace string `json:"replace"`
}

type Delete struct {
	Path string `json:"path"`
}

// Step is one realize edit. Exactly one of Write, Patch and Delete is set.
type Step struct {
	Write *Write `json:"write,omitempty"`

	Patch *Patch `json:"patch,omitempty"`

	Delete *Delete `json:"delete,omitempty"`
}

type Scenario struct {
	Contexts map[string]Draft `json:"contexts"`

	Realize map[string][]Step `json:"realize"`
}

func Load(path string) (*Scenario, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scriptedagent: read %s: %w", path, err)
	}
	return LoadBytes(contents)
}

func LoadBytes(contents []byte) (*Scenario, error) {
	scenario := &Scenario{}
	if err := yaml.Unmarshal(contents, scenario); err != nil {
		return nil, fmt.Errorf("scriptedagent: parse the scenario: %w", err)
	}
	return scenario, nil
}

type Agent struct {
	scenario *Scenario
}

func New(scenario *Scenario) *Agent {
	return &Agent{scenario: scenario}
}

// Summarize answers from the scenario through the same strict parser a model
// answer goes through, so a scenario cannot draft what a model could not.
func (a *Agent) Summarize(_ context.Context, bundle agent.ContextBundle) (agent.SpecDraft, error) {
	if a.scenario == nil {
		return agent.SpecDraft{}, fmt.Errorf("scriptedagent: no scenario")
	}
	draft, ok := a.scenario.Contexts[bundle.Context]
	if !ok {
		return agent.SpecDraft{}, fmt.Errorf("scriptedagent: the scenario has no draft for %s", bundle.Context)
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		return agent.SpecDraft{}, fmt.Errorf("scriptedagent: encode the draft of %s: %w", bundle.Context, err)
	}
	return agent.ParseDraft(string(encoded), bundle.Observed)
}

// Realize applies the scenario's edits to the directory the reconciler handed
// over. It touches nothing the scenario does not name.
func (a *Agent) Realize(_ context.Context, request agent.RealizeRequest) (agent.RealizeResult, error) {
	if a.scenario == nil {
		return agent.RealizeResult{}, fmt.Errorf("scriptedagent: no scenario")
	}
	steps := a.scenario.Realize[request.Context]
	if len(steps) == 0 {
		return agent.RealizeResult{}, fmt.Errorf("scriptedagent: the scenario has no realize steps for %s", request.Context)
	}
	result := agent.RealizeResult{Summary: fmt.Sprintf("applied %d step(s) for %s", len(steps), request.Context)}
	for index, step := range steps {
		files, err := apply(request.Dir, step)
		if err != nil {
			return result, fmt.Errorf("scriptedagent: step %d of %s: %w", index, request.Context, err)
		}
		result.Files = append(result.Files, files...)
	}
	return result, nil
}

func apply(dir string, step Step) ([]string, error) {
	switch {
	case step.Write != nil:
		path, err := resolve(dir, step.Write.Path)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(step.Write.Contents), 0o644); err != nil {
			return nil, err
		}
		return []string{step.Write.Path}, nil
	case step.Patch != nil:
		path, err := resolve(dir, step.Patch.Path)
		if err != nil {
			return nil, err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(contents), step.Patch.Find) {
			return nil, fmt.Errorf("patch target %q is not in %s", step.Patch.Find, step.Patch.Path)
		}
		patched := strings.ReplaceAll(string(contents), step.Patch.Find, step.Patch.Replace)
		if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
			return nil, err
		}
		return []string{step.Patch.Path}, nil
	case step.Delete != nil:
		path, err := resolve(dir, step.Delete.Path)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		return []string{step.Delete.Path}, nil
	}
	return nil, fmt.Errorf("a step is a write, a patch or a delete")
}

func resolve(dir, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("a step needs a path")
	}
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("%q is outside the working tree", path)
	}
	return filepath.Join(dir, cleaned), nil
}
