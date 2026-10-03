package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// PopulateReport is what one Repository manifest reached: the phase specd
// settled on, the context tally, and how long it took.
type PopulateReport struct {
	Fixture string `json:"fixture"`

	Phase string `json:"phase"`

	Contexts int `json:"contexts"`

	Summarized int `json:"summarized"`

	Failed int `json:"failed"`

	WallTimeSeconds float64 `json:"wallTimeSeconds"`

	Error string `json:"error,omitempty"`
}

// CodeToSpecReport is one context after the code -> spec half ran: what the
// index observed, what the spec declares, and whether the claims are anchored.
type CodeToSpecReport struct {
	Fixture string `json:"fixture"`

	Context string `json:"context"`

	Score Score `json:"interfaces"`

	Requirements int `json:"requirements"`

	AnchoringRate float64 `json:"anchoringRate"`

	ValidatorPass bool `json:"validatorPass"`

	DroppedRefs int `json:"droppedRefs"`

	RoundTripJaccard float64 `json:"roundTripJaccard"`

	RequirementCountDelta int `json:"requirementCountDelta"`
}

// ScenarioReport is one scenario after the spec -> code half ran.
type ScenarioReport struct {
	Fixture string `json:"fixture"`

	Scenario string `json:"scenario"`

	Context string `json:"context"`

	Difficulty int `json:"difficulty"`

	Description string `json:"description"`

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

// Report is one eval run: the fixtures it visited, the numbers it measured and
// the agent that produced them.
type Report struct {
	Agent string `json:"agent"`

	Fixtures string `json:"fixtures"`

	StartedAt string `json:"startedAt"`

	FinishedAt string `json:"finishedAt"`

	Populate []PopulateReport `json:"populate"`

	CodeToSpec []CodeToSpecReport `json:"codeToSpec"`

	Scenarios []ScenarioReport `json:"scenarios"`

	Notes []string `json:"notes,omitempty"`
}

// ScenarioPassRate is the share of scenarios whose verify command and hidden
// acceptance tests both passed.
func (r Report) ScenarioPassRate() float64 {
	if len(r.Scenarios) == 0 {
		return 1
	}
	passed := 0
	for _, scenario := range r.Scenarios {
		if scenario.Pass {
			passed++
		}
	}
	return ratio(passed, len(r.Scenarios))
}

// MeanRecall, MeanPrecision and MeanAnchoring are the code -> spec numbers
// averaged over the contexts that were measured.
func (r Report) MeanRecall() float64 { return r.meanScore(func(s Score) float64 { return s.Recall }) }

func (r Report) MeanPrecision() float64 {
	return r.meanScore(func(s Score) float64 { return s.Precision })
}

func (r Report) MeanF1() float64 { return r.meanScore(func(s Score) float64 { return s.F1 }) }

func (r Report) MeanAnchoring() float64 {
	if len(r.CodeToSpec) == 0 {
		return 1
	}
	total := 0.0
	for _, entry := range r.CodeToSpec {
		total += entry.AnchoringRate
	}
	return total / float64(len(r.CodeToSpec))
}

func (r Report) ValidatorPassRate() float64 {
	if len(r.CodeToSpec) == 0 {
		return 1
	}
	passed := 0
	for _, entry := range r.CodeToSpec {
		if entry.ValidatorPass {
			passed++
		}
	}
	return ratio(passed, len(r.CodeToSpec))
}

func (r Report) MeanRoundTripJaccard() float64 {
	if len(r.CodeToSpec) == 0 {
		return 1
	}
	total := 0.0
	for _, entry := range r.CodeToSpec {
		total += entry.RoundTripJaccard
	}
	return total / float64(len(r.CodeToSpec))
}

// DeltaPrecisionRate is the share of scenarios whose change carried exactly the
// entry count the scenario intended.
func (r Report) DeltaPrecisionRate() float64 {
	if len(r.Scenarios) == 0 {
		return 1
	}
	precise := 0
	for _, scenario := range r.Scenarios {
		if scenario.Delta.Precise {
			precise++
		}
	}
	return ratio(precise, len(r.Scenarios))
}

func (r Report) PopulatedFixtures() float64 {
	if len(r.Populate) == 0 {
		return 1
	}
	populated := 0
	for _, entry := range r.Populate {
		if entry.Phase == "Populated" {
			populated++
		}
	}
	return ratio(populated, len(r.Populate))
}

func (r Report) meanScore(pick func(Score) float64) float64 {
	if len(r.CodeToSpec) == 0 {
		return 1
	}
	total := 0.0
	for _, entry := range r.CodeToSpec {
		total += pick(entry.Score)
	}
	return total / float64(len(r.CodeToSpec))
}

// JSON is the machine readable report, indented so a diff of two runs reads.
func (r Report) JSON() ([]byte, error) {
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("eval: encode the report: %w", err)
	}
	return append(encoded, '\n'), nil
}

// Markdown is the human readable report: a headline table of the measures the
// plan names, then one table per half of the loop.
func (r Report) Markdown() string {
	builder := &strings.Builder{}
	fmt.Fprintf(builder, "# specctl eval report\n\n")
	fmt.Fprintf(builder, "- agent: `%s`\n", orNone(r.Agent))
	fmt.Fprintf(builder, "- fixtures: `%s`\n", r.Fixtures)
	fmt.Fprintf(builder, "- started: %s\n", r.StartedAt)
	fmt.Fprintf(builder, "- finished: %s\n", r.FinishedAt)
	fmt.Fprintf(builder, "- scenarios: %d\n\n", len(r.Scenarios))

	fmt.Fprintf(builder, "## Measures\n\n")
	fmt.Fprintf(builder, "| measure | value |\n| --- | --- |\n")
	fmt.Fprintf(builder, "| spec -> code pass rate (verify + acceptance) | %s |\n", percent(r.ScenarioPassRate()))
	fmt.Fprintf(builder, "| delta precision (entries as intended) | %s |\n", percent(r.DeltaPrecisionRate()))
	fmt.Fprintf(builder, "| interface recall | %s |\n", percent(r.MeanRecall()))
	fmt.Fprintf(builder, "| interface precision | %s |\n", percent(r.MeanPrecision()))
	fmt.Fprintf(builder, "| interface F1 | %s |\n", percent(r.MeanF1()))
	fmt.Fprintf(builder, "| requirement anchoring | %s |\n", percent(r.MeanAnchoring()))
	fmt.Fprintf(builder, "| validator pass | %s |\n", percent(r.ValidatorPassRate()))
	fmt.Fprintf(builder, "| round trip interface Jaccard | %s |\n", percent(r.MeanRoundTripJaccard()))
	fmt.Fprintf(builder, "| fixtures reaching Populated | %s |\n", percent(r.PopulatedFixtures()))
	fmt.Fprintf(builder, "\n")

	r.writePopulate(builder)
	r.writeCodeToSpec(builder)
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
		fmt.Fprintf(builder, "| %s | %s | %d | %d | %s | %s | %s | %s | %s | %+d |\n",
			entry.Fixture, entry.Context, entry.Score.Target, entry.Score.Declared,
			percent(entry.Score.Recall), percent(entry.Score.Precision), percent(entry.AnchoringRate),
			yesNo(entry.ValidatorPass), percent(entry.RoundTripJaccard), entry.RequirementCountDelta)
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
	fmt.Fprintf(builder, "| fixture | scenario | level | context | pass | verify | acceptance | delta | attempts | outside | progress | wall time |\n")
	fmt.Fprintf(builder, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range sorted {
		delta := fmt.Sprintf("%d/%d", entry.Delta.Entries, entry.Delta.Expected)
		pass := yesNo(entry.Pass)
		if entry.Error != "" {
			pass = "no: " + firstLine(entry.Error)
		}
		fmt.Fprintf(builder, "| %s | %s | %d | %s | %s | %s | %s | %s | %d | %s | %d | %s |\n",
			entry.Fixture, entry.Scenario, entry.Difficulty, entry.Context, pass,
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
