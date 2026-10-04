// Package claudecli is the agent that shells out to a headless model CLI. The
// prompt goes on standard input, never in the argument list: the launcher word
// splits its argv, so a prompt passed as an argument would arrive as a handful
// of words.
package claudecli

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"

	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
)

const (
	DefaultCommand = "deepseek-claude"

	DefaultTimeout = 3 * time.Minute

	// WaitDelay is the grace after the timeout or a cancel, before the output
	// pipes of a killed model are closed by force.
	WaitDelay = 5 * time.Second

	LogTailBytes = 4000
)

// The environment a host running inside the model reads to find the state it
// must report into. They are set on every call, so a plain model ignores them
// and a mod or an extension uses them.
const (
	EnvContext = "SPECD_CLM_CONTEXT"

	EnvChange = "SPECD_CLM_CHANGE"

	// EnvRoot is the worktree the model may work in. It is the same directory
	// the model runs in, so a host inside the model can hold every path a tool
	// names against it and refuse the ones that land outside. specd sets it on
	// both the summarize and the realize call.
	EnvRoot = "SPECD_CLM_ROOT"

	EnvRepository = "SPECD_CLM_REPOSITORY"

	EnvDoc = "SPECD_CLM_DOC"
)

func docEnv(repository, context string) map[string]string {
	if repository == "" || context == "" {
		return map[string]string{}
	}
	return map[string]string{
		EnvRepository: repository,
		EnvDoc:        agent.ContextDocPath(statedir.ClmDocDir(), repository, context),
	}
}

func DefaultArgs() []string {
	return []string{"-p", "--output-format", "text"}
}

type Options struct {
	Command string

	Args []string

	Dir string

	Timeout time.Duration

	// Env is set over the process environment of every call: the workspace
	// kubeconfig, the specctl path and the bolt endpoint a host inside the model
	// needs to report into the same state the controllers watch.
	Env map[string]string
}

type Agent struct {
	options Options
}

func New(options Options) *Agent {
	if options.Command == "" {
		options.Command = DefaultCommand
	}
	if len(options.Args) == 0 {
		options.Args = DefaultArgs()
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}
	return &Agent{options: options}
}

func (a *Agent) Command() string {
	return a.options.Command
}

// Summarize asks the model for the JSON contract and reads the answer strictly.
// A model that wraps the object in a fence is tolerated; a model that answers
// with an unknown level is not.
func (a *Agent) Summarize(ctx context.Context, bundle agent.ContextBundle) (agent.SpecDraft, error) {
	prompt, _ := agent.RenderPromptWithSections(bundle)
	env := docEnv(bundle.Repository, bundle.Context)
	env[EnvContext] = bundle.Context
	env[EnvRoot] = a.options.Dir
	stdout, stderr, err := a.run(ctx, prompt, env)
	if err != nil {
		return agent.SpecDraft{}, fmt.Errorf("claudecli: summarize %s: %w: %s", bundle.Context, err, tail(stderr))
	}
	draft, err := agent.ParseDraft(stdout, bundle.Observed)
	if err != nil {
		return agent.SpecDraft{}, fmt.Errorf("claudecli: summarize %s: %w: %s", bundle.Context, err, tail(stdout))
	}
	return draft, nil
}

// Realize runs the model inside the worktree so it can edit files in place, and
// reports its output. Deciding what the model actually changed is the
// reconciler's job: it owns the git worktree.
func (a *Agent) Realize(ctx context.Context, request agent.RealizeRequest) (agent.RealizeResult, error) {
	prompt := RealizePrompt(request)
	env := docEnv(request.Repository, request.Context)
	env[EnvContext] = request.Context
	env[EnvChange] = request.Change
	env[EnvRoot] = a.options.Dir
	stdout, stderr, err := a.run(ctx, prompt, env)
	result := agent.RealizeResult{Summary: firstLine(stdout), Log: tail(stdout + "\n" + stderr)}
	if err != nil {
		return result, fmt.Errorf("claudecli: realize %s: %w", request.Context, err)
	}
	return result, nil
}

// Run drives the model with a prompt the caller owns. The summarize and realize
// contracts cover the loop's two directions; the CLM path needs a third ask
// ("change the specification by editing the context document"), and a host
// inside the model applies it. Everything about the transport is the same, so
// this is the same process with a different prompt.
func (a *Agent) Run(ctx context.Context, prompt string, extra map[string]string) (string, string, error) {
	return a.run(ctx, prompt, extra)
}

// RealizePrompt is the ask for the other direction. The delta comes first: the
// agent is told what changed in the specification, then the spec the code must
// reach, then the command that decides whether the change is accepted. The
// rules are the contract with the reconciler, which owns the worktree and the
// commit.
func RealizePrompt(request agent.RealizeRequest) string {
	builder := strings.Builder{}
	builder.WriteString("You change a working tree so the code matches its specification.\n\n")
	builder.WriteString("Edit the files in place. Do not ask questions and do not print a plan.\n")
	builder.WriteString("Edit files only: do not run git and do not commit. The tool commits for you.\n")
	// A host inside the model injects the context document, whose spec block is
	// the specification being realized. Rewriting it during the realize would
	// change the target while the code is being brought to it, so the ask names
	// it: the model changes code, the tool owns the spec.
	builder.WriteString("Do not rewrite the spec block of the context document; it is the specification you are implementing.\n\n")

	builder.WriteString("## what changed in the specification\n\n")
	builder.WriteString(agent.RenderDelta(request.Delta))
	builder.WriteString("\n## the specification the code must reach\n\n")
	target, _ := agent.RenderPromptWithSections(agent.ContextBundle{
		Context:  request.Context,
		Spec:     request.ToSpec,
		Observed: request.Observed,
		Budget:   request.Budget,
	})
	builder.WriteString(target)

	if len(request.Verify) > 0 {
		builder.WriteString("\n## verify\n\n")
		fmt.Fprintf(&builder, "`%s` must exit zero before this change is accepted.\n", strings.Join(request.Verify, " "))
	}
	if request.Instruction != "" {
		fmt.Fprintf(&builder, "\n## instruction\n\n%s\n", request.Instruction)
	}
	return builder.String()
}

func (a *Agent) run(ctx context.Context, prompt string, extra map[string]string) (string, string, error) {
	runCtx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()

	command := exec.CommandContext(runCtx, a.options.Command, a.options.Args...)
	command.Dir = a.options.Dir
	command.Env = environ(a.options.Env, extra)
	command.Stdin = strings.NewReader(prompt)
	// A cancelled command is killed, but a child it spawned can hold the output
	// pipes open; WaitDelay bounds that wait instead of hanging forever.
	command.WaitDelay = WaitDelay
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr

	err := command.Run()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return stdout.String(), stderr.String(), fmt.Errorf("the model did not answer within %s", a.options.Timeout)
		}
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

// environ is the process environment with the agent's own variables over it.
// A name is replaced, never appended twice, because which of two entries a
// child reads is not defined. No override at all means the process environment
// is inherited as it stands.
func environ(base, extra map[string]string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	overrides := make(map[string]string, len(base)+len(extra))
	for name, value := range base {
		overrides[name] = value
	}
	for name, value := range extra {
		if value == "" {
			continue
		}
		overrides[name] = value
	}
	out := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if _, replaced := overrides[name]; found && replaced {
			continue
		}
		out = append(out, entry)
	}
	for name, value := range overrides {
		out = append(out, name+"="+value)
	}
	return out
}

func tail(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= LogTailBytes {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-LogTailBytes:]
}

func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.Index(trimmed, "\n"); index >= 0 {
		trimmed = trimmed[:index]
	}
	if len(trimmed) > 200 {
		return trimmed[:200]
	}
	return trimmed
}
