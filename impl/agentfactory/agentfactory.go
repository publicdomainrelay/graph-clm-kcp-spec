package agentfactory

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/piagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

const (
	Claude = "claude"

	Scripted = "scripted"

	Pi = "pi"

	ClaudeMod = "claude-mod"
)

type Options struct {
	Kind string

	Command string

	Args []string

	Timeout time.Duration

	ClmMod string

	PiExtension string

	Env map[string]string
}

type Factory struct {
	options Options

	scenario *scriptedagent.Scenario

	scenarioFile string
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
		if err := factory.useScenario(file); err != nil {
			return nil, err
		}
		return factory, nil
	case options.Kind == Pi:
		return factory, nil
	case options.Kind == ClaudeMod:
		if err := factory.checkMod(); err != nil {
			return nil, err
		}
		return factory, nil
	}
	return nil, fmt.Errorf("agentfactory: %q is not %s, %s, %s or %s:<file>",
		options.Kind, Claude, ClaudeMod, Pi, Scripted)
}

func (f *Factory) checkMod() error {
	if f.options.ClmMod == "" {
		return fmt.Errorf("agentfactory: %s needs --clm-mod pointing at the cc-clm-mod folder", ClaudeMod)
	}
	if _, err := os.Stat(filepath.Join(f.options.ClmMod, ".claude-plugin", "plugin.json")); err != nil {
		return fmt.Errorf("agentfactory: %s is not a Claude Code plugin folder: %w", f.options.ClmMod, err)
	}
	return nil
}

func (f *Factory) ClmModFolder() string {
	if f == nil {
		return ""
	}
	return f.options.ClmMod
}

func (f *Factory) useScenario(file string) error {
	scenario, err := scriptedagent.Load(file)
	if err != nil {
		return err
	}
	f.options.Kind = Scripted
	f.scenario = scenario
	f.scenarioFile = file
	return nil
}

func (f *Factory) Kind() string {
	return f.options.Kind
}

func (f *Factory) Configured() bool {
	return f != nil && (f.options.Kind != "" || f.options.Command != "")
}

func (f *Factory) KindFor(repository *spec.Repository) string {
	if repository != nil && repository.Spec.Agent != nil && repository.Spec.Agent.Kind != "" {
		return repository.Spec.Agent.Kind
	}
	if f == nil {
		return ""
	}
	if f.options.Kind == Scripted && f.scenarioFile != "" {
		return Scripted + ":" + f.scenarioFile
	}
	return f.options.Kind
}

func (f *Factory) ConfiguredFor(repository *spec.Repository) bool {
	if f == nil {
		return false
	}
	if f.Configured() {
		return true
	}
	return f.KindFor(repository) != ""
}

// ModelCoverageFor reports whether this repository's agent is a model this
// controller can also ask a separate coverage question, one call per change.
func (f *Factory) ModelCoverageFor(repository *spec.Repository) bool {
	if f == nil {
		return false
	}
	switch f.KindFor(repository) {
	case "", Claude, ClaudeMod:
		return true
	}
	return false
}

func (f *Factory) PopulateConfiguredFor(repository *spec.Repository) bool {
	if f == nil {
		return false
	}
	if f.Configured() {
		return true
	}
	if agent := repository.PopulateAgent(); agent != nil && agent.Kind != "" {
		return true
	}
	return f.KindFor(repository) != ""
}

func (f *Factory) AgentFor(selection *spec.AgentSpec, repository *spec.Repository, dir string) (agent.Agent, error) {
	if selection == nil {
		return f.Agent(repository, dir)
	}
	scoped := spec.Repository{}
	if repository != nil {
		scoped = *repository
	}
	scoped.Spec.Agent = selection
	return f.Agent(&scoped, dir)
}

func (f *Factory) Agent(repository *spec.Repository, dir string) (agent.Agent, error) {
	if f == nil {
		return nil, fmt.Errorf("agentfactory: no agent is configured")
	}
	kind := f.KindFor(repository)
	command := f.options.Command
	args := f.options.Args
	if repository != nil && repository.Spec.Agent != nil && repository.Spec.Agent.Command != "" {
		command = repository.Spec.Agent.Command
		args = repository.Spec.Agent.Args
	}

	switch {
	case strings.HasPrefix(kind, Scripted+":"):
		file := strings.TrimPrefix(kind, Scripted+":")
		if file == "" {
			return nil, fmt.Errorf("agentfactory: %s needs a scenario file", Scripted)
		}
		if f.scenario != nil && f.scenarioFile == file {
			return scriptedagent.New(f.scenario), nil
		}
		scenario, err := scriptedagent.Load(file)
		if err != nil {
			return nil, err
		}
		return scriptedagent.New(scenario), nil
	case kind == Pi:
		if len(args) == 0 {
			args = piagent.DefaultArgs()
		}
		extension := absolute(f.options.PiExtension)
		return piagent.New(piagent.Options{
			Command:   command,
			Args:      piArgs(args, extension),
			Dir:       dir,
			Timeout:   f.options.Timeout,
			Env:       f.options.Env,
			Extension: extension,
		}), nil
	case kind == ClaudeMod:
		if err := f.checkMod(); err != nil {
			return nil, err
		}
		if len(args) == 0 {
			args = claudecli.DefaultArgs()
		}
		return claudecli.New(claudecli.Options{
			Command: command,
			Args:    modArgs(args, absolute(f.options.ClmMod)),
			Dir:     dir,
			Timeout: f.options.Timeout,
			Env:     f.options.Env,
		}), nil
	case kind == "" || kind == Claude:
	default:
		return nil, fmt.Errorf("agentfactory: %q is not %s, %s, %s or %s:<file>",
			kind, Claude, ClaudeMod, Pi, Scripted)
	}

	if kind == "" && command == "" {
		return nil, fmt.Errorf("agentfactory: no agent is configured")
	}
	return claudecli.New(claudecli.Options{
		Command: command,
		Args:    args,
		Dir:     dir,
		Timeout: f.options.Timeout,
		Env:     f.options.Env,
	}), nil
}

func absolute(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if resolved, err := filepath.Abs(path); err == nil {
		return resolved
	}
	return path
}

func modArgs(args []string, folder string) []string {
	out := append([]string{}, args...)
	if slices.Contains(out, "--plugin-dir") {
		return out
	}
	return append(out, "--plugin-dir", folder)
}

func piArgs(args []string, folder string) []string {
	if folder == "" {
		return args
	}
	out := append([]string{}, args...)
	if slices.Contains(out, "--extension") || slices.Contains(out, "-e") {
		return out
	}
	return append(out, "--extension", folder)
}
