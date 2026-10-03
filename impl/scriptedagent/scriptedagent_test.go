package scriptedagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const scenario = `
contexts:
  calc:
    summary: The calc package does arithmetic on two integers.
    intent: Arithmetic over two integers.
    requirements:
      - id: r.add
        level: MUST
        text: Add adds two integers.
        codeRefs: ["function:abc"]
      - id: r.invented
        level: MAY
        text: Something the facts do not carry.
        codeRefs: ["function:not-there"]
    interfaces:
      - name: Add
        kind: function
        signature: "func Add(a, b int) int"
        file: calc/calc.go
realize:
  calc:
    - write:
        path: calc/calc.go
        contents: |
          package calc

          func Add(a, b int) int { return a + b }
    - patch:
        path: notes.txt
        find: BEFORE
        replace: AFTER
`

func load(t *testing.T) *Agent {
	t.Helper()
	loaded, err := LoadBytes([]byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	return New(loaded)
}

func observed() spec.ObservedFacts {
	return spec.ObservedFacts{
		Files: []string{"calc/calc.go"},
		Interfaces: []spec.ObservedInterface{
			{Name: "Add", Kind: "function", CodegraphID: "function:abc", File: "calc/calc.go", Line: 3},
		},
		Fingerprint: "f1",
	}
}

func TestSummarizeAnswersThroughTheStrictParser(t *testing.T) {
	draft, err := load(t).Summarize(context.Background(), agent.ContextBundle{Context: "calc", Observed: observed()})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Intent != "Arithmetic over two integers." || draft.Summary == "" {
		t.Errorf("draft = %+v", draft)
	}
	if len(draft.Requirements) != 2 || len(draft.Interfaces) != 1 {
		t.Fatalf("draft = %+v", draft)
	}
	if len(draft.Dropped) != 1 || draft.Dropped[0].Ref != "function:not-there" {
		t.Errorf("dropped = %+v, want the ref the facts do not carry", draft.Dropped)
	}
}

func TestSummarizeRefusesAContextTheScenarioDoesNotName(t *testing.T) {
	if _, err := load(t).Summarize(context.Background(), agent.ContextBundle{Context: "other"}); err == nil {
		t.Fatal("a context with no draft was answered")
	}
}

func TestSummarizeRefusesAScenarioWithABadLevel(t *testing.T) {
	bad, err := LoadBytes([]byte("contexts:\n  calc:\n    intent: i\n    requirements:\n      - id: r\n        level: MUSTARD\n        text: t\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(bad).Summarize(context.Background(), agent.ContextBundle{Context: "calc"}); err == nil {
		t.Fatal("an unknown level was accepted from a scenario")
	}
}

func TestRealizeAppliesTheStepsInOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("BEFORE"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := load(t).Realize(context.Background(), agent.RealizeRequest{Context: "calc", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 {
		t.Errorf("files = %v, want the write and the patch", result.Files)
	}
	written, err := os.ReadFile(filepath.Join(dir, "calc", "calc.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "func Add") {
		t.Errorf("written = %q", written)
	}
	patched, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(patched) != "AFTER" {
		t.Errorf("patched = %q", patched)
	}
}

func TestRealizeRefusesAPatchThatDoesNotMatchAndAPathOutsideTheTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nothing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := load(t).Realize(context.Background(), agent.RealizeRequest{Context: "calc", Dir: dir}); err == nil {
		t.Fatal("a patch that matches nothing was applied")
	}

	escape, err := LoadBytes([]byte("realize:\n  calc:\n    - write:\n        path: ../outside.txt\n        contents: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(escape).Realize(context.Background(), agent.RealizeRequest{Context: "calc", Dir: dir}); err == nil {
		t.Fatal("a write outside the working tree was applied")
	}
}

func TestRealizeRefusesAContextWithNoSteps(t *testing.T) {
	if _, err := load(t).Realize(context.Background(), agent.RealizeRequest{Context: "other", Dir: t.TempDir()}); err == nil {
		t.Fatal("a context with no steps was realized")
	}
}

func TestLoadReadsAJSONScenarioToo(t *testing.T) {
	loaded, err := LoadBytes([]byte(`{"contexts": {"calc": {"intent": "json intent"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := New(loaded).Summarize(context.Background(), agent.ContextBundle{Context: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Intent != "json intent" {
		t.Errorf("intent = %q", draft.Intent)
	}
}
