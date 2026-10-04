package agent

import (
	"strings"
	"testing"
)

func TestReportStripsHarnessNoise(t *testing.T) {
	log := strings.Join([]string{
		"**Edited** apply.sh and the README.",
		"",
		"⚠ claude.ai connectors are disabled because ANTHROPIC_API_KEY or another auth source is set and takes precedence over your claude.ai login · Unset it to load your organization's connectors",
		`[claude-code:unrecognized_model] {"model":"deepseek-v4-flash","query_source":"sdk"}`,
		"ok  \tgithub.com/x/calc\t(cached)",
	}, "\n")
	report := Report(log)
	if !strings.Contains(report, "**Edited** apply.sh and the README.") {
		t.Errorf("the report dropped the agent's own text:\n%s", report)
	}
	if !strings.Contains(report, "ok  \tgithub.com/x/calc\t(cached)") {
		t.Errorf("the report dropped a line that is not harness chatter:\n%s", report)
	}
	for _, unwanted := range []string{"connectors are disabled", "[claude-code:", "⚠"} {
		if strings.Contains(report, unwanted) {
			t.Errorf("the report kept %q:\n%s", unwanted, report)
		}
	}
}

func TestReportOfNothingIsEmpty(t *testing.T) {
	if Report("") != "" || Report("\n\n") != "" {
		t.Error("an empty log must give an empty report")
	}
	if got := Report("\n[claude-code:x]\n"); got != "" {
		t.Errorf("a log of nothing but noise gives %q", got)
	}
}
