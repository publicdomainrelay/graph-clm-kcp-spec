package mirror

import (
	"errors"
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
	// The keyed lists come out canonically ordered, so the same spec always
	// renders to the same bytes.
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

func TestApplyKeepsWhatTheToolOwns(t *testing.T) {
	live := spec.SystemContextSpec{Repository: "calc", Intent: "old", CodeRefs: []string{"file:calc/calc.go"}}
	file := spec.SystemContextSpec{Intent: "new", Interfaces: []spec.Interface{{Name: "Add"}}}
	merged, err := Apply(live, file)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Intent != "new" || len(merged.Interfaces) != 1 {
		t.Errorf("the file's declared state did not win: %+v", merged)
	}
	if merged.Repository != "calc" || len(merged.CodeRefs) != 1 {
		t.Errorf("the tool-owned fields did not survive: %+v", merged)
	}
	if _, err := Apply(live, spec.SystemContextSpec{Repository: "greet"}); err == nil {
		t.Fatal("a file that moves the context to another repository was accepted")
	}
}

func TestResolveConflictRule(t *testing.T) {
	const last = "aaaaaaaaaaaaaaaa"
	cases := []struct {
		name      string
		direction Direction
		prefer    Prefer
		state     State
		pull      bool
		push      bool
		conflict  bool
	}{
		{name: "a missing file is a pull, whatever the direction", direction: Both, state: State{Kcp: "k1"}, pull: true},
		{name: "push alone does nothing without a file", direction: Push, state: State{Kcp: "k1"}},
		{name: "the file moved, so push applies it", direction: Both, state: State{HasFile: true, Last: last, Kcp: last, File: "f1"}, push: true, pull: true},
		{name: "kcp moved, so pull writes the file and push leaves kcp alone", direction: Both, state: State{HasFile: true, Last: last, Kcp: "k2", File: last}, pull: true},
		{name: "both moved is a conflict", direction: Both, state: State{HasFile: true, Last: last, Kcp: "k2", File: "f2"}, conflict: true},
		{name: "both moved, kcp wins", direction: Both, prefer: PreferKCP, state: State{HasFile: true, Last: last, Kcp: "k2", File: "f2"}, pull: true},
		{name: "both moved, the file wins", direction: Both, prefer: PreferGit, state: State{HasFile: true, Last: last, Kcp: "k2", File: "f2"}, push: true},
		{name: "both sides agree and the baseline was behind", direction: Both, state: State{HasFile: true, Last: last, Kcp: "k2", File: "k2"}},
		{name: "a first pull adopts kcp as the baseline", direction: Pull, state: State{HasFile: true, Kcp: "k1", File: "f1"}, pull: true},
		{name: "a pull alone never clobbers a file that moved with kcp quiet", direction: Pull, state: State{HasFile: true, Last: last, Kcp: last, File: "f1"}, pull: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			action, err := Resolve(testCase.direction, testCase.prefer, testCase.state)
			if testCase.conflict {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("err = %v, want a conflict", err)
				}
				if !strings.Contains(err.Error(), "--prefer") {
					t.Errorf("the conflict does not say how to resolve it: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if action.Pull != testCase.pull || action.Push != testCase.push {
				t.Errorf("action = %+v, want pull=%v push=%v", action, testCase.pull, testCase.push)
			}
			if action.Note == "" {
				t.Error("an action carries no note")
			}
		})
	}
}

func TestParseDirectionAndPrefer(t *testing.T) {
	if _, err := ParseDirection("sideways"); err == nil {
		t.Fatal("an unknown direction was accepted")
	}
	if _, err := ParsePrefer("mine"); err == nil {
		t.Fatal("an unknown preference was accepted")
	}
	if got, err := ParseDirection("both"); err != nil || got != Both {
		t.Fatalf("both = %q, %v", got, err)
	}
}
