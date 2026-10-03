// Package claudecli is the agent that shells out to a headless model CLI. The
// prompt goes on standard input, never in the argument list: the launcher word
// splits its argv, so a prompt passed as an argument would arrive as a handful
// of words.
package claudecli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

func DefaultArgs() []string {
	return []string{"-p", "--output-format", "text"}
}

type Options struct {
	Command string

	Args []string

	Dir string

	Timeout time.Duration
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
	stdout, stderr, err := a.run(ctx, prompt)
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
	stdout, stderr, err := a.run(ctx, prompt)
	result := agent.RealizeResult{Summary: firstLine(stdout), Log: tail(stdout + "\n" + stderr)}
	if err != nil {
		return result, fmt.Errorf("claudecli: realize %s: %w", request.Context, err)
	}
	return result, nil
}

// RealizePrompt is the ask for the other direction: the spec the code must
// reach, the spec it starts from, and the instruction to edit in place.
func RealizePrompt(request agent.RealizeRequest) string {
	builder := strings.Builder{}
	builder.WriteString("You change a working tree so the code matches its specification.\n\n")
	builder.WriteString("Edit the files in place. Do not ask questions and do not print a plan.\n")
	builder.WriteString("Run the repository's tests when you are done.\n\n")
	builder.WriteString("## target spec\n\n")
	target, _ := agent.RenderPromptWithSections(agent.ContextBundle{
		Context:  request.Context,
		Spec:     request.ToSpec,
		Observed: request.Observed,
		Budget:   request.Budget,
	})
	builder.WriteString(target)
	if request.Instruction != "" {
		fmt.Fprintf(&builder, "\n## instruction\n\n%s\n", request.Instruction)
	}
	return builder.String()
}

func (a *Agent) run(ctx context.Context, prompt string) (string, string, error) {
	runCtx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()

	command := exec.CommandContext(runCtx, a.options.Command, a.options.Args...)
	command.Dir = a.options.Dir
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
