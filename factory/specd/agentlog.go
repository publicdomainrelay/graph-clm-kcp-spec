package specd

import (
	"os"
	"path/filepath"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
)

// changeAgentLog is what a change record carries: the agent's own report with
// the harness's chatter removed, and the verify summary instead of the raw
// verify output.
func changeAgentLog(result realize.Result) string {
	report := agent.Report(result.Agent.Log)
	summary := spec.VerifySummary(result.VerifyExitCode, result.VerifyDuration, result.VerifyOutput)
	if report == "" {
		return summary
	}
	return report + "\n\n" + summary
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
