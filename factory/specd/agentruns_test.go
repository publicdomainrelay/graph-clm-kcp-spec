package specd

import (
	"context"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func readAgentRun(t *testing.T, cluster *fakeCluster, name string) *spec.AgentRun {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.AgentRunGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := typed.(*spec.AgentRun)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	return record
}

func TestAgentRunSummaryRecordsTheRunOnTheChange(t *testing.T) {
	cluster := newFakeCluster()
	driftedCalc(t, cluster)
	pendingCodeToSpec(t, cluster, "calc-c2s-c1-c2")
	controller := agentController(t, cluster, calcScenario)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-c2s-c1-c2"); err != nil {
		t.Fatal(err)
	}

	change := readChange(t, cluster, "calc-c2s-c1-c2")
	if change.Status.Phase != specapi.PhaseSucceeded {
		t.Fatalf("phase = %q, want Succeeded: %s", change.Status.Phase, change.Status.Message)
	}
	if len(change.Status.AgentRuns) != 1 {
		t.Fatalf("agentRuns = %v, want the one summarize run", change.Status.AgentRuns)
	}
	name := change.Status.AgentRuns[0]
	if !strings.HasPrefix(name, "calc-c2s-c1-c2-summarize-") {
		t.Errorf("run = %q, want the change and the summarize purpose", name)
	}
	if runs := cluster.names(specapi.AgentRunGVR); len(runs) != 1 || runs[0] != name {
		t.Errorf("runs = %v, want the recorded run %q", runs, name)
	}

	record := readAgentRun(t, cluster, name)
	if record.Spec.Purpose != "summarize" || record.Spec.Change != "calc-c2s-c1-c2" {
		t.Errorf("spec = %+v, want the summarize of the change", record.Spec)
	}
	if record.Spec.Context != "calc" {
		t.Errorf("context = %q, want calc", record.Spec.Context)
	}
	if record.Status.Phase != specapi.PhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded: %s", record.Status.Phase, record.Status.Message)
	}
}
