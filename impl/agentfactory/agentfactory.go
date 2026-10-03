// Package agentfactory turns one agent option string into an agent. The
// controller and the CLI both build agents this way, so `--agent scripted:x`
// means the same thing wherever it is typed.
package agentfactory

import (
	"fmt"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

const (
	Claude = "claude"

	Scripted = "scripted"
)

type Options struct {
	Kind string

	Command string

	Args []string

	Timeout time.Duration
}

// Factory holds the parsed choice. The scenario is read once, so every context
// of one run is answered from the same file.
type Factory struct {
	options Options

	scenario *scriptedagent.Scenario
}

func New(options Options) (*Factory, error) {
	factory := &Factory{options: options}
	switch {
	case options.Kind == "" || options.Kind == Claude:
		return factory, nil
	case strings.HasPrefix(options.Kind, Scripted+":"):
		file := strings.TrimPrefix(options.Kind, Scripted+":")
		if file == "" {
			return nil, fmt.Errorf("agentfactory: %s needs a scenario file", Scripted)
		}
		scenario, err := scriptedagent.Load(file)
		if err != nil {
			return nil, err
		}
		factory.options.Kind = Scripted
		factory.scenario = scenario
		return factory, nil
	}
	return nil, fmt.Errorf("agentfactory: %q is not %s or %s:<file>", options.Kind, Claude, Scripted)
}

func (f *Factory) Kind() string {
	return f.options.Kind
}

// Configured reports whether an agent can be built at all. A caller with no
// agent leaves the work for a human instead of failing it.
func (f *Factory) Configured() bool {
	return f != nil && (f.options.Kind != "" || f.options.Command != "")
}

// Agent builds the agent of one working tree. A Repository that names its own
// command overrides the default; the scripted kind is fixed, because a
// deterministic scenario must not be talked out of itself.
func (f *Factory) Agent(repository *spec.Repository, dir string) (agent.Agent, error) {
	if !f.Configured() {
		return nil, fmt.Errorf("agentfactory: no agent is configured")
	}
	if f.options.Kind == Scripted {
		return scriptedagent.New(f.scenario), nil
	}
	command := f.options.Command
	args := f.options.Args
	if repository != nil && repository.Spec.Agent != nil && repository.Spec.Agent.Command != "" {
		command = repository.Spec.Agent.Command
		args = repository.Spec.Agent.Args
	}
	return claudecli.New(claudecli.Options{
		Command: command,
		Args:    args,
		Dir:     dir,
		Timeout: f.options.Timeout,
	}), nil
}
