package judge_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/judge"
)

// fake writes a command that answers every prompt with the given output, so the
// judge's request and parsing are tested without a model.
func fake(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-model.sh")
	script := "#!/bin/sh\ncat > /dev/null\ncat <<'ANSWER'\n" + output + "\nANSWER\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestJudgeReadsAVerdictPerFact(t *testing.T) {
	command := fake(t, "Sure, here it is:\n```json\n{\"verdicts\":[{\"id\":\"zero\",\"stated\":true,\"evidence\":\"Divide returns an error when b is 0\"},{\"id\":\"abs\",\"stated\":false}]}\n```")
	grader := judge.New(judge.Options{Command: command})
	facts := []eval.Fact{
		{ID: "zero", Text: "Divide rejects a zero divisor."},
		{ID: "abs", Text: "Abs is exported."},
	}
	result, err := grader.Judge(t.Context(), eval.JudgeRequest{Context: "calc", Facts: facts, Spec: "Divide returns an error when b is 0."})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Verdicts) != 2 {
		t.Fatalf("verdicts = %+v", result.Verdicts)
	}
	if !result.Verdicts[0].Stated || result.Verdicts[1].Stated {
		t.Fatalf("verdicts = %+v", result.Verdicts)
	}
	report := eval.ScoreFacts("calc", "calc", facts, result, "model")
	if report.Stated != 1 || report.Score != 0.5 {
		t.Fatalf("facts report = %+v", report)
	}
}

func TestJudgeRefusesAnAnswerThatIsNotJSON(t *testing.T) {
	grader := judge.New(judge.Options{Command: fake(t, "I could not decide.")})
	_, err := grader.Judge(t.Context(), eval.JudgeRequest{
		Context: "calc",
		Facts:   []eval.Fact{{ID: "zero", Text: "Divide rejects a zero divisor."}},
		Spec:    "Divide returns an error.",
	})
	if err == nil {
		t.Fatal("the judge accepted an answer with no verdicts")
	}
	if !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("err = %v", err)
	}
}

func TestPromptCarriesTheRubricAndTheFactsNotTheInterfaces(t *testing.T) {
	prompt := judge.Prompt(eval.JudgeRequest{
		Context: "calc",
		Facts:   []eval.Fact{{ID: "zero", Text: "Divide rejects a zero divisor."}},
		Spec:    "Divide returns an error when the divisor is zero.",
	})
	for _, want := range []string{"A fact counts as stated only when the specification's own words say it", "id: zero", "Divide rejects a zero divisor.", "Divide returns an error"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, prompt)
		}
	}
}
