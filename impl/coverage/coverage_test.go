package coverage

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func deltaWith(entries ...spec.RequirementDelta) spec.Delta {
	return spec.Delta{Requirements: entries}
}

func TestRequirementsKeepsOnlyAddedAndChanged(t *testing.T) {
	delta := deltaWith(
		spec.RequirementDelta{Op: spec.OpAdded, ID: "r.add", To: &spec.Requirement{ID: "r.add", Level: spec.LevelMust, Text: "Add."}},
		spec.RequirementDelta{Op: spec.OpRemoved, ID: "r.gone", From: &spec.Requirement{ID: "r.gone", Level: spec.LevelMust, Text: "Gone."}},
		spec.RequirementDelta{Op: spec.OpChanged, ID: "r.edit", From: &spec.Requirement{ID: "r.edit", Level: spec.LevelShould}, To: &spec.Requirement{ID: "r.edit", Level: spec.LevelMust, Text: "Edited."}},
	)
	got := Requirements(delta)
	if len(got) != 2 {
		t.Fatalf("requirements = %+v", got)
	}
	if got[0].ID != "r.add" || got[0].Op != spec.OpAdded || got[0].Text != "Add." {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ID != "r.edit" || got[1].Op != spec.OpChanged {
		t.Errorf("second = %+v", got[1])
	}
}

func TestMissingNamesTheUnimplementedOnes(t *testing.T) {
	verdicts := []Verdict{
		{ID: "r.a", Implemented: true},
		{ID: "r.b", Implemented: false, Evidence: "no such code"},
	}
	missing := Missing(verdicts)
	if len(missing) != 1 || missing[0].ID != "r.b" {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestParseVerdictsReadsAProseWrappedAnswer(t *testing.T) {
	raw := "Here is the answer:\n{\"verdicts\":[{\"id\":\"r.a\",\"implemented\":true,\"evidence\":\"x\"},{\"id\":\"r.b\",\"implemented\":false}]}\nDone."
	verdicts, err := ParseVerdicts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(verdicts) != 2 || !verdicts[0].Implemented || verdicts[1].Implemented {
		t.Fatalf("verdicts = %+v", verdicts)
	}
	if _, err := ParseVerdicts("no json here"); err == nil {
		t.Fatal("an answer without JSON parsed")
	}
}

func TestPromptNamesEveryRequirementAndTheDiff(t *testing.T) {
	prompt := Prompt(Request{
		Change: "calc-s2c-1",
		Requirements: []Requirement{
			{ID: "r.add", Level: "MUST", Text: "Add returns the sum."},
			{ID: "r.sub", Level: "SHOULD", Text: "Subtract returns the difference."},
		},
		Diff: "diff --git a/calc.go b/calc.go\n+func Subtract() {}",
	})
	for _, want := range []string{"r.add", "r.sub", "Add returns the sum.", "+func Subtract"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
}

func TestTruncateBoundsAHugeDiff(t *testing.T) {
	diff := strings.Repeat("a", DiffLimit*2)
	out := Truncate(diff)
	if len(out) > DiffLimit+64 {
		t.Fatalf("truncated length = %d", len(out))
	}
	if !strings.Contains(out, "diff truncated") {
		t.Fatalf("the truncation is not marked")
	}
}
