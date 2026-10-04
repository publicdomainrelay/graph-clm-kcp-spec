package eval

import "fmt"

type Measure struct {
	Value float64 `json:"value"`

	Samples int `json:"samples"`

	Excluded int `json:"excluded,omitempty"`

	ExcludedNames []string `json:"excludedNames,omitempty"`
}

func (m Measure) Measured() bool {
	return m.Samples > 0
}

func (m Measure) Text() string {
	if !m.Measured() {
		return "not measured"
	}
	return fmt.Sprintf("%.1f%%", m.Value*100)
}

func (m Measure) SamplesText() string {
	if !m.Measured() {
		return "0"
	}
	if m.Excluded > 0 {
		return fmt.Sprintf("%d (%d excluded)", m.Samples, m.Excluded)
	}
	return fmt.Sprintf("%d", m.Samples)
}

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
