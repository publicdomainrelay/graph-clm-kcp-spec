package eval_test

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func observed(names ...string) spec.ObservedFacts {
	facts := spec.ObservedFacts{Files: []string{"calc/calc.go"}}
	for _, name := range names {
		facts.Interfaces = append(facts.Interfaces, spec.ObservedInterface{
			Name: name, Kind: "function", File: "calc/calc.go", CodegraphID: "function:" + name,
		})
	}
	return facts
}

func TestMatchSetsScoresRecallAndPrecisionApart(t *testing.T) {
	// The declaration found both observed symbols and invented one.
	score := eval.MatchSets([]string{"Add", "Multiply", "Ghost"}, []string{"Add", "Multiply"})
	if score.Matched != 2 || score.Target != 2 || score.Declared != 3 {
		t.Fatalf("score = %+v", score)
	}
	if score.Recall != 1 {
		t.Errorf("recall = %v, want 1", score.Recall)
	}
	if score.Precision < 0.66 || score.Precision > 0.67 {
		t.Errorf("precision = %v, want two of three", score.Precision)
	}
	if len(score.Extra) != 1 || score.Extra[0] != "Ghost" {
		t.Errorf("extra = %v", score.Extra)
	}
	// The declaration missed one observed symbol and invented nothing.
	missed := eval.MatchSets([]string{"Add"}, []string{"Add", "Multiply"})
	if missed.Recall != 0.5 || missed.Precision != 1 {
		t.Errorf("missed = %+v", missed)
	}
	if len(missed.Missing) != 1 || missed.Missing[0] != "Multiply" {
		t.Errorf("missing = %v", missed.Missing)
	}
}

func TestMatchSetsIsOneWhenThereIsNothingToFind(t *testing.T) {
	score := eval.MatchSets(nil, nil)
	if score.Recall != 1 || score.Precision != 1 || score.F1 != 1 {
		t.Fatalf("empty score = %+v, want a perfect one", score)
	}
}

func TestInterfaceScoreReadsTheObservedSurface(t *testing.T) {
	score := eval.InterfaceScore(
		[]spec.Interface{{Name: "Add"}, {Name: "Multiply"}},
		observed("Add", "Multiply"),
	)
	if score.Recall != 1 || score.Precision != 1 {
		t.Fatalf("score = %+v", score)
	}
}

func TestAnchoringRateCountsARequirementWithNoRef(t *testing.T) {
	facts := observed("Add")
	requirements := []spec.Requirement{
		{ID: "r.add", Level: spec.LevelMust, Text: "Add sums.", CodeRefs: []string{"Add"}},
		{ID: "r.ghost", Level: spec.LevelMust, Text: "Ghost.", CodeRefs: []string{"Ghost"}},
		{ID: "r.bare", Level: spec.LevelMust, Text: "Nothing points at this."},
	}
	if got := eval.AnchoringRate(requirements, facts); got < 0.33 || got > 0.34 {
		t.Fatalf("anchoring = %v, want one of three", got)
	}
	if got := eval.AnchoringRate(nil, facts); got != 1 {
		t.Fatalf("anchoring with no requirements = %v, want 1", got)
	}
}

func TestValidatorPassUsesTheSameValidatorAHumanEditPasses(t *testing.T) {
	good := spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   spec.RefSelf,
		Intent:     "Arithmetic.",
		Requirements: []spec.Requirement{
			{ID: "r.add", Level: spec.LevelMust, Text: "Add sums.", CodeRefs: []string{"function:Add"}},
		},
	}
	if !eval.ValidatorPass("calc", good) {
		t.Error("a valid spec did not pass")
	}
	bad := good
	bad.Requirements = []spec.Requirement{{ID: "r.add", Level: "SURELY", Text: "Add sums."}}
	if eval.ValidatorPass("calc", bad) {
		t.Error("an unknown level passed the validator")
	}
}

func TestJaccardMeasuresRoundTripStability(t *testing.T) {
	if got := eval.Jaccard([]string{"Add", "Multiply"}, []string{"Multiply", "Add"}); got != 1 {
		t.Errorf("the same set twice = %v, want 1", got)
	}
	if got := eval.Jaccard([]string{"Add", "Multiply"}, []string{"Add", "Divide"}); got < 0.33 || got > 0.34 {
		t.Errorf("one of three shared = %v", got)
	}
	if got := eval.Jaccard(nil, nil); got != 1 {
		t.Errorf("two empty sets = %v, want 1", got)
	}
}

func TestScoreDeltaComparesEntriesToIntent(t *testing.T) {
	change := &spec.Delta{
		Interfaces: []spec.InterfaceDelta{{Op: spec.OpAdded, Name: "Subtract"}},
		Requirements: []spec.RequirementDelta{{
			Op: spec.OpAdded, ID: "r.subtract",
			To: &spec.Requirement{ID: "r.subtract", Level: spec.LevelMust, Text: "Subtract."},
		}},
	}
	score := eval.ScoreDelta(change, 2)
	if score.Entries != 2 || score.Added != 2 || !score.Precise {
		t.Fatalf("score = %+v", score)
	}
	if over := eval.ScoreDelta(change, 3); over.Precise {
		t.Fatalf("a three entry intent accepted a two entry delta: %+v", over)
	}
	if nilScore := eval.ScoreDelta(nil, 0); !nilScore.Precise {
		t.Fatalf("no delta against no intent = %+v", nilScore)
	}
}

func TestFilesOutsideContextOwnsTheDirectoryNotOneFile(t *testing.T) {
	facts := spec.ObservedFacts{Files: []string{"calc/calc.go", "calc/calc_test.go"}}
	outside := eval.FilesOutsideContext([]string{"calc/calc.go", "calc/errors.go", "cmd/calc/main.go"}, facts)
	if len(outside) != 1 || outside[0] != "cmd/calc/main.go" {
		t.Fatalf("outside = %v, want only the other context's file", outside)
	}
	// The host inside the model renders the context document into the tree and
	// the commit carries it. That is the CLM loop working, not the agent
	// leaving the context it was given.
	withDoc := eval.FilesOutsideContext([]string{".specs/context/calc.md", "calc/calc.go"}, facts)
	if len(withDoc) != 0 {
		t.Fatalf("outside = %v, want the spec artifact ignored", withDoc)
	}
	if got := eval.FilesOutsideContext(nil, facts); len(got) != 0 {
		t.Fatalf("outside = %v, want none", got)
	}
}

func TestReportRendersEveryMeasure(t *testing.T) {
	report := eval.Report{
		Agent:      "scripted",
		Fixtures:   "fixtures",
		StartedAt:  "2026-01-01T00:00:00Z",
		FinishedAt: "2026-01-01T00:01:00Z",
		Populate: []eval.PopulateReport{
			{Fixture: "calc", Phase: "Populated", Contexts: 2, Summarized: 2, WallTimeSeconds: 3.5},
		},
		CodeToSpec: []eval.CodeToSpecReport{
			{Fixture: "calc", Context: "calc", Score: eval.Score{Declared: 2, Target: 2, Matched: 2, Recall: 1, Precision: 1, F1: 1}, AnchoringRate: 1, ValidatorPass: true, RoundTripJaccard: 1},
		},
		Scenarios: []eval.ScenarioReport{
			{Fixture: "calc", Scenario: "add-subtract", Context: "calc", Difficulty: 1, Pass: true, VerifyPass: true, AcceptancePass: true, Delta: eval.DeltaScore{Entries: 2, Expected: 2, Precise: true}},
		},
	}
	if report.ScenarioPassRate() != 1 || report.MeanRecall() != 1 || report.MeanAnchoring() != 1 || report.DeltaPrecisionRate() != 1 {
		t.Fatalf("rates = %v %v %v %v", report.ScenarioPassRate(), report.MeanRecall(), report.MeanAnchoring(), report.DeltaPrecisionRate())
	}
	markdown := report.Markdown()
	for _, want := range []string{"spec -> code pass rate", "interface recall", "add-subtract", "Populated"} {
		if !strings.Contains(markdown, want) {
			t.Errorf("the markdown report does not mention %q:\n%s", want, markdown)
		}
	}
	encoded, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"scenario": "add-subtract"`) {
		t.Errorf("the json report does not carry the scenario:\n%s", encoded)
	}
}

func TestReportRatesAreOneWhenNothingWasMeasured(t *testing.T) {
	empty := eval.Report{}
	if empty.ScenarioPassRate() != 1 || empty.MeanRecall() != 1 || empty.MeanAnchoring() != 1 {
		t.Fatalf("empty rates = %v %v %v", empty.ScenarioPassRate(), empty.MeanRecall(), empty.MeanAnchoring())
	}
}
