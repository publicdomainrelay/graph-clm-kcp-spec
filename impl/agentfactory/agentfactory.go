// Package agentfactory turns one agent option string into an agent. The
// controller and the CLI both build agents this way, so `--agent scripted:x`
// means the same thing wherever it is typed.
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

	// Pi runs the deepseek-claude CLI with the pi host's CLM extension, the
	// other half of phase 8: the same agent contract, the same state bridge.
	Pi = "pi"

	// ClaudeMod runs the model with the Claude Code mod loaded, so the agent
	// inside the model reports into the context file, the graph and kcp while it
	// works. It is what makes the alignment observable while it happens.
	ClaudeMod = "claude-mod"
)

type Options struct {
	Kind string

	Command string

	Args []string

	Timeout time.Duration

	// ClmMod is the plugin folder the claude-mod kind loads with --plugin-dir.
	ClmMod string

	// PiExtension is the pi-hydradb-clm folder the pi kind tells the agent
	// about through SPECD_PI_EXTENSION.
	PiExtension string

	// Env is set over the process environment of every model call.
	Env map[string]string
}

// Factory holds the parsed choice. The scenario is read once, so every context
// of one run is answered from the same file.
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

// checkMod refuses a claude-mod selection with no usable plugin folder at once,
// not when a change is already running: a typo would otherwise leave every
// change Failed for a reason nobody reads.
func (f *Factory) checkMod() error {
	if f.options.ClmMod == "" {
		return fmt.Errorf("agentfactory: %s needs --clm-mod pointing at the cc-clm-mod folder", ClaudeMod)
	}
	if _, err := os.Stat(filepath.Join(f.options.ClmMod, ".claude-plugin", "plugin.json")); err != nil {
		return fmt.Errorf("agentfactory: %s is not a Claude Code plugin folder: %w", f.options.ClmMod, err)
	}
	return nil
}

// ClmModFolder is the plugin folder the claude-mod kind loads, or "".
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

// Configured reports whether the controller itself names an agent. A caller
// with no agent leaves the work for a human instead of failing it.
func (f *Factory) Configured() bool {
	return f != nil && (f.options.Kind != "" || f.options.Command != "")
}

// KindFor is the agent kind one Repository selects: its own spec.agent.kind
// when it names one, otherwise the controller's --agent.
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

// ConfiguredFor reports whether an agent can be built for one Repository. A
// Repository that names its own kind makes the controller work, even when
// specd was started without --agent.
func (f *Factory) ConfiguredFor(repository *spec.Repository) bool {
	if f == nil {
		return false
	}
	if f.Configured() {
		return true
	}
	return f.KindFor(repository) != ""
}

// PopulateConfiguredFor reports whether the populate step of one Repository has
// an agent at all: the controller's --agent, the repository's spec.agent, or
// the populate step's own agent.
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

// AgentFor builds an agent with an explicit selection: the populate step names
// its own agent, which wins over spec.agent, which wins over the controller's
// --agent. A nil selection is the repository's own choice.
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

// Agent builds the agent of one working tree. A Repository that names its own
// kind wins over the controller's, and one that names a command overrides the
// model command. The scripted kind answers from the file the kind names, so a
// deterministic scenario cannot be talked out of itself.
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
			// The mod's arguments are the headless ones plus the plugin folder;
			// claudecli fills its own defaults only when it is given none.
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

// absolute names a folder the way the model's own working directory can be
// relied on to reach it. The model runs in the working tree and not in the
// directory the caller typed the flag in, so a folder left relative would be
// looked for inside the tree: the failure reads as a missing extension rather
// than as a path the caller got wrong.
func absolute(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if resolved, err := filepath.Abs(path); err == nil {
		return resolved
	}
	return path
}

// modArgs adds the plugin folder to the model arguments, unless the caller
// already named one.
func modArgs(args []string, folder string) []string {
	out := append([]string{}, args...)
	if slices.Contains(out, "--plugin-dir") {
		return out
	}
	return append(out, "--plugin-dir", folder)
}

// piArgs adds the extension folder to the model arguments, unless the caller
// already named one. pi discovers an extension from a settings file, not from
// the folder holding it, so a controller that was given one has to hand it to
// the command as well: without this the CLM path would ask the model to edit a
// document nothing applies, and the scenario would fail for the harness's
// reason rather than the model's.
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
