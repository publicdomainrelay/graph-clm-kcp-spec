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

func TestRealizePromptPutsTheDeltaFirst(t *testing.T) {
	request := agent.RealizeRequest{
		Context:    "calc",
		Repository: "calc",
		Delta: spec.Delta{
			Interfaces: []spec.InterfaceDelta{{
				Op: spec.OpAdded, Name: "Subtract",
				To: &spec.Interface{Name: "Subtract", Kind: "function", Signature: "func Subtract(a, b int) int", File: "calc/calc.go"},
			}},
		},
		ToSpec: spec.SystemContextSpec{
			Repository: "calc",
			Intent:     "Arithmetic on two integers.",
		},
		Verify:      []string{"go", "test", "./..."},
		Instruction: "the previous attempt failed",
	}
	prompt := RealizePrompt(request)

	deltaAt := strings.Index(prompt, "what changed in the specification")
	specAt := strings.Index(prompt, "the specification the code must reach")
	verifyAt := strings.Index(prompt, "go test ./...` must exit zero")
	if deltaAt < 0 || specAt < 0 || verifyAt < 0 {
		t.Fatalf("the prompt is missing a section:\n%s", prompt)
	}
	if !(deltaAt < specAt && specAt < verifyAt) {
		t.Errorf("the sections are out of order: delta %d, spec %d, verify %d", deltaAt, specAt, verifyAt)
	}
	if !strings.Contains(prompt, "+ interface Subtract (function) func Subtract(a, b int) int in calc/calc.go") {
		t.Errorf("the delta is not rendered:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Edit files only: do not run git and do not commit") {
		t.Errorf("the rule is missing:\n%s", prompt)
	}
	if !strings.Contains(prompt, "the previous attempt failed") {
		t.Errorf("the retry instruction is missing:\n%s", prompt)
	}
}

func TestBothDirectionsHandTheModelItsContainmentRoot(t *testing.T) {
	dir := t.TempDir()
	rootFile := filepath.Join(dir, "root")
	answerFile := filepath.Join(dir, "answer")
	if err := os.WriteFile(answerFile, []byte(answer), 0o644); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(dir, "model.sh")
	script := "#!/bin/sh\n" +
		"printf '%s' \"$SPECD_CLM_ROOT\" >> " + rootFile + "\n" +
		"cat > /dev/null\n" +
		"cat " + answerFile + "\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	built := New(Options{Command: command, Dir: worktree})

	if _, err := built.Summarize(context.Background(), agent.ContextBundle{
		Context: "calc", Repository: "calc",
		Spec:     spec.SystemContextSpec{Repository: "calc"},
		Observed: observed(),
		Budget:   5000,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := built.Realize(context.Background(), agent.RealizeRequest{
		Context: "calc",
		ToSpec:  spec.SystemContextSpec{Repository: "calc"},
	}); err != nil {
		t.Fatal(err)
	}
	roots, err := os.ReadFile(rootFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(roots); got != worktree+worktree {
		t.Errorf("containment root = %q, want %q twice", got, worktree)
	}
}

func TestRealizeRunsInTheGivenDirectoryAndReportsItsOutput(t *testing.T) {
	command, dir := recorder(t)
	worktree := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	built := New(Options{Command: command, Dir: worktree})
	result, err := built.Realize(context.Background(), agent.RealizeRequest{
		Context: "calc",
		ToSpec:  spec.SystemContextSpec{Repository: "calc", Intent: "Arithmetic."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Log == "" {
		t.Error("the realize result carries no log")
	}
	where, err := os.ReadFile(filepath.Join(dir, "where"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(where)) != worktree {
		t.Errorf("the model ran in %q, want the worktree %q", strings.TrimSpace(string(where)), worktree)
	}
}
