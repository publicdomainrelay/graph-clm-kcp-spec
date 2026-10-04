package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type PopulateReport struct {
	Fixture string `json:"fixture"`

	Phase string `json:"phase"`

	Contexts int `json:"contexts"`

	Summarized int `json:"summarized"`

	Failed int `json:"failed"`

	WallTimeSeconds float64 `json:"wallTimeSeconds"`

	Failures []string `json:"failures,omitempty"`

	Error string `json:"error,omitempty"`
}

type CodeToSpecReport struct {
	Fixture string `json:"fixture"`

	Context string `json:"context"`

	Score Score `json:"interfaces"`

	Empty bool `json:"empty,omitempty"`

	Requirements int `json:"requirements"`

	AnchoringRate float64 `json:"anchoringRate"`

	NoRequirements bool `json:"noRequirements,omitempty"`

	ValidatorPass bool `json:"validatorPass"`

	DroppedRefs int `json:"droppedRefs"`

	RoundTripJaccard float64 `json:"roundTripJaccard"`

	RoundTripMeasured bool `json:"roundTripMeasured"`

	RequirementCountDelta int `json:"requirementCountDelta"`
}

type ScenarioReport struct {
	Fixture string `json:"fixture"`

	Scenario string `json:"scenario"`

	Context string `json:"context"`

	Difficulty int `json:"difficulty"`

	Description string `json:"description"`

	Via string `json:"via,omitempty"`

	Request string `json:"request,omitempty"`

	Skipped bool `json:"skipped,omitempty"`

	Pass bool `json:"pass"`

	VerifyPass bool `json:"verifyPass"`

	AcceptancePass bool `json:"acceptancePass"`

	Delta DeltaScore `json:"delta"`

	Attempts int `json:"attempts"`

	FilesOutside []string `json:"filesOutside,omitempty"`

	ProgressRecords int `json:"progressRecords"`

	WallTimeSeconds float64 `json:"wallTimeSeconds"`

	Agent string `json:"agent"`

	Error string `json:"error,omitempty"`
}

type SufficiencyReport struct {
	Fixture string `json:"fixture"`

	Context string `json:"context"`

	Files int `json:"files"`

	Stripped int `json:"stripped"`

	Realized bool `json:"realized"`

	TestsPass bool `json:"testsPass"`

	FilesTouched []string `json:"filesTouched,omitempty"`

	WallTimeSeconds float64 `json:"wallTimeSeconds"`

	Agent string `json:"agent"`

	Error string `json:"error,omitempty"`

	Skipped bool `json:"skipped,omitempty"`
}

type DriftReport struct {
	Fixture string `json:"fixture"`

	Scenario string `json:"scenario"`

	Context string `json:"context"`

	Difficulty int `json:"difficulty"`

	Description string `json:"description"`

	ChangePhase string `json:"changePhase"`

	InterfacesAdded []string `json:"interfacesAdded,omitempty"`

	InterfacesRemoved []string `json:"interfacesRemoved,omitempty"`

	ExpectedAdded []string `json:"expectedAdded,omitempty"`

	ExpectedRemoved []string `json:"expectedRemoved,omitempty"`

	Delta DeltaScore `json:"delta"`

	ProseScore FactsReport `json:"prose"`

	Pass bool `json:"pass"`

	WallTimeSeconds float64 `json:"wallTimeSeconds"`

	Error string `json:"error,omitempty"`
}

type Report struct {
	Agent string `json:"agent"`

	Fixtures string `json:"fixtures"`

	StartedAt string `json:"startedAt"`

	FinishedAt string `json:"finishedAt"`

	Populate []PopulateReport `json:"populate"`

	CodeToSpec []CodeToSpecReport `json:"codeToSpec"`

	Facts []FactsReport `json:"facts,omitempty"`

	Sufficiency []SufficiencyReport `json:"sufficiency,omitempty"`

	Drift []DriftReport `json:"drift,omitempty"`

	Scenarios []ScenarioReport `json:"scenarios"`

	Notes []string `json:"notes,omitempty"`
}

func (r Report) PassRate(via string) Measure {
	samples, excluded := []float64{}, []string{}
	for _, scenario := range r.Scenarios {
		if via != "" && scenario.Via != via {
			continue
		}
		name := scenario.Fixture + "/" + scenario.Scenario
		switch {
		case scenario.Skipped:
			excluded = append(excluded, name)
		default:
			samples = append(samples, boolValue(scenario.Pass))
		}
	}
	return measure(samples, excluded)
}

func (r Report) DeltaPrecisionRate() Measure {
	samples, excluded := []float64{}, []string{}
	for _, scenario := range r.Scenarios {
		name := scenario.Fixture + "/" + scenario.Scenario
		if scenario.Skipped {
			excluded = append(excluded, name)
			continue
		}
		samples = append(samples, boolValue(scenario.Delta.Precise))
	}
	return measure(samples, excluded)
}

func (r Report) FactsScore() Measure {
	samples, excluded := []float64{}, []string{}
	for _, entry := range r.Facts {
		name := entry.Fixture + "/" + entry.Context
		if entry.Expected == 0 || entry.Error != "" {
			excluded = append(excluded, name)
			continue
		}
		samples = append(samples, entry.Score)
	}
	return measure(samples, excluded)
}

func (r Report) SufficiencyPassRate() Measure {
	samples, excluded := []float64{}, []string{}
	for _, entry := range r.Sufficiency {
		name := entry.Fixture + "/" + entry.Context
		if entry.Skipped || entry.Files == 0 {
			excluded = append(excluded, name)
			continue
		}
		samples = append(samples, boolValue(entry.TestsPass))
	}
	return measure(samples, excluded)
}

func (r Report) DriftPassRate() Measure {
	samples := []float64{}
	for _, entry := range r.Drift {
		samples = append(samples, boolValue(entry.Pass))
	}
	return measure(samples, nil)
}

func (r Report) MeanRecall() Measure {
	return r.meanScore(func(s Score) float64 { return s.Recall })
}

func (r Report) MeanPrecision() Measure {
	return r.meanScore(func(s Score) float64 { return s.Precision })
}

func (r Report) MeanF1() Measure { return r.meanScore(func(s Score) float64 { return s.F1 }) }

func (r Report) MeanAnchoring() Measure {
	samples, excluded := []float64{}, []string{}
	for _, entry := range r.CodeToSpec {
		name := entry.Fixture + "/" + entry.Context
		if entry.NoRequirements {
			excluded = append(excluded, name)
			continue
		}
		samples = append(samples, entry.AnchoringRate)
	}
	return measure(samples, excluded)
}

func (r Report) ValidatorPassRate() Measure {
	samples, excluded := []float64{}, []string{}
	for _, entry := range r.CodeToSpec {
		name := entry.Fixture + "/" + entry.Context
		if entry.Empty {
			excluded = append(excluded, name)
			continue
		}
		samples = append(samples, boolValue(entry.ValidatorPass))
	}
	return measure(samples, excluded)
}

func (r Report) MeanRoundTripJaccard() Measure {
	samples, excluded := []float64{}, []string{}
	for _, entry := range r.CodeToSpec {
		name := entry.Fixture + "/" + entry.Context
		if !entry.RoundTripMeasured {
			excluded = append(excluded, name)
			continue
		}
		samples = append(samples, entry.RoundTripJaccard)
	}
	return measure(samples, excluded)
}

func (r Report) PopulatedFixtures() Measure {
	samples := []float64{}
	for _, entry := range r.Populate {
		samples = append(samples, boolValue(entry.Phase == "Populated"))
	}
	return measure(samples, nil)
}

func (r Report) meanScore(pick func(Score) float64) Measure {
	samples, excluded := []float64{}, []string{}
	for _, entry := range r.CodeToSpec {
		name := entry.Fixture + "/" + entry.Context
		if entry.Empty {
			excluded = append(excluded, name)
			continue
		}
		samples = append(samples, pick(entry.Score))
	}
	return measure(samples, excluded)
}

func boolValue(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func (r Report) JSON() ([]byte, error) {
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("eval: encode the report: %w", err)
	}
	return append(encoded, '\n'), nil
}

func ParseReport(encoded []byte) (Report, error) {
	report := Report{}
	if err := json.Unmarshal(encoded, &report); err != nil {
		return report, fmt.Errorf("eval: parse a report: %w", err)
	}
	return report, nil
}

func (r Report) Markdown() string {
	builder := &strings.Builder{}
	fmt.Fprintf(builder, "# specctl eval report\n\n")
	fmt.Fprintf(builder, "- agent: `%s`\n", orNone(r.Agent))
	fmt.Fprintf(builder, "- fixtures: `%s`\n", r.Fixtures)
	fmt.Fprintf(builder, "- started: %s\n", r.StartedAt)
	fmt.Fprintf(builder, "- finished: %s\n", r.FinishedAt)
	fmt.Fprintf(builder, "- scenarios: %d\n\n", len(r.Scenarios))

	measures := []struct {
		name    string
		measure Measure
	}{
		{"spec -> code pass rate (verify + acceptance)", r.PassRate("")},
		{"spec -> code via CLM", r.PassRate("clm")},
		{"delta precision (entries as intended)", r.DeltaPrecisionRate()},
		{"code -> spec facts stated (judged)", r.FactsScore()},
		{"spec sufficiency (rebuild from spec, original tests)", r.SufficiencyPassRate()},
		{"drift (code -> spec from a human edit)", r.DriftPassRate()},
		{"interface recall", r.MeanRecall()},
		{"interface precision", r.MeanPrecision()},
		{"interface F1", r.MeanF1()},
		{"requirement anchoring", r.MeanAnchoring()},
		{"validator pass", r.ValidatorPassRate()},
		{"round trip interface Jaccard", r.MeanRoundTripJaccard()},
		{"fixtures reaching Populated", r.PopulatedFixtures()},
	}
	fmt.Fprintf(builder, "## Measures\n\n")
	fmt.Fprintf(builder, "| measure | value | samples |\n| --- | --- | --- |\n")
	for _, entry := range measures {
		fmt.Fprintf(builder, "| %s | %s | %s |\n", entry.name, entry.measure.Text(), entry.measure.SamplesText())
	}
	fmt.Fprintf(builder, "\n")

	r.writePopulate(builder)
	r.writeCodeToSpec(builder)
	r.writeFacts(builder)
	r.writeSufficiency(builder)
	r.writeDrift(builder)
	r.writeScenarios(builder)

	if len(r.Notes) > 0 {
		fmt.Fprintf(builder, "## Notes\n\n")
		for _, note := range r.Notes {
			fmt.Fprintf(builder, "- %s\n", note)
		}
		fmt.Fprintf(builder, "\n")
	}
	return builder.String()
}

func (r Report) writePopulate(builder *strings.Builder) {
	fmt.Fprintf(builder, "## Populate (one Repository manifest per fixture)\n\n")
	if len(r.Populate) == 0 {
		fmt.Fprintf(builder, "not measured\n\n")
		return
	}
	fmt.Fprintf(builder, "| fixture | phase | contexts | summarized | failed | wall time |\n")
	fmt.Fprintf(builder, "| --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range r.Populate {
		phase := entry.Phase
		if entry.Error != "" {
			phase = fmt.Sprintf("%s (%s)", entry.Phase, entry.Error)
		}
		fmt.Fprintf(builder, "| %s | %s | %d | %d | %d | %s |\n",
			entry.Fixture, phase, entry.Contexts, entry.Summarized, entry.Failed, seconds(entry.WallTimeSeconds))
	}
	fmt.Fprintf(builder, "\n")
	for _, entry := range r.Populate {
		for _, failure := range entry.Failures {
			fmt.Fprintf(builder, "- %s could not be summarized: %s\n", entry.Fixture, failure)
		}
	}
}

func (r Report) writeCodeToSpec(builder *strings.Builder) {
	fmt.Fprintf(builder, "## Code -> spec\n\n")
	if len(r.CodeToSpec) == 0 {
		fmt.Fprintf(builder, "not measured\n\n")
		return
	}
	fmt.Fprintf(builder, "| fixture | context | observed | declared | recall | precision | anchoring | validator | round trip | req delta |\n")
	fmt.Fprintf(builder, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range r.CodeToSpec {
		roundTrip := "not measured"
		if entry.RoundTripMeasured {
			roundTrip = percent(entry.RoundTripJaccard)
		}
		anchoring := percent(entry.AnchoringRate)
		if entry.NoRequirements {
			anchoring = "not measured"
		}
		fmt.Fprintf(builder, "| %s | %s | %d | %d | %s | %s | %s | %s | %s | %+d |\n",
			entry.Fixture, entry.Context, entry.Score.Target, entry.Score.Declared,
			percent(entry.Score.Recall), percent(entry.Score.Precision), anchoring,
			yesNo(entry.ValidatorPass), roundTrip, entry.RequirementCountDelta)
	}
	fmt.Fprintf(builder, "\n")
}

func (r Report) writeFacts(builder *strings.Builder) {
	if len(r.Facts) == 0 {
		return
	}
	fmt.Fprintf(builder, "## Facts stated (what the model was not handed)\n\n")
	fmt.Fprintf(builder, "| fixture | context | fact | stated | judge | evidence |\n")
	fmt.Fprintf(builder, "| --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range r.Facts {
		for _, verdict := range entry.Verdicts {
			evidence := verdict.Evidence
			if verdict.Reason != "" {
				evidence = verdict.Reason
			}
			fmt.Fprintf(builder, "| %s | %s | %s | %s | %s | %s |\n",
				entry.Fixture, entry.Context, verdict.ID, yesNo(verdict.Stated), verdict.Judge, truncate(evidence, 80))
		}
		if len(entry.Verdicts) == 0 && entry.Error != "" {
			fmt.Fprintf(builder, "| %s | %s | - | no | - | %s |\n", entry.Fixture, entry.Context, truncate(entry.Error, 80))
		}
	}
	fmt.Fprintf(builder, "\n")
}

func (r Report) writeSufficiency(builder *strings.Builder) {
	if len(r.Sufficiency) == 0 {
		return
	}
	fmt.Fprintf(builder, "## Spec sufficiency (spec kept, bodies removed, original tests)\n\n")
	fmt.Fprintf(builder, "| fixture | context | files | stripped | realized | tests | touched | wall time |\n")
	fmt.Fprintf(builder, "| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range r.Sufficiency {
		tests := yesNo(entry.TestsPass)
		if entry.Error != "" {
			tests = "no: " + firstLine(entry.Error)
		}
		if entry.Skipped {
			tests = "skipped"
		}
		fmt.Fprintf(builder, "| %s | %s | %d | %d | %s | %s | %d | %s |\n",
			entry.Fixture, entry.Context, entry.Files, entry.Stripped, yesNo(entry.Realized),
			tests, len(entry.FilesTouched), seconds(entry.WallTimeSeconds))
	}
	fmt.Fprintf(builder, "\n")
}

func (r Report) writeDrift(builder *strings.Builder) {
	if len(r.Drift) == 0 {
		return
	}
	fmt.Fprintf(builder, "## Drift (a human code edit, the spec specd wrote for it)\n\n")
	fmt.Fprintf(builder, "| fixture | scenario | context | phase | added | removed | delta | facts | pass |\n")
	fmt.Fprintf(builder, "| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range r.Drift {
		facts := "not measured"
		if entry.ProseScore.Expected > 0 {
			facts = fmt.Sprintf("%d/%d", entry.ProseScore.Stated, entry.ProseScore.Expected)
		}
		pass := yesNo(entry.Pass)
		if entry.Error != "" {
			pass = "no: " + firstLine(entry.Error)
		}
		fmt.Fprintf(builder, "| %s | %s | %s | %s | %s | %s | %d/%d | %s | %s |\n",
			entry.Fixture, entry.Scenario, entry.Context, entry.ChangePhase,
			strings.Join(entry.InterfacesAdded, " "), strings.Join(entry.InterfacesRemoved, " "),
			entry.Delta.Entries, entry.Delta.Expected, facts, pass)
	}
	fmt.Fprintf(builder, "\n")
}

func (r Report) writeScenarios(builder *strings.Builder) {
	fmt.Fprintf(builder, "## Spec -> code (hidden acceptance tests)\n\n")
	if len(r.Scenarios) == 0 {
		fmt.Fprintf(builder, "not measured\n\n")
		return
	}
	sorted := append([]ScenarioReport{}, r.Scenarios...)
	sort.SliceStable(sorted, func(left, right int) bool {
		if sorted[left].Fixture != sorted[right].Fixture {
			return sorted[left].Fixture < sorted[right].Fixture
		}
		return sorted[left].Scenario < sorted[right].Scenario
	})
	fmt.Fprintf(builder, "| fixture | scenario | via | level | context | pass | verify | acceptance | delta | attempts | outside | progress | wall time |\n")
	fmt.Fprintf(builder, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range sorted {
		delta := fmt.Sprintf("%d/%d", entry.Delta.Entries, entry.Delta.Expected)
		pass := yesNo(entry.Pass)
		switch {
		case entry.Skipped:
			pass = "skipped"
		case entry.Error != "":
			pass = "no: " + firstLine(entry.Error)
		}
		via := entry.Via
		if via == "" {
			via = "apply"
		}
		fmt.Fprintf(builder, "| %s | %s | %s | %d | %s | %s | %s | %s | %s | %d | %s | %d | %s |\n",
			entry.Fixture, entry.Scenario, via, entry.Difficulty, entry.Context, pass,
			yesNo(entry.VerifyPass), yesNo(entry.AcceptancePass), delta, entry.Attempts,
			outsideText(entry.FilesOutside), entry.ProgressRecords, seconds(entry.WallTimeSeconds))
	}
	fmt.Fprintf(builder, "\n")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func percent(value float64) string {
	return fmt.Sprintf("%.1f%%", value*100)
}

func seconds(value float64) string {
	return fmt.Sprintf("%.2fs", value)
}

func orNone(value string) string {
	if value == "" {
		return "default"
	}
	return value
}

func outsideText(files []string) string {
	if len(files) == 0 {
		return "none"
	}
	return strings.Join(files, " ")
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(value, "\n")
	return line
}

func truncate(value string, maximum int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) <= maximum {
		return value
	}
	return value[:maximum] + "..."
}
