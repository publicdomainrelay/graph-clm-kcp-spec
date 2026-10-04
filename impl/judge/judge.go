// Package judge asks a model whether a specification states a list of
// behavioural facts, and reads the answer strictly. It is the code -> spec
// grade: the interface list is handed to the model, so the only measurement
// worth taking is whether the spec says what the code does.
//
// The judge runs its own command without the mod or the extension loaded: the
// grading call must not be able to change the state it is grading.
package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
)

const (
	DefaultTimeout = 3 * time.Minute

	WaitDelay = 5 * time.Second

	LogTailBytes = 2000
)

// Rubric is the fixed instruction every judge call gets. It is fixed on
// purpose: a rubric that moved with the run would move the score with it.
const Rubric = `You grade a specification against a list of behavioural facts.

A fact counts as stated only when the specification's own words say it. A fact
that is merely implied by an interface name, or that a reader could infer from
the code, does not count.

Answer only with a JSON object of this exact shape, one entry per fact:

{"verdicts":[{"id":"<fact id>","stated":true,"evidence":"the words from the specification that state it"}]}

Answer for every fact exactly once, in the order the facts are given.
`

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

// Judge asks the model for one verdict per fact and refuses an answer that does
// not name every fact, because a missing verdict silently grades as a miss.
func (j *Judge) Judge(ctx context.Context, request eval.JudgeRequest) (eval.JudgeResult, error) {
	result := eval.JudgeResult{}
	if len(request.Facts) == 0 {
		return result, nil
	}
	stdout, stderr, err := j.run(ctx, Prompt(request))
	result.Raw = stdout
	if err != nil {
		return result, fmt.Errorf("judge: %s: %w: %s", request.Context, err, tail(stderr))
	}
	verdicts, err := parseVerdicts(stdout)
	if err != nil {
		return result, fmt.Errorf("judge: %s: %w: %s", request.Context, err, tail(stdout))
	}
	result.Verdicts = verdicts
	return result, nil
}

// Prompt is the fixed ask: the rubric, the facts, then the specification's
// prose. Only the prose is graded, so the interface list never reaches the
// judge and cannot be echoed back as an answer.
func Prompt(request eval.JudgeRequest) string {
	builder := &strings.Builder{}
	builder.WriteString(Rubric)
	builder.WriteString("\n## facts\n\n")
	for _, fact := range request.Facts {
		fmt.Fprintf(builder, "- id: %s\n  fact: %s\n", fact.ID, fact.Text)
	}
	builder.WriteString("\n## specification\n\n")
	builder.WriteString(request.Spec)
	builder.WriteString("\n")
	return builder.String()
}

type answer struct {
	Verdicts []struct {
		ID string `json:"id"`

		Stated bool `json:"stated"`

		Evidence string `json:"evidence"`
	} `json:"verdicts"`
}

// parseVerdicts reads the JSON object a judge answered with, tolerating the
// prose and the fences a model wraps it in.
func parseVerdicts(raw string) ([]eval.JudgeVerdict, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, errors.New("the judge answered no JSON object")
	}
	parsed := answer{}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, fmt.Errorf("the judge's JSON did not parse: %w", err)
	}
	verdicts := make([]eval.JudgeVerdict, 0, len(parsed.Verdicts))
	for _, entry := range parsed.Verdicts {
		verdicts = append(verdicts, eval.JudgeVerdict{ID: entry.ID, Stated: entry.Stated, Evidence: entry.Evidence})
	}
	return verdicts, nil
}

func (j *Judge) run(ctx context.Context, prompt string) (string, string, error) {
	runCtx, cancel := context.WithTimeout(ctx, j.options.Timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, j.options.Command, j.options.Args...)
	command.Dir = j.options.Dir
	command.Env = environ(j.options.Env)
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

func environ(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}
	out := os.Environ()
	for name, value := range extra {
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
