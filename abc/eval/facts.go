package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

type Fact struct {
	ID string `json:"id"`

	Text string `json:"text"`

	Must []Keyword `json:"must,omitempty"`
}

type Keyword []string

func (k *Keyword) UnmarshalJSON(data []byte) error {
	single := ""
	if err := json.Unmarshal(data, &single); err == nil {
		*k = Keyword{single}
		return nil
	}
	many := []any{}
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("eval: a keyword is a string or a list of strings: %w", err)
	}
	out := make(Keyword, 0, len(many))
	for _, entry := range many {
		switch value := entry.(type) {
		case string:
			out = append(out, value)
		case float64:
			out = append(out, strconv.FormatFloat(value, 'f', -1, 64))
		default:
			return fmt.Errorf("eval: a keyword is a string or a list of strings, not %T", entry)
		}
	}
	*k = out
	return nil
}

func (k Keyword) Match(text string) bool {
	for _, alternative := range k {
		if alternative == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(alternative)) {
			return true
		}
	}
	return false
}

type JudgeRequest struct {
	Context string

	Facts []Fact

	Spec string
}

type JudgeVerdict struct {
	ID string `json:"id"`

	Stated bool `json:"stated"`

	Evidence string `json:"evidence,omitempty"`
}

type JudgeResult struct {
	Verdicts []JudgeVerdict

	Raw string
}

type Judge interface {
	Judge(ctx context.Context, request JudgeRequest) (JudgeResult, error)
}

type KeywordJudge struct{}

func (KeywordJudge) Judge(_ context.Context, request JudgeRequest) (JudgeResult, error) {
	folded := strings.ToLower(request.Spec)
	result := JudgeResult{}
	for _, fact := range request.Facts {
		stated := len(fact.Must) > 0
		evidence := ""
		for _, keyword := range fact.Must {
			if !keyword.Match(folded) {
				stated = false
				break
			}
			if evidence == "" {
				evidence = keyword[0]
			}
		}
		result.Verdicts = append(result.Verdicts, JudgeVerdict{ID: fact.ID, Stated: stated, Evidence: evidence})
	}
	return result, nil
}

type FactVerdict struct {
	ID string `json:"id"`

	Text string `json:"text"`

	Stated bool `json:"stated"`

	Judge string `json:"judge"`

	Evidence string `json:"evidence,omitempty"`

	Reason string `json:"reason,omitempty"`
}

type FactsReport struct {
	Fixture string `json:"fixture"`

	Context string `json:"context"`

	Verdicts []FactVerdict `json:"verdicts"`

	Stated int `json:"stated"`

	Expected int `json:"expected"`

	Score float64 `json:"score"`

	Error string `json:"error,omitempty"`
}

func SpecProse(declared spec.SystemContextSpec) string {
	builder := &strings.Builder{}
	builder.WriteString(declared.Intent)
	builder.WriteString("\n")
	for _, requirement := range declared.Requirements {
		builder.WriteString(requirement.Text)
		builder.WriteString("\n")
	}
	return builder.String()
}

func ScoreFacts(fixture, contextName string, facts []Fact, result JudgeResult, judge string) FactsReport {
	report := FactsReport{Fixture: fixture, Context: contextName, Expected: len(facts)}
	byID := map[string]JudgeVerdict{}
	for _, verdict := range result.Verdicts {
		byID[verdict.ID] = verdict
	}
	sorted := append([]Fact{}, facts...)
	sort.SliceStable(sorted, func(left, right int) bool { return sorted[left].ID < sorted[right].ID })
	for _, fact := range sorted {
		entry := FactVerdict{ID: fact.ID, Text: fact.Text, Judge: judge}
		verdict, found := byID[fact.ID]
		switch {
		case !found:
			entry.Reason = "the judge did not answer for this fact"
		default:
			entry.Stated = verdict.Stated
			entry.Evidence = verdict.Evidence
		}
		if entry.Stated {
			report.Stated++
		}
		report.Verdicts = append(report.Verdicts, entry)
	}
	report.Score = ratio(report.Stated, report.Expected)
	return report
}
