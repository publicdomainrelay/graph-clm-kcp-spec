package mirror

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func TestRenderAndParseRoundTrip(t *testing.T) {
	context := spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   "self",
		Intent:     "a calculator",
		Requirements: []spec.Requirement{
			{ID: "r.mul", Level: "MUST", Text: "multiply", CodeRefs: []string{"function:Multiply"}},
			{ID: "r.add", Level: "MUST", Text: "add"},
		},
		CodeRefs: []string{"file:calc/calc.go"},
	}
	data, err := Render("calc", "default", context)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"apiVersion: specs.publicdomainrelay.dev/v1alpha1", "kind: SystemContext", "name: calc", "namespace: default", "intent: a calculator"} {
		if !strings.Contains(text, want) {
			t.Errorf("the document has no %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "status") {
		t.Errorf("the document carries status:\n%s", text)
	}
	if strings.Contains(text, "file:calc/calc.go") || strings.Contains(text, "\n  codeRefs:") {
		t.Errorf("the document carries the derived code refs:\n%s", text)
	}
	if !strings.Contains(text, "function:Multiply") {
		t.Errorf("a requirement's declared code ref was dropped:\n%s", text)
	}
	if strings.Index(text, "r.add") > strings.Index(text, "r.mul") {
		t.Errorf("requirements are not in id order:\n%s", text)
	}
	parsed, err := Parse("calc", data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Spec.Intent != "a calculator" || len(parsed.Spec.Requirements) != 2 {
		t.Fatalf("parsed = %+v", parsed.Spec)
	}
	second, err := Render("calc", "default", parsed.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != text {
		t.Error("render, parse, render is not stable")
	}
}

func TestParseRejectsAnotherKind(t *testing.T) {
	if _, err := Parse("calc", []byte("kind: Repository\nmetadata:\n  name: calc\n")); err == nil {
		t.Fatal("a Repository document was accepted")
	}
}
