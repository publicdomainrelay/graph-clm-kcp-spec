// Package piagent is the pi host of the CLM loop: the same agent contract as
// the claude CLI, run against pi with the pi-hydradb-clm extension loaded. The
// extension is pi's own half of phase 8, so a change worked off by either host
// reports into the same context file, graph and kcp state.
//
// It composes impl/claudecli rather than repeating it: the transport (a
// headless process, the prompt on standard input, the output read strictly) is
// the same, and only the command, the arguments and the environment differ.
package piagent

import (
	"maps"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
)

const (
	// DefaultCommand is pi's headless entry point.
	DefaultCommand = "pi"

	// EnvExtension names the folder pi loads the CLM extension from.
	EnvExtension = "SPECD_PI_EXTENSION"
)

// DefaultArgs is the headless invocation. A pi build whose flags differ is
// selected with --agent-args, exactly as the claude kind is.
func DefaultArgs() []string {
	return []string{"-p"}
}

type Options struct {
	Command string

	Args []string

	Dir string

	Timeout time.Duration

	Env map[string]string

	// Extension is the pi-hydradb-clm folder the agent is told about through
	// EnvExtension, so the extension can find the state bridge.
	Extension string
}

func New(options Options) *claudecli.Agent {
	command := options.Command
	if command == "" {
		command = DefaultCommand
	}
	args := options.Args
	if len(args) == 0 {
		args = DefaultArgs()
	}
	env := map[string]string{}
	maps.Copy(env, options.Env)
	if options.Extension != "" {
		env[EnvExtension] = options.Extension
	}
	return claudecli.New(claudecli.Options{
		Command: command,
		Args:    args,
		Dir:     options.Dir,
		Timeout: options.Timeout,
		Env:     env,
	})
}
