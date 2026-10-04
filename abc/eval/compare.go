package eval

import (
	"fmt"
	"math"
	"strings"
)

// equality is how close two measures must be to count as the same number. The
// measures are ratios of small counts, so a float that differs by more than
// this was computed from a different sample.
const equality = 1e-9

// MeasurePair is one measure of two runs side by side.
type MeasurePair struct {
	Name string `json:"name"`

	Baseline Measure `json:"baseline"`

	Live Measure `json:"live"`

	// Equal is true only when both runs measured the measure and reached the
	// same number.
	Equal bool `json:"equal"`
}

// Comparison is a live run against the scripted baseline. NotDiscriminating is
// the flag the phase 11 review asked for: the live model scored exactly the
// baseline on every measure both runs took, so the eval cannot tell them apart.
type Comparison struct {
	BaselineAgent string `json:"baselineAgent"`

	LiveAgent string `json:"liveAgent"`

	Measures []MeasurePair `json:"measures"`

	NotDiscriminating bool `json:"notDiscriminating"`

	Note string `json:"note"`
}

// Compare puts a live run beside the scripted baseline. A measure the live run
// took and the baseline could not is part of the comparison and counts as
// discriminating, because the baseline did not score it at all.
func Compare(baseline, live Report) Comparison {
	comparison := Comparison{BaselineAgent: orNone(baseline.Agent), LiveAgent: orNone(live.Agent)}
	names := measureNames()
	comparable := 0
	equal := 0
	liveOnly := 0
	for _, name := range names {
		pair := MeasurePair{Name: name, Baseline: measureNamed(baseline, name), Live: measureNamed(live, name)}
		switch {
		case pair.Baseline.Measured() && pair.Live.Measured():
			comparable++
			pair.Equal = math.Abs(pair.Baseline.Value-pair.Live.Value) <= equality
			if pair.Equal {
				equal++
			}
		case pair.Live.Measured() && !pair.Baseline.Measured():
			liveOnly++
		}
		comparison.Measures = append(comparison.Measures, pair)
	}
	switch {
	case comparable == 0:
		comparison.Note = "no measure was taken by both runs, so there is nothing to compare"
	case equal == comparable && liveOnly == 0:
		comparison.NotDiscriminating = true
		comparison.Note = "the live run is equal to the scripted baseline on every measure both took: the eval does not discriminate"
	case equal == comparable:
		comparison.Note = fmt.Sprintf("equal on every shared measure, but the live run measured %d measure(s) the baseline did not", liveOnly)
	default:
		comparison.Note = fmt.Sprintf("%d of %d shared measure(s) differ", comparable-equal, comparable)
	}
	return comparison
}

// measureNames is every measure a report carries, in the order the headline
// table prints them, so a comparison reads like the report above it.
func measureNames() []string {
	return []string{
		"spec -> code pass rate (verify + acceptance)",
		"spec -> code via CLM",
		"delta precision (entries as intended)",
		"code -> spec facts stated (judged)",
		"spec sufficiency (rebuild from spec, original tests)",
		"drift (code -> spec from a human edit)",
		"interface recall",
		"interface precision",
		"interface F1",
		"requirement anchoring",
		"validator pass",
		"round trip interface Jaccard",
		"fixtures reaching Populated",
	}
}

func measureNamed(report Report, name string) Measure {
	switch name {
	case "spec -> code pass rate (verify + acceptance)":
		return report.PassRate("")
	case "spec -> code via CLM":
		return report.PassRate("clm")
	case "delta precision (entries as intended)":
		return report.DeltaPrecisionRate()
	case "code -> spec facts stated (judged)":
		return report.FactsScore()
	case "spec sufficiency (rebuild from spec, original tests)":
		return report.SufficiencyPassRate()
	case "drift (code -> spec from a human edit)":
		return report.DriftPassRate()
	case "interface recall":
		return report.MeanRecall()
	case "interface precision":
		return report.MeanPrecision()
	case "interface F1":
		return report.MeanF1()
	case "requirement anchoring":
		return report.MeanAnchoring()
	case "validator pass":
		return report.ValidatorPassRate()
	case "round trip interface Jaccard":
		return report.MeanRoundTripJaccard()
	case "fixtures reaching Populated":
		return report.PopulatedFixtures()
	}
	return Measure{}
}

// Markdown is the side by side table the report ends with.
func (c Comparison) Markdown() string {
	builder := &strings.Builder{}
	fmt.Fprintf(builder, "# specctl eval comparison\n\n")
	fmt.Fprintf(builder, "- baseline agent: `%s`\n", c.BaselineAgent)
	fmt.Fprintf(builder, "- live agent: `%s`\n", c.LiveAgent)
	flag := "no"
	if c.NotDiscriminating {
		flag = "**yes**"
	}
	fmt.Fprintf(builder, "- not discriminating: %s\n", flag)
	fmt.Fprintf(builder, "- %s\n\n", c.Note)
	fmt.Fprintf(builder, "| measure | scripted | live | equal |\n| --- | --- | --- | --- |\n")
	for _, pair := range c.Measures {
		fmt.Fprintf(builder, "| %s | %s | %s | %s |\n", pair.Name, pair.Baseline.Text(), pair.Live.Text(), yesNo(pair.Equal))
	}
	fmt.Fprintf(builder, "\n")
	return builder.String()
}
