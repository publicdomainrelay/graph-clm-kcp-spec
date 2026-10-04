package eval

import "fmt"

// Measure is one number and the sample count behind it. A measure with no
// samples is not measured: it has no value, and it is never reported as 0% or
// as 100%. Excluded counts the contexts a mean left out because they carried
// nothing to measure, so a mean over two of ten contexts cannot read as a mean
// over ten.
type Measure struct {
	Value float64 `json:"value"`

	Samples int `json:"samples"`

	Excluded int `json:"excluded,omitempty"`

	ExcludedNames []string `json:"excludedNames,omitempty"`
}

// Measured reports whether the measure has any sample at all.
func (m Measure) Measured() bool {
	return m.Samples > 0
}

// Text is the measure as a report prints it: the percent when it was measured,
// and the words "not measured" when it was not.
func (m Measure) Text() string {
	if !m.Measured() {
		return "not measured"
	}
	return fmt.Sprintf("%.1f%%", m.Value*100)
}

// SamplesText is the sample count as a report prints it.
func (m Measure) SamplesText() string {
	if !m.Measured() {
		return "0"
	}
	if m.Excluded > 0 {
		return fmt.Sprintf("%d (%d excluded)", m.Samples, m.Excluded)
	}
	return fmt.Sprintf("%d", m.Samples)
}

// measure averages the samples and records how many contexts were left out and
// by name, so a mean says what it is a mean of.
func measure(samples []float64, excluded []string) Measure {
	out := Measure{Samples: len(samples), Excluded: len(excluded)}
	if len(excluded) > 0 {
		out.ExcludedNames = append([]string{}, excluded...)
	}
	if len(samples) == 0 {
		return out
	}
	total := 0.0
	for _, value := range samples {
		total += value
	}
	out.Value = total / float64(len(samples))
	return out
}

// rate is measure() over booleans: the samples are 1 for true and 0 for false,
// so a pass rate carries the count it was taken over.
func rate(passed, total int, excluded []string) Measure {
	samples := make([]float64, 0, passed)
	for index := 0; index < passed; index++ {
		samples = append(samples, 1)
	}
	for index := passed; index < total; index++ {
		samples = append(samples, 0)
	}
	return measure(samples, excluded)
}
