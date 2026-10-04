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

	WaitDelay = 5 * time.Second

	LogTailBytes = 4000
)

const (
	EnvContext = "SPECD_CLM_CONTEXT"

	EnvChange = "SPECD_CLM_CHANGE"

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

func (a *Agent) Args() []string {
	return a.options.Args
}

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

func (a *Agent) Run(ctx context.Context, prompt string, extra map[string]string) (string, string, error) {
	return a.run(ctx, prompt, extra)
}

func RealizePrompt(request agent.RealizeRequest) string {
	builder := strings.Builder{}
	builder.WriteString("You change a working tree so the code matches its specification.\n\n")
	builder.WriteString("Edit the files in place. Do not ask questions and do not print a plan.\n")
	builder.WriteString("Edit files only: do not run git and do not commit. The tool commits for you.\n")
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
