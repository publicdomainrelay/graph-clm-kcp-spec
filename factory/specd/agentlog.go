package specd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
)

// changeAgentLog is what a change record carries: the agent's own report with
// the harness's chatter removed, and the verify summary instead of the raw
// verify output. A repository with no verify command gets no verify line.
func changeAgentLog(result realize.Result, verifyConfigured bool) string {
	report := agent.Report(result.Agent.Log)
	if result.Policy != nil && len(result.Policy.Denied) > 0 {
		denied := policyDeniedMessage(result.Policy.Denied)
		if report == "" {
			return denied
		}
		return report + "\n\n" + denied
	}
	if !verifyConfigured {
		return report
	}
	summary := spec.VerifySummary(result.VerifyExitCode, result.VerifyDuration, result.VerifyOutput)
	if report == "" {
		return summary
	}
	return report + "\n\n" + summary
}

// policyDeniedMessage is what the next attempt is told: the constraints the
// gate refused the change for, and what each of them said.
func policyDeniedMessage(violations []spec.PolicyViolation) string {
	builder := strings.Builder{}
	builder.WriteString("The change failed the policy gate. Fix these before trying again:\n")
	for _, violation := range violations {
		fmt.Fprintf(&builder, "- %s: %s", violation.Constraint, violation.Msg)
		if violation.File != "" {
			fmt.Fprintf(&builder, " (%s:%d)", violation.File, violation.Line)
		}
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

// writeFullLog keeps the whole agent log and the raw verify output in the
// state dir, which is never committed. The change record keeps the report and
// the summary.
func (c *Controller) writeFullLog(change string, result realize.Result) {
	if c.opts.LogDir == "" || change == "" {
		return
	}
	if err := os.MkdirAll(c.opts.LogDir, 0o755); err != nil {
		c.log.Warn("could not keep the full agent log", "change", change, "err", err)
		return
	}
	path := filepath.Join(c.opts.LogDir, change+".log")
	body := result.Agent.Log + "\n" + result.VerifyOutput
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		c.log.Warn("could not keep the full agent log", "change", change, "path", path, "err", err)
	}
}
