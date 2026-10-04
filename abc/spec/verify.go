package spec

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	verifyDetailBytes = 2000

	verifyTestPrefix = "--- FAIL: "
)

// VerifySummary replaces the raw verify output in a change record: the exit
// code, how long it took, how the packages ended, and the failing tests when
// there are any, with a bounded tail of the output that names what failed.
func VerifySummary(exitCode int, duration time.Duration, output string) string {
	ok, failed, noTests, tests := parseVerify(output)
	parts := []string{
		fmt.Sprintf("exit %d", exitCode),
		duration.Round(time.Millisecond).String(),
		fmt.Sprintf("%d ok", ok),
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if noTests > 0 {
		parts = append(parts, fmt.Sprintf("%d without tests", noTests))
	}
	if len(tests) > 0 {
		parts = append(parts, "failing: "+strings.Join(tests, ", "))
	}
	summary := "verify: " + strings.Join(parts, ", ")
	if exitCode == 0 {
		return summary
	}
	if detail := tailBytes(strings.TrimSpace(output), verifyDetailBytes); detail != "" {
		summary += "\n" + detail
	}
	return summary
}

func parseVerify(output string) (int, int, int, []string) {
	ok, failed, noTests := 0, 0, 0
	tests := []string{}
	for _, line := range strings.Split(output, "\n") {
		if name, found := strings.CutPrefix(line, verifyTestPrefix); found {
			if fields := strings.Fields(name); len(fields) > 0 {
				tests = append(tests, fields[0])
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "ok":
			ok++
		case "?":
			noTests++
		case "FAIL":
			failed++
		}
	}
	sort.Strings(tests)
	return ok, failed, noTests, dedupe(tests)
}

func dedupe(values []string) []string {
	out := values[:0]
	for index, value := range values {
		if index > 0 && value == values[index-1] {
			continue
		}
		out = append(out, value)
	}
	return out
}

func tailBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	tail := text[len(text)-limit:]
	if index := strings.Index(tail, "\n"); index >= 0 {
		tail = tail[index+1:]
	}
	return "..." + tail
}
