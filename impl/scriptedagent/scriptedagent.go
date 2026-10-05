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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
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

type Step struct {
	Write *Write `json:"write,omitempty"`

	Patch *Patch `json:"patch,omitempty"`

	Delete *Delete `json:"delete,omitempty"`
}

// GeneratedFile is one file a scripted generation writes, relative to the
// directory the request names.
type GeneratedFile struct {
	Path string `json:"path"`

	Contents string `json:"contents"`
}

// Generation is one scripted policy authoring: the files of a policy tree, or
// the policies.yaml of a binding.
type Generation struct {
	Summary string `json:"summary,omitempty"`

	Files []GeneratedFile `json:"files,omitempty"`
}

type Scenario struct {
	Contexts map[string]Draft `json:"contexts"`

	Realize map[string][]Step `json:"realize"`

	// Policies and Bindings are the scripted generations, one entry per
	// attempt, the way Attempts carries one step list per realize.
	Policies [][]Generation `json:"policies,omitempty"`

	Bindings [][]Generation `json:"bindings,omitempty"`

	// Attempts, when a context names it, replaces Realize for that context
	// with one step list per attempt: the first realize applies the first
	// list, the second the second, and a later realize repeats the last. It is
	// how a scenario writes code a policy denies and then complies.
	Attempts map[string][][]Step `json:"attempts,omitempty"`
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

func (a *Agent) Realize(_ context.Context, request agent.RealizeRequest) (agent.RealizeResult, error) {
	if a.scenario == nil {
		return agent.RealizeResult{}, fmt.Errorf("scriptedagent: no scenario")
	}
	members := request.Batch()
	contexts := make([]string, 0, len(members))
	for _, member := range members {
		contexts = append(contexts, member.Context)
	}
	result := agent.RealizeResult{Summary: fmt.Sprintf("applied the steps of %s", strings.Join(contexts, ", "))}
	for _, member := range members {
		steps := a.stepsFor(member.Context, request.Attempt)
		if len(steps) == 0 {
			return result, fmt.Errorf("scriptedagent: the scenario has no realize steps for %s", member.Context)
		}
		for index, step := range steps {
			files, err := apply(request.Dir, step)
			if err != nil {
				return result, fmt.Errorf("scriptedagent: step %d of %s: %w", index, member.Context, err)
			}
			result.Files = append(result.Files, files...)
		}
	}
	return result, nil
}

// Generate writes the scripted policy tree of an attempt. The scenario's
// Policies section is the policy mode, Bindings the bind mode; an attempt past
// the end of the list repeats the last one, so a scenario may write a first
// attempt a check refuses and a second one that passes.
func (a *Agent) Generate(_ context.Context, request policy.GenerateRequest) (policy.GenerateResult, error) {
	if a.scenario == nil {
		return policy.GenerateResult{}, fmt.Errorf("scriptedagent: no scenario")
	}
	if request.Dir == "" {
		return policy.GenerateResult{}, fmt.Errorf("scriptedagent: the generate request names no directory")
	}
	generations := a.scenario.Policies
	if request.Mode == policy.GenerateModeBind {
		generations = a.scenario.Bindings
	}
	if len(generations) == 0 {
		return policy.GenerateResult{}, fmt.Errorf("scriptedagent: the scenario has no %s generation", request.Mode)
	}
	index := request.Attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(generations) {
		index = len(generations) - 1
	}
	result := policy.GenerateResult{}
	for _, step := range generations[index] {
		if step.Summary != "" {
			result.Summary = step.Summary
		}
		for _, file := range step.Files {
			path, err := resolve(request.Dir, file.Path)
			if err != nil {
				return result, err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return result, err
			}
			if err := os.WriteFile(path, []byte(file.Contents), 0o644); err != nil {
				return result, err
			}
			result.Files = append(result.Files, file.Path)
		}
	}
	return result, nil
}

// stepsFor picks the step list of an attempt: the per-attempt list when the
// scenario has one, else the fixed realize steps.
func (a *Agent) stepsFor(context string, attempt int) []Step {
	if byAttempt, ok := a.scenario.Attempts[context]; ok && len(byAttempt) > 0 {
		index := attempt - 1
		if index < 0 {
			index = 0
		}
		if index >= len(byAttempt) {
			index = len(byAttempt) - 1
		}
		return byAttempt[index]
	}
	return a.scenario.Realize[context]
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
