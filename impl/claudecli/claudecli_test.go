package claudecli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const answer = `{"summary":"s","intent":"Arithmetic.","requirements":[{"id":"r.add","level":"MUST","text":"Add adds.","codeRefs":["function:abc"]}]}`

func observed() spec.ObservedFacts {
	return spec.ObservedFacts{
		Files:       []string{"calc/calc.go"},
		Interfaces:  []spec.ObservedInterface{{Name: "Add", Kind: "function", CodegraphID: "function:abc", File: "calc/calc.go", Line: 3}},
		Fingerprint: "f1",
	}
}

// recorder writes a stub model: it records its arguments, the directory it ran
// in and the prompt it read, then prints the answer.
func recorder(t *testing.T) (command, dir string) {
	t.Helper()
	dir = t.TempDir()
	argsFile := filepath.Join(dir, "args")
	promptFile := filepath.Join(dir, "prompt")
	answerFile := filepath.Join(dir, "answer")
	whereFile := filepath.Join(dir, "where")
	if err := os.WriteFile(answerFile, []byte(answer), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argsFile + "\n" +
		"pwd > " + whereFile + "\n" +
		"cat > " + promptFile + "\n" +
		"cat " + answerFile + "\n"
	command = filepath.Join(dir, "model.sh")
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return command, dir
}

func TestSummarizePassesThePromptOnStandardInputAndReadsTheAnswer(t *testing.T) {
	command, dir := recorder(t)
	work := t.TempDir()
	built := New(Options{Command: command, Dir: work, Timeout: 20 * time.Second})

	bundle := agent.ContextBundle{
		Context: "calc", Repository: "calc",
		Spec:     spec.SystemContextSpec{Repository: "calc"},
		Observed: observed(),
		Budget:   5000,
	}
	draft, err := built.Summarize(context.Background(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Intent != "Arithmetic." || len(draft.Requirements) != 1 {
		t.Fatalf("draft = %+v", draft)
	}
	if len(draft.Dropped) != 0 {
		t.Errorf("dropped = %+v", draft.Dropped)
	}

	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(args)); got != "-p\n--output-format\ntext" {
		t.Errorf("argv = %q, want the configured arguments only", got)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "one JSON object") {
		t.Errorf("the prompt did not arrive on stdin: %q", prompt)
	}
	if !strings.Contains(string(prompt), "function:abc") {
		t.Errorf("the prompt lost the observed facts")
	}
	where, err := os.ReadFile(filepath.Join(dir, "where"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(work); err == nil && strings.TrimSpace(string(where)) != resolved {
		t.Errorf("the model ran in %q, want %q", strings.TrimSpace(string(where)), work)
	}
}

func TestSummarizeReportsAFailedCommandWithItsStderrTail(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "model.sh")
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'the model exploded' >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	built := New(Options{Command: command, Timeout: 20 * time.Second})

	_, err := built.Summarize(context.Background(), agent.ContextBundle{Context: "calc"})
	if err == nil {
		t.Fatal("a failing model was accepted")
	}
	if !strings.Contains(err.Error(), "the model exploded") {
		t.Errorf("error does not carry the stderr tail: %v", err)
	}
}

func TestSummarizeGivesUpWhenTheModelIsTooSlow(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "model.sh")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	built := New(Options{Command: command, Timeout: 200 * time.Millisecond})

	started := time.Now()
	_, err := built.Summarize(context.Background(), agent.ContextBundle{Context: "calc"})
	if err == nil {
		t.Fatal("a model that never answers was accepted")
	}
	if !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Errorf("the timeout took %s", elapsed)
	}
}

func TestSummarizeRejectsAnAnswerThatDoesNotRead(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "model.sh")
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'I could not read the code.'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	built := New(Options{Command: command, Timeout: 20 * time.Second})
	if _, err := built.Summarize(context.Background(), agent.ContextBundle{Context: "calc"}); err == nil {
		t.Fatal("an unreadable answer was accepted")
	}
}

func TestDefaultsAreTheHeadlessCommand(t *testing.T) {
	built := New(Options{})
	if built.Command() != DefaultCommand {
		t.Errorf("command = %q", built.Command())
	}
	if got := strings.Join(DefaultArgs(), " "); got != "-p --output-format text" {
		t.Errorf("args = %q", got)
	}
	unstubbed := New(Options{Command: "definitely-not-a-real-model-command"})
	if _, err := unstubbed.Summarize(context.Background(), agent.ContextBundle{}); err == nil {
		t.Error("a missing command was accepted")
	}
}
