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
		Interactions: []spec.Interaction{
			{ID: "i.report", Peer: "host", Initiator: spec.InitiatorSelf, Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust},
			{ID: "i.no-reach-in", Peer: "host", Initiator: spec.InitiatorPeer, Level: spec.LevelMust, Forbidden: true},
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
	if len(parsed.Spec.Interactions) != 2 || parsed.Spec.Interactions[0].ID != "i.no-reach-in" {
		t.Fatalf("interactions did not survive the round trip: %+v", parsed.Spec.Interactions)
	}
	if !parsed.Spec.Interactions[0].Forbidden || parsed.Spec.Interactions[0].Level != spec.LevelMust {
		t.Errorf("the forbidden marker or level was dropped: %+v", parsed.Spec.Interactions[0])
	}
	if parsed.Spec.Interactions[1].Channel != "relay" || len(parsed.Spec.Interactions[1].Carries) != 1 {
		t.Errorf("the channel or carries were dropped: %+v", parsed.Spec.Interactions[1])
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
