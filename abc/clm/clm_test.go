package clm

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func sample() spec.SystemContextSpec {
	return spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   spec.RefSelf,
		Intent:     "The calc context adds two integers.",
		Requirements: []spec.Requirement{
			{ID: "r.add", Level: spec.LevelMust, Text: "Add adds two integers.", CodeRefs: []string{"function:add"}},
		},
		Interfaces: []spec.Interface{{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int"}},
		CodeRefs:   []string{"file:calc/calc.go"},
	}
}

func TestRenderParseRoundTrip(t *testing.T) {
	model, err := RenderModelZone("calc", "calc", sample())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseModelZone(model)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Intent != "The calc context adds two integers." {
		t.Errorf("intent = %q", parsed.Intent)
	}
	if len(parsed.Requirements) != 1 || parsed.Requirements[0].ID != "r.add" {
		t.Fatalf("requirements = %+v", parsed.Requirements)
	}
	if len(parsed.Requirements[0].CodeRefs) != 1 || !spec.IsCodeRef(parsed.Requirements[0].CodeRefs[0]) {
		t.Errorf("codeRefs = %v", parsed.Requirements[0].CodeRefs)
	}
	if len(parsed.Interfaces) != 1 || parsed.Interfaces[0].Name != "Add" {
		t.Fatalf("interfaces = %+v", parsed.Interfaces)
	}
	// The tool owns these, so they never round trip through the model zone.
	if parsed.Repository != "" || parsed.CodeRefs != nil || parsed.Arch != nil {
		t.Errorf("the model zone carried a tool-owned field: %+v", parsed)
	}
	if got := MergeDeclared(sample(), parsed); got.CodeRefs[0] != "file:calc/calc.go" {
		t.Errorf("the merge dropped the code refs: %v", got.CodeRefs)
	}
}

func TestAnEmptyIntentIsNotAPlaceholder(t *testing.T) {
	model, err := RenderModelZone("calc", "calc", spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model, EmptyIntent) {
		t.Fatalf("the placeholder is missing:\n%s", model)
	}
	parsed, err := ParseModelZone(model)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Intent != "" {
		t.Errorf("intent = %q, want empty", parsed.Intent)
	}
}

func TestAModelZoneWithoutASpecBlockIsRefused(t *testing.T) {
	if _, err := ParseModelZone("# Context: calc\n\njust prose\n"); err == nil {
		t.Error("a model zone with no spec block was accepted")
	}
	if _, err := ParseModelZone("```yaml spec\nrequirements: []\n"); err == nil {
		t.Error("an unclosed spec block was accepted")
	}
}

func TestDocumentHasOneManagedZone(t *testing.T) {
	model, err := RenderModelZone("calc", "calc", sample())
	if err != nil {
		t.Fatal(err)
	}
	document := Document(model, []agent.ResolvedRef{{CodegraphID: "function:add", Kind: "function", Name: "Add", FilePath: "calc/calc.go"}}, 0)
	if !strings.Contains(document, agent.ManagedBegin) || !strings.Contains(document, agent.ManagedEnd) {
		t.Fatalf("the managed zone is missing:\n%s", document)
	}
	parsedModel, managed := SplitDocument(document)
	if !strings.Contains(managed, "function:add") {
		t.Errorf("the managed zone does not list the resolved ref: %q", managed)
	}
	if _, err := ParseModelZone(parsedModel); err != nil {
		t.Errorf("the model zone does not survive a split: %v", err)
	}
}

func TestDeclaredDropsWhatTheToolOwns(t *testing.T) {
	declared := Declared(sample())
	if declared.Repository != "" || declared.CodeRefs != nil || declared.Arch != nil {
		t.Errorf("declared = %+v", declared)
	}
	if declared.Intent == "" || len(declared.Requirements) != 1 {
		t.Errorf("declared dropped a field the model owns: %+v", declared)
	}
}
