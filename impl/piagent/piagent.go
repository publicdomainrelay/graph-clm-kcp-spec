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
	"bufio"
	"maps"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
)

const (
	// DefaultCommand runs pi from the npm package. The pi coding agent is not
	// installed globally on every machine this repository is used from, so the
	// default resolves it the way the project's own documentation does; a host
	// with a pi on PATH names it with --agent-command.
	DefaultCommand = "npx"

	// Package is the npm package the default command runs.
	Package = "@earendil-works/pi-coding-agent@1.0.0"

	// DefaultProvider and DefaultModel are the hosted provider every number in
	// docs/eval is taken with. A local model is not used anywhere in this
	// repository: a measurement taken from one describes the model, not the
	// loop, and cannot be compared with the runs recorded beside it.
	DefaultProvider = "deepseek"

	DefaultModel = "deepseek-flash"

	// EnvAPIKey is the credential the provider reads. It is passed to the child
	// and never logged.
	EnvAPIKey = "DEEPSEEK_API_KEY"

	// Launcher is the wrapper that carries the same credential for the claude
	// host, so an environment that runs one host can run the other without
	// exporting the key a second time.
	Launcher = "deepseek-claude"

	// EnvExtension names the folder pi loads the CLM extension from.
	EnvExtension = "SPECD_PI_EXTENSION"
)

// DefaultArgs is the headless invocation: the npm package, the hosted provider
// and model, no session to leave behind, no extension discovery beyond the one
// the caller names, and non-interactive mode. A pi build whose flags differ is
// selected with --agent-args.
func DefaultArgs() []string {
	return []string{
		"--yes", Package,
		"--provider", DefaultProvider,
		"--model", DefaultModel,
		"--no-session",
		"-ne",
		"-p",
	}
}

// APIKeyFromLauncher reads the provider credential out of the launcher script
// that carries it for the claude host. An environment with no launcher, or with
// a launcher that names no key, answers empty and the child runs with whatever
// credential the caller exported. The value is a secret: it is handed to the
// child and never printed.
func APIKeyFromLauncher() string {
	path, err := exec.LookPath(Launcher)
	if err != nil {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !found || strings.TrimPrefix(name, "export ") != "ANTHROPIC_API_KEY" {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return ""
	}
	return ""
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
	if _, given := env[EnvAPIKey]; !given && os.Getenv(EnvAPIKey) == "" {
		if key := APIKeyFromLauncher(); key != "" {
			env[EnvAPIKey] = key
		}
	}
	return claudecli.New(claudecli.Options{
		Command: command,
		Args:    args,
		Dir:     options.Dir,
		Timeout: options.Timeout,
		Env:     env,
	})
}
