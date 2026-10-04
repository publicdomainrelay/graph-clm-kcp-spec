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
			{Fixture: "calc", Context: "calc", Score: eval.Score{Declared: 2, Target: 2, Matched: 2, Recall: 1, Precision: 1, F1: 1}, AnchoringRate: 1, ValidatorPass: true, RoundTripJaccard: 1, RoundTripMeasured: true},
		},
		Facts: []eval.FactsReport{
			{Fixture: "calc", Context: "calc", Expected: 1, Stated: 1, Score: 1},
		},
		Scenarios: []eval.ScenarioReport{
			{Fixture: "calc", Scenario: "add-subtract", Context: "calc", Difficulty: 1, Pass: true, VerifyPass: true, AcceptancePass: true, Delta: eval.DeltaScore{Entries: 2, Expected: 2, Precise: true}},
		},
	}
	for _, measure := range []eval.Measure{report.PassRate(""), report.MeanRecall(), report.MeanAnchoring(), report.DeltaPrecisionRate(), report.FactsScore()} {
		if !measure.Measured() || measure.Value != 1 {
			t.Fatalf("measure = %+v, want 1 over one sample", measure)
		}
	}
	markdown := report.Markdown()
	for _, want := range []string{"spec -> code pass rate", "interface recall", "add-subtract", "Populated", "code -> spec facts stated", "samples"} {
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

// TestReportSaysNotMeasuredWhenNothingWasMeasured is the honest-reporting rule
// phase 11 added: a measure with no samples has no value, and it is never
// printed as 0% or as 100%.
func TestReportSaysNotMeasuredWhenNothingWasMeasured(t *testing.T) {
	empty := eval.Report{}
	for name, measure := range map[string]eval.Measure{
		"pass rate":   empty.PassRate(""),
		"recall":      empty.MeanRecall(),
		"anchoring":   empty.MeanAnchoring(),
		"delta":       empty.DeltaPrecisionRate(),
		"facts":       empty.FactsScore(),
		"sufficiency": empty.SufficiencyPassRate(),
		"drift":       empty.DriftPassRate(),
		"validator":   empty.ValidatorPassRate(),
		"round trip":  empty.MeanRoundTripJaccard(),
		"populate":    empty.PopulatedFixtures(),
		"clm":         empty.PassRate("clm"),
	} {
		if measure.Measured() {
			t.Errorf("the %s of an empty report is measured: %+v", name, measure)
		}
		if measure.Text() != "not measured" {
			t.Errorf("the %s of an empty report prints %q", name, measure.Text())
		}
	}
	if markdown := empty.Markdown(); !strings.Contains(markdown, "not measured") {
		t.Errorf("the empty report does not say not measured:\n%s", markdown)
	}
}

// TestMeansExcludeEmptyContexts is the other half of the rule: a context with
// no declared and no observed surface scores a perfect 1 by definition, and
// averaging those in is how a run of empty contexts reports 100%.
func TestMeansExcludeEmptyContexts(t *testing.T) {
	report := eval.Report{
		CodeToSpec: []eval.CodeToSpecReport{
			{Fixture: "f", Context: "half", Score: eval.Score{Declared: 1, Target: 2, Matched: 1, Recall: 0.5, Precision: 1, F1: 2.0 / 3.0}, AnchoringRate: 0.5, RoundTripMeasured: true, RoundTripJaccard: 0.5},
			{Fixture: "f", Context: "empty", Empty: true, NoRequirements: true, ValidatorPass: true, RoundTripJaccard: 1},
		},
	}
	recall := report.MeanRecall()
	if recall.Samples != 1 || recall.Excluded != 1 {
		t.Fatalf("recall = %+v, want one sample and one exclusion", recall)
	}
	if recall.Value != 0.5 {
		t.Errorf("recall = %v, want the empty context left out", recall.Value)
	}
	if len(recall.ExcludedNames) != 1 || recall.ExcludedNames[0] != "f/empty" {
		t.Errorf("excluded = %v", recall.ExcludedNames)
	}
	if got := report.MeanAnchoring(); got.Samples != 1 || got.Value != 0.5 {
		t.Errorf("anchoring = %+v", got)
	}
	if got := report.ValidatorPassRate(); got.Samples != 1 {
		t.Errorf("validator = %+v, want the empty context left out", got)
	}
	if got := report.MeanRoundTripJaccard(); got.Samples != 1 || got.Value != 0.5 {
		t.Errorf("round trip = %+v", got)
	}
	if text := report.Markdown(); !strings.Contains(text, "1 (1 excluded)") {
		t.Errorf("the report does not count the exclusion:\n%s", text)
	}
}

func TestSkippedScenariosAreCountedNotScored(t *testing.T) {
	report := eval.Report{
		Scenarios: []eval.ScenarioReport{
			{Fixture: "f", Scenario: "did", Pass: true, Delta: eval.DeltaScore{Precise: true}},
			{Fixture: "f", Scenario: "clm", Via: "clm", Skipped: true, Pass: false, Delta: eval.DeltaScore{Expected: 1}},
		},
	}
	rate := report.PassRate("")
	if rate.Samples != 1 || rate.Excluded != 1 || rate.Value != 1 {
		t.Fatalf("pass rate = %+v, want one sample and one skip", rate)
	}
	if clm := report.PassRate("clm"); clm.Measured() {
		t.Fatalf("the clm pass rate = %+v, want not measured", clm)
	}
	if delta := report.DeltaPrecisionRate(); delta.Samples != 1 {
		t.Fatalf("delta = %+v, want the skipped scenario left out", delta)
	}
}

func TestKeywordJudgeIsDeterministicAndReadsAlternatives(t *testing.T) {
	facts := []eval.Fact{
		{ID: "zero", Text: "Divide rejects a zero divisor.", Must: []eval.Keyword{{"zero", "0"}, {"error", "err"}}},
		{ID: "absent", Text: "Abs is exported.", Must: []eval.Keyword{{"abs"}}},
	}
	result, err := eval.KeywordJudge{}.Judge(t.Context(), eval.JudgeRequest{
		Context: "calc",
		Facts:   facts,
		Spec:    "Divide returns an error when b is 0.",
	})
	if err != nil {
		t.Fatal(err)
	}
	report := eval.ScoreFacts("calc", "calc", facts, result, "keyword")
	if report.Stated != 1 || report.Expected != 2 || report.Score != 0.5 {
		t.Fatalf("facts report = %+v", report)
	}
	byID := map[string]eval.FactVerdict{}
	for _, verdict := range report.Verdicts {
		byID[verdict.ID] = verdict
	}
	if !byID["zero"].Stated {
		t.Error("the alternative 0 did not satisfy the zero keyword")
	}
	if byID["absent"].Stated {
		t.Error("a fact with no matching word was stated")
	}
}

func TestScoreInterfaceDeltaCountsOnlyInterfaces(t *testing.T) {
	change := spec.Delta{
		Intent:       &spec.FieldDelta{From: "old", To: "new"},
		Requirements: []spec.RequirementDelta{{Op: spec.OpAdded, ID: "r.abs"}},
		Interfaces: []spec.InterfaceDelta{
			{Op: spec.OpAdded, Name: "Abs"},
			{Op: spec.OpRemoved, Name: "Sign"},
		},
	}
	entries, score := eval.ScoreInterfaceDelta(change, 2)
	if entries.Added[0] != "Abs" || entries.Removed[0] != "Sign" {
		t.Fatalf("entries = %+v", entries)
	}
	if score.Entries != 2 || !score.Precise {
		t.Fatalf("score = %+v, want the intent text ignored", score)
	}
}

func TestCompareFlagsARunEqualToTheBaseline(t *testing.T) {
	baseline := eval.Report{Agent: "scripted", Scenarios: []eval.ScenarioReport{
		{Fixture: "f", Scenario: "s", Pass: true, Delta: eval.DeltaScore{Precise: true}},
	}}
	live := eval.Report{Agent: "claude-mod", Scenarios: []eval.ScenarioReport{
		{Fixture: "f", Scenario: "s", Pass: true, Delta: eval.DeltaScore{Precise: true}},
	}}
	comparison := eval.Compare(baseline, live)
	if !comparison.NotDiscriminating {
		t.Fatalf("comparison = %+v, want the flag", comparison)
	}
	if !strings.Contains(comparison.Markdown(), "not discriminating: **yes**") {
		t.Errorf("the markdown does not flag it:\n%s", comparison.Markdown())
	}

	live.Scenarios[0].Pass = false
	differing := eval.Compare(baseline, live)
	if differing.NotDiscriminating {
		t.Fatalf("comparison = %+v, want no flag when the pass rate differs", differing)
	}

	// A measure only the live run could take, scored perfect, is still nothing
	// that separates the two agents, and the note says so.
	withClm := eval.Report{Agent: "claude-mod", Scenarios: []eval.ScenarioReport{
		{Fixture: "f", Scenario: "s", Pass: true, Delta: eval.DeltaScore{Precise: true}},
		{Fixture: "f", Scenario: "clm", Via: "clm", Pass: true, Delta: eval.DeltaScore{Precise: true}},
	}}
	reaching := eval.Compare(baseline, withClm)
	if !reaching.NotDiscriminating {
		t.Fatalf("comparison = %+v, want the flag: everything shared is equal and the extra measure is perfect", reaching)
	}
	if !strings.Contains(reaching.Note, "only the live run could take") {
		t.Errorf("note = %q", reaching.Note)
	}

	// A measure only the live run could take, scored below perfect, is the run
	// separating something: the baseline could not be measured there at all.
	withFailedSuffice := eval.Report{Agent: "claude-mod",
		Scenarios: []eval.ScenarioReport{
			{Fixture: "f", Scenario: "s", Pass: true, Delta: eval.DeltaScore{Precise: true}},
		},
		Sufficiency: []eval.SufficiencyReport{
			{Fixture: "f", Context: "f", Files: 1, Stripped: 2, Realized: true, TestsPass: false},
		},
	}
	reachingFailed := eval.Compare(baseline, withFailedSuffice)
	if reachingFailed.NotDiscriminating {
		t.Fatalf("comparison = %+v, want no flag when the live-only measure is not perfect", reachingFailed)
	}
	if !strings.Contains(reachingFailed.Note, "below perfect") {
		t.Errorf("note = %q", reachingFailed.Note)
	}
}
