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

// Fact is one behavioural statement a correct spec must state. It is what the
// code -> spec half is graded on: the interface list is handed to the model, so
// scoring the interface list measures nothing; a fact about what the code does
// is not in the bundle unless the model read the code.
type Fact struct {
	ID string `json:"id"`

	Text string `json:"text"`

	// Must is the deterministic fallback's reading of the fact: every entry has
	// to appear in the spec's prose, and an entry with alternatives is satisfied
	// by any one of them. An empty Must means the fallback cannot judge the
	// fact, and only a model verdict counts.
	Must []Keyword `json:"must,omitempty"`
}

// Keyword is one required term, or a set of alternatives of which one is
// enough. It reads from YAML as either a string or a list.
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
		// A YAML scalar that looks like a number arrives as one, and a fact
		// about "0" or "-1" is still a word to look for.
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

// Match reports whether the term is present: any alternative counts, and the
// comparison folds case and whitespace so a spec that says "Zero" or that wraps
// the word still matches.
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

// JudgeRequest is one grading call: the facts to look for in the prose a spec
// states. The prose is the intent and the requirement texts, which is where a
// behavioural claim lives.
type JudgeRequest struct {
	Context string

	Facts []Fact

	Spec string
}

// JudgeVerdict is one fact's verdict. Evidence is the sentence the judge read,
// so a verdict can be checked rather than trusted.
type JudgeVerdict struct {
	ID string `json:"id"`

	Stated bool `json:"stated"`

	Evidence string `json:"evidence,omitempty"`
}

// JudgeResult is one grading call's answer, with the raw answer kept so a
// parse failure can be reported as the model's words and not as a lost verdict.
type JudgeResult struct {
	Verdicts []JudgeVerdict

	Raw string
}

// Judge grades facts against a spec. The model implementation asks a model with
// a fixed rubric; the keyword implementation is deterministic and is used when
// the agent under test is the scripted baseline.
type Judge interface {
	Judge(ctx context.Context, request JudgeRequest) (JudgeResult, error)
}

// KeywordJudge is the deterministic fallback: a fact is stated when every
// required term is present in the prose. A fact with no terms is never stated
// by this judge, because a keyword judge that says yes to everything would make
// the score meaningless.
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

// FactVerdict is one fact's recorded verdict in a report. Judge names which
// judge answered, so a keyword pass and a model pass are never confused.
type FactVerdict struct {
	ID string `json:"id"`

	Text string `json:"text"`

	Stated bool `json:"stated"`

	Judge string `json:"judge"`

	Evidence string `json:"evidence,omitempty"`

	Reason string `json:"reason,omitempty"`
}

// FactsReport is one context's fact score: how many of the facts a correct spec
// must state the spec actually stated.
type FactsReport struct {
	Fixture string `json:"fixture"`

	Context string `json:"context"`

	Verdicts []FactVerdict `json:"verdicts"`

	Stated int `json:"stated"`

	Expected int `json:"expected"`

	Score float64 `json:"score"`

	Error string `json:"error,omitempty"`
}

// SpecProse is the part of a spec a behavioural claim can live in: the intent
// and the requirement texts. Interface names are left out on purpose, because
// they are handed to the model and grading them measures the prompt.
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

// ScoreFacts pairs the facts a fixture expects with the judge's verdicts. A
// fact the judge did not answer for is not stated, and the reason says so.
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
