package specd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

func TestChangeAgentLogKeepsTheReportAndSummarizesVerify(t *testing.T) {
	result := realize.Result{
		Agent:          agent.RealizeResult{Log: "**Edited** apply.sh.\n⚠ claude.ai connectors are disabled because a key is set\n[claude-code:unrecognized_model] {\"model\":\"x\"}"},
		VerifyExitCode: 0,
		VerifyDuration: 1500 * time.Millisecond,
		VerifyOutput:   "ok  \tgithub.com/x/calc\t(cached)\nok  \tgithub.com/x/more\t0.02s\n",
	}
	log := changeAgentLog(result, true)
	if !strings.Contains(log, "**Edited** apply.sh.") {
		t.Errorf("the agent's report was dropped:\n%s", log)
	}
	if !strings.Contains(log, "verify: exit 0, 1.5s, 2 ok") {
		t.Errorf("the verify summary is missing:\n%s", log)
	}
	for _, unwanted := range []string{"connectors are disabled", "[claude-code:", "(cached)"} {
		if strings.Contains(log, unwanted) {
			t.Errorf("the record still carries %q:\n%s", unwanted, log)
		}
	}
}

func TestChangeAgentLogWithoutAVerifyCommand(t *testing.T) {
	result := realize.Result{Agent: agent.RealizeResult{Log: "**Edited** calc.go."}}
	if log := changeAgentLog(result, false); log != "**Edited** calc.go." {
		t.Errorf("log = %q, want the report alone", log)
	}
}

func TestWriteFullLogKeepsTheRawLogInTheStateDir(t *testing.T) {
	dir := t.TempDir()
	controller := &Controller{opts: Options{LogDir: dir}, log: logging.Discard()}
	result := realize.Result{
		Agent:        agent.RealizeResult{Log: "raw agent log\n[claude-code:noise]"},
		VerifyOutput: "ok  \tgithub.com/x/calc\t(cached)",
	}
	controller.writeFullLog("calc-s2c-1", result)
	data, err := os.ReadFile(filepath.Join(dir, "calc-s2c-1.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"raw agent log", "[claude-code:noise]", "(cached)"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the full log lacks %q:\n%s", want, data)
		}
	}
}

func TestWriteFullLogIsSkippedWithoutALogDir(t *testing.T) {
	controller := &Controller{log: logging.Discard()}
	controller.writeFullLog("calc-s2c-1", realize.Result{Agent: agent.RealizeResult{Log: "x"}})
}
