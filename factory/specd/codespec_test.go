package specd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const calcScenario = `
contexts:
  calc:
    summary: The calc package adds and multiplies two integers.
    intent: Arithmetic over two integers.
    requirements:
      - id: r.add
        level: MUST
        text: Add adds two integers.
        codeRefs: ["function:add"]
      - id: r.subtract
        level: SHOULD
        text: Subtract subtracts two integers.
        codeRefs: ["function:subtract"]
    interfaces:
      - name: Add
        kind: function
        signature: "func Add(a, b int) int"
        file: calc/calc.go
      - name: Subtract
        kind: function
        signature: "func Subtract(a, b int) int"
        file: calc/calc.go
`

const emptyScenario = `
contexts: {}
`

func agentController(t *testing.T, cluster Cluster, scenario string) *Controller {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(path, []byte(scenario), 0o644); err != nil {
		t.Fatal(err)
	}
	factory, err := agentfactory.New(agentfactory.Options{Kind: agentfactory.Scripted + ":" + path})
	if err != nil {
		t.Fatal(err)
	}
	controller := testController(cluster)
	controller.agents = factory
	controller.opts.MaxAttempts = DefaultMaxAttempts
	controller.opts.RetryBackoff = time.Hour
	return controller
}

func driftedCalc(t *testing.T, cluster *fakeCluster) string {
	t.Helper()
	dir := t.TempDir()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: dir, Branch: "main"},
	})
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Requirements = []spec.Requirement{{ID: "r.add", Level: spec.LevelMust, Text: "Add adds."}}
		systemContext.Status.Observed = spec.ObservedFacts{
			Files: []string{"calc/calc.go"},
			Interfaces: []spec.ObservedInterface{
				{Name: "Add", Kind: "function", CodegraphID: "function:add", File: "calc/calc.go", Line: 3},
				{Name: "Subtract", Kind: "function", CodegraphID: "function:subtract", File: "calc/calc.go", Line: 8},
			},
			Fingerprint: "f2",
		}
		systemContext.Status.ObservedCommit = "c2"
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.SyncedCommit = "c1"
		systemContext.Status.Conditions = []metav1.Condition{{
			Type: specapi.ConditionDrifted, Status: metav1.ConditionTrue, Reason: specapi.ReasonFingerprintChanged,
		}}
	}))
	return dir
}

func pendingCodeToSpec(t *testing.T, cluster *fakeCluster, name string) {
	t.Helper()
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c2",
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	})
}

func readContext(t *testing.T, cluster *fakeCluster) *spec.SystemContext {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	return systemContext
}

func readChange(t *testing.T, cluster *fakeCluster, name string) *spec.SpecChange {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change, ok := typed.(*spec.SpecChange)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	return change
}

func TestCodeToSpecWritesTheSpecAndEndsTheEpisode(t *testing.T) {
	cluster := newFakeCluster()
	dir := driftedCalc(t, cluster)
	pendingCodeToSpec(t, cluster, "calc-c2s-c1-c2")
	controller := agentController(t, cluster, calcScenario)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-c2s-c1-c2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".specs")); err == nil {
		t.Fatal("the summarize wrote a spec artefact into the project tree")
	}

	change := readChange(t, cluster, "calc-c2s-c1-c2")
	if change.Status.Phase != specapi.PhaseSucceeded {
		t.Fatalf("phase = %q, want Succeeded: %s", change.Status.Phase, change.Status.Message)
	}
	if !strings.Contains(change.Status.Message, "2 requirement(s)") {
		t.Errorf("message = %q", change.Status.Message)
	}
	if !strings.Contains(change.Status.AgentLog, "calc package") {
		t.Errorf("agentLog = %q, want the model zone prose", change.Status.AgentLog)
	}

	updated := readContext(t, cluster)
	if updated.Spec.Intent != "Arithmetic over two integers." {
		t.Errorf("intent = %q", updated.Spec.Intent)
	}
	if len(updated.Spec.Requirements) != 2 || len(updated.Spec.Interfaces) != 2 {
		t.Fatalf("spec = %+v", updated.Spec)
	}
	if updated.Annotations[specapi.OriginAnnotation] != specapi.OriginIngest {
		t.Errorf("annotations = %v, want the ingest origin", updated.Annotations)
	}

	hash, err := spec.HashSystemContextSpec(updated.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status.RealizedSpecHash != hash {
		t.Errorf("realizedSpecHash = %q, want the hash of the spec that was written", updated.Status.RealizedSpecHash)
	}
	if updated.Status.SyncedFingerprint != "f2" || updated.Status.SyncedCommit != "c2" {
		t.Errorf("baseline = %s/%s, want the observed facts",
			updated.Status.SyncedFingerprint, updated.Status.SyncedCommit)
	}
	if drifted := conditionStatus(t, mustUnstructured(t, updated), specapi.ConditionDrifted); drifted != "False" {
		t.Errorf("Drifted = %s, want False", drifted)
	}

	document := contextDocPath("calc", "calc")
	contents, err := os.ReadFile(document)
	if err != nil {
		t.Fatalf("the context document was not written: %v", err)
	}
	if !strings.Contains(string(contents), "calc package") || !strings.Contains(string(contents), "function:add") {
		t.Errorf("document = %q", contents)
	}
}

func TestCodeToSpecDoesNotRaiseTheOppositeDirection(t *testing.T) {
	cluster := newFakeCluster()
	driftedCalc(t, cluster)
	pendingCodeToSpec(t, cluster, "calc-c2s-c1-c2")
	controller := agentController(t, cluster, calcScenario)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-c2s-c1-c2"); err != nil {
		t.Fatal(err)
	}
	for round := range 3 {
		if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
			t.Fatal(err)
		}
		names := cluster.names(specapi.SpecChangeGVR)
		if len(names) != 1 {
			t.Fatalf("round %d: spec changes = %v, want the one that ran", round, names)
		}
	}

	updated := readContext(t, cluster)
	if updated.Status.RealizedSpecHash == "" {
		t.Error("the realized hash was lost")
	}
	if got := conditionStatus(t, mustUnstructured(t, updated), specapi.ConditionDrifted); got != "False" {
		t.Errorf("Drifted = %s, want False", got)
	}
}

func TestCodeToSpecFailsWithoutAnAgentAndAsksForAnotherAttempt(t *testing.T) {
	cluster := newFakeCluster()
	driftedCalc(t, cluster)
	pendingCodeToSpec(t, cluster, "calc-c2s-c1-c2")
	controller := agentController(t, cluster, emptyScenario)
	controller.opts.RetryBackoff = 0

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-c2s-c1-c2"); err != nil {
		t.Fatal(err)
	}
	change := readChange(t, cluster, "calc-c2s-c1-c2")
	if change.Status.Phase != specapi.PhaseFailed {
		t.Fatalf("phase = %q, want Failed", change.Status.Phase)
	}
	if !strings.Contains(change.Status.Message, "no draft for calc") {
		t.Errorf("message = %q", change.Status.Message)
	}
	updated := readContext(t, cluster)
	if updated.Spec.Intent != "" {
		t.Errorf("intent = %q, want the spec left alone", updated.Spec.Intent)
	}

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	names := cluster.names(specapi.SpecChangeGVR)
	if strings.Join(names, ",") != "calc-c2s-c1-c2,calc-c2s-c1-c2-a2" {
		t.Fatalf("spec changes = %v, want a second attempt", names)
	}
}

func TestCodeToSpecStopsAtTheAttemptCap(t *testing.T) {
	cluster := newFakeCluster()
	driftedCalc(t, cluster)
	for _, name := range []string{"calc-c2s-c1-c2", "calc-c2s-c1-c2-a2", "calc-c2s-c1-c2-a3"} {
		apply(t, cluster, &spec.SpecChange{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
			Spec: spec.SpecChangeSpec{
				SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c2",
			},
			Status: spec.SpecChangeStatus{Phase: specapi.PhaseFailed},
		})
	}
	controller := agentController(t, cluster, calcScenario)

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	names := cluster.names(specapi.SpecChangeGVR)
	if len(names) != 3 {
		t.Fatalf("spec changes = %v, want the three failed attempts and no fourth", names)
	}
}

func TestAManualRetryRunsPastTheAttemptCap(t *testing.T) {
	cluster := newFakeCluster()
	driftedCalc(t, cluster)
	for _, name := range []string{"calc-c2s-c1-c2", "calc-c2s-c1-c2-a2", "calc-c2s-c1-c2-a3"} {
		apply(t, cluster, &spec.SpecChange{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
			Spec: spec.SpecChangeSpec{
				SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c2",
			},
			Status: spec.SpecChangeStatus{Phase: specapi.PhaseFailed},
		})
	}
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-c1-c2-a4", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c2",
		},
		Status: spec.SpecChangeStatus{
			Phase:       specapi.PhasePending,
			Attempt:     4,
			RetryReason: "the provider is fixed now",
			RetryBy:     "operator",
		},
	})
	controller := agentController(t, cluster, calcScenario)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-c2s-c1-c2-a4"); err != nil {
		t.Fatal(err)
	}
	change := readChange(t, cluster, "calc-c2s-c1-c2-a4")
	if change.Status.Phase != specapi.PhaseSucceeded {
		t.Fatalf("phase = %q, want the deliberate retry to run past the cap: %s", change.Status.Phase, change.Status.Message)
	}
}

func TestCodeToSpecWaitsForTheBackoff(t *testing.T) {
	cluster := newFakeCluster()
	driftedCalc(t, cluster)
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-c1-c2", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c2",
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseFailed},
	})
	setCreated(t, cluster, specapi.SpecChangeGVR, "calc-c2s-c1-c2", time.Now())
	controller := agentController(t, cluster, calcScenario)
	controller.opts.RetryBackoff = time.Hour

	requeue, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	names := cluster.names(specapi.SpecChangeGVR)
	if len(names) != 1 {
		t.Fatalf("spec changes = %v, want no retry while the backoff holds", names)
	}
	if requeue <= 0 || requeue > time.Hour {
		t.Errorf("requeue = %s, want the rest of the backoff", requeue)
	}

	setCreated(t, cluster, specapi.SpecChangeGVR, "calc-c2s-c1-c2", time.Now().Add(-2*time.Hour))
	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	names = cluster.names(specapi.SpecChangeGVR)
	if strings.Join(names, ",") != "calc-c2s-c1-c2,calc-c2s-c1-c2-a2" {
		t.Fatalf("spec changes = %v, want the retry after the wait", names)
	}
}

func mustUnstructured(t *testing.T, systemContext *spec.SystemContext) *unstructured.Unstructured {
	t.Helper()
	object, err := kcpclient.Unstructured(systemContext)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func setCreated(t *testing.T, cluster *fakeCluster, gvr schema.GroupVersionResource, name string, created time.Time) {
	t.Helper()
	stored, ok := cluster.objects[objectKey(gvr, specapi.DefaultNamespace, name)]
	if !ok {
		t.Fatalf("no %s/%s to stamp", gvr.Resource, name)
	}
	stored.SetCreationTimestamp(metav1.NewTime(created))
}
