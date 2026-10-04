package agent

import "strings"

// The model harness prints lines about itself, not about the change: sign-in
// notices and model warnings. They are not part of the agent's report.
var harnessNoisePrefixes = []string{"[claude-code:"}

var harnessNoisePhrases = []string{"claude.ai connectors are disabled"}

// Report is the agent's own report, with the harness's own chatter removed.
// The full log, noise included, is kept in the state dir.
func Report(log string) string {
	if log == "" {
		return ""
	}
	lines := strings.Split(log, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if noise(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func noise(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range harnessNoisePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	for _, phrase := range harnessNoisePhrases {
		if strings.Contains(trimmed, phrase) {
			return true
		}
	}
	return false
}
