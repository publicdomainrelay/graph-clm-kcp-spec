package coverage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
)

const (
	DefaultTimeout = 3 * time.Minute

	WaitDelay = 5 * time.Second

	DiffLimit = 60000
)

const Rubric = `You check whether a code diff implements each requirement of a spec change.

For every requirement, answer implemented true only when the diff contains the
behaviour the requirement describes. A requirement whose behaviour is absent, or
present only in a comment or a test that cannot pass, is false. Judge the diff,
not the requirement's wording.

Answer only with a JSON object of this exact shape, one entry per requirement:

{"verdicts":[{"id":"<requirement id>","implemented":true,"evidence":"the lines of the diff that do it, or why none do"}]}

Answer for every requirement exactly once, in the order given.
`

type Requirement struct {
	ID string `json:"id"`

	Level string `json:"level,omitempty"`

	Text string `json:"text,omitempty"`

	Op string `json:"op,omitempty"`
}

type Verdict struct {
	ID string `json:"id"`

	Implemented bool `json:"implemented"`

	Evidence string `json:"evidence,omitempty"`
}

type Request struct {
	Context string

	Change string

	Requirements []Requirement

	Diff string
}

type Options struct {
	Command string

	Args []string

	Dir string

	Timeout time.Duration

	Env map[string]string
}

type Judge struct {
	options Options
}

func New(options Options) *Judge {
	if options.Command == "" {
		options.Command = claudecli.DefaultCommand
	}
	if len(options.Args) == 0 {
		options.Args = claudecli.DefaultArgs()
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}
	return &Judge{options: options}
}

func Requirements(change spec.Delta) []Requirement {
	out := []Requirement{}
	for _, entry := range change.Requirements {
		if entry.Op != spec.OpAdded && entry.Op != spec.OpChanged {
			continue
		}
		if entry.To == nil {
			continue
		}
		out = append(out, Requirement{
			ID:    entry.To.ID,
			Level: string(entry.To.Level),
			Text:  entry.To.Text,
			Op:    entry.Op,
		})
	}
	sort.Slice(out, func(left, right int) bool { return out[left].ID < out[right].ID })
	return out
}

func Missing(verdicts []Verdict) []Verdict {
	out := []Verdict{}
	for _, verdict := range verdicts {
		if !verdict.Implemented {
			out = append(out, verdict)
		}
	}
	return out
}

func (j *Judge) Cover(ctx context.Context, request Request) ([]Verdict, error) {
	if len(request.Requirements) == 0 {
		return nil, nil
	}
	stdout, stderr, err := j.run(ctx, Prompt(request))
	if err != nil {
		return nil, fmt.Errorf("coverage: %s: %w: %s", request.Change, err, tail(stderr))
	}
	verdicts, err := ParseVerdicts(stdout)
	if err != nil {
		return nil, fmt.Errorf("coverage: %s: %w: %s", request.Change, err, tail(stdout))
	}
	return verdicts, nil
}

func Prompt(request Request) string {
	builder := &strings.Builder{}
	builder.WriteString(Rubric)
	builder.WriteString("\n## requirements\n\n")
	for _, requirement := range request.Requirements {
		fmt.Fprintf(builder, "- id: %s\n  level: %s\n  requirement: %s\n", requirement.ID, requirement.Level, requirement.Text)
	}
	builder.WriteString("\n## diff\n\n")
	builder.WriteString(Truncate(request.Diff))
	builder.WriteString("\n")
	return builder.String()
}

// Truncate bounds the diff so one huge change cannot blow the model's context;
// the head and the tail are kept, because a diff's new files are usually at the
// top and the last edits at the bottom.
func Truncate(diff string) string {
	if len(diff) <= DiffLimit {
		return diff
	}
	half := DiffLimit / 2
	return diff[:half] + "\n... (diff truncated) ...\n" + diff[len(diff)-half:]
}

type answer struct {
	Verdicts []struct {
		ID string `json:"id"`

		Implemented bool `json:"implemented"`

		Evidence string `json:"evidence"`
	} `json:"verdicts"`
}

func ParseVerdicts(raw string) ([]Verdict, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, errors.New("the judge answered no JSON object")
	}
	parsed := answer{}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, fmt.Errorf("the judge's JSON did not parse: %w", err)
	}
	verdicts := make([]Verdict, 0, len(parsed.Verdicts))
	for _, entry := range parsed.Verdicts {
		verdicts = append(verdicts, Verdict{ID: entry.ID, Implemented: entry.Implemented, Evidence: entry.Evidence})
	}
	return verdicts, nil
}

func (j *Judge) run(ctx context.Context, prompt string) (string, string, error) {
	runCtx, cancel := context.WithTimeout(ctx, j.options.Timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, j.options.Command, j.options.Args...)
	command.Dir = j.options.Dir
	command.Env = append(os.Environ(), pairs(j.options.Env)...)
	command.Stdin = strings.NewReader(prompt)
	command.WaitDelay = WaitDelay
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return stdout.String(), stderr.String(), fmt.Errorf("the judge did not answer within %s", j.options.Timeout)
		}
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

func pairs(extra map[string]string) []string {
	out := make([]string, 0, len(extra))
	for name, value := range extra {
		out = append(out, name+"="+value)
	}
	sort.Strings(out)
	return out
}

func tail(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= 2000 {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-2000:]
}
