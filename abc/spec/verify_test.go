package spec

import (
	"strings"
	"testing"
	"time"
)

func TestVerifySummaryOfAPassingRun(t *testing.T) {
	output := strings.Join([]string{
		"ok  \tgithub.com/x/calc/api/v1alpha1\t(cached)",
		"?   \tgithub.com/x/calc/cmd/tool\t[no test files]",
		"ok  \tgithub.com/x/calc/internal/denopod\t0.077s",
	}, "\n")
	summary := VerifySummary(0, 1234*time.Millisecond, output)
	for _, want := range []string{"exit 0", "1.234s", "2 ok", "1 without tests"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q: %s", want, summary)
		}
	}
	if strings.Contains(summary, "cached") || strings.Contains(summary, "github.com/x/calc") {
		t.Errorf("summary carries the raw output: %s", summary)
	}
}

func TestVerifySummaryOfAFailingRunNamesTheFailingTest(t *testing.T) {
	output := strings.Join([]string{
		"ok  \tgithub.com/x/calc/api/v1alpha1\t0.010s",
		"--- FAIL: TestSubtract (0.00s)",
		"    calc_test.go:12: Subtract(5, 3) = 8, want 2",
		"FAIL",
		"FAIL\tgithub.com/x/calc/calc\t0.032s",
		"FAIL",
	}, "\n")
	summary := VerifySummary(1, 400*time.Millisecond, output)
	for _, want := range []string{"exit 1", "400ms", "1 ok", "1 failed", "failing: TestSubtract", "Subtract(5, 3) = 8"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q: %s", want, summary)
		}
	}
}

func TestVerifySummaryBoundsTheFailureDetail(t *testing.T) {
	output := strings.Repeat("noise line that says nothing about the failure\n", 200) + "--- FAIL: TestLast (0.00s)\n"
	summary := VerifySummary(1, time.Second, output)
	if len(summary) > 2400 {
		t.Errorf("summary is %d bytes, want it bounded", len(summary))
	}
	if !strings.Contains(summary, "TestLast") {
		t.Errorf("the bounded summary lost the last line: %s", summary)
	}
}
