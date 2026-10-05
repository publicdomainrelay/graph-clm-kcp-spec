package specd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func pendingChange(name, context string, at time.Time) *spec.SpecChange {
	return &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace, CreationTimestamp: metav1.NewTime(at)},
		Spec: spec.SpecChangeSpec{
			SystemContext: context,
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    "hash-" + name,
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	}
}

func readRepository(t *testing.T, cluster *fakeCluster, name string) *spec.Repository {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.RepositoryGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		t.Fatalf("%s is not a Repository", name)
	}
	return repository
}

func batchCluster(t *testing.T) *fakeCluster {
	t.Helper()
	cluster := newFakeCluster()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: "/does/not/matter", Branch: "main"},
	})
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
	}))
	apply(t, cluster, systemContext("cmd-calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
	}))
	apply(t, cluster, systemContext("other", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "other"
	}))
	return cluster
}

func TestPlanBatchGathersThePendingChangesOfOneRepository(t *testing.T) {
	cluster := batchCluster(t)
	base := time.Unix(100, 0)
	apply(t, cluster, pendingChange("calc-s2c-1", "calc", base))
	apply(t, cluster, pendingChange("cmd-calc-s2c-1", "cmd-calc", base))
	apply(t, cluster, pendingChange("other-s2c-1", "other", base))
	controller := testController(cluster)

	plan, err := controller.planBatch(context.Background(), specapi.DefaultNamespace,
		readChange(t, cluster, "calc-s2c-1"), readRepository(t, cluster, "calc"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, member := range plan.members {
		names = append(names, member.Name)
	}
	if strings.Join(names, ",") != "calc-s2c-1,cmd-calc-s2c-1" {
		t.Errorf("members = %v, want the repository's own pending changes, oldest first", names)
	}
	if plan.wait != 0 || plan.deferred {
		t.Errorf("plan = %+v, want an immediate batch", plan)
	}
}

func TestPlanBatchDefersToTheOldestPendingChange(t *testing.T) {
	cluster := batchCluster(t)
	base := time.Unix(100, 0)
	apply(t, cluster, pendingChange("calc-s2c-1", "calc", base))
	apply(t, cluster, pendingChange("cmd-calc-s2c-1", "cmd-calc", base.Add(time.Second)))
	controller := testController(cluster)

	plan, err := controller.planBatch(context.Background(), specapi.DefaultNamespace,
		readChange(t, cluster, "cmd-calc-s2c-1"), readRepository(t, cluster, "calc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.members) != 0 || !plan.deferred {
		t.Errorf("plan = %+v, want the follower to defer to calc-s2c-1", plan)
	}
}

func TestPlanBatchWaitsForTheGatherWindow(t *testing.T) {
	cluster := batchCluster(t)
	at := time.Now()
	apply(t, cluster, pendingChange("calc-s2c-1", "calc", at))
	controller := testController(cluster)
	controller.opts.BatchWindow = time.Minute

	plan, err := controller.planBatch(context.Background(), specapi.DefaultNamespace,
		readChange(t, cluster, "calc-s2c-1"), readRepository(t, cluster, "calc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.members) != 0 || plan.wait <= 0 {
		t.Errorf("plan = %+v, want the leader to wait out the window", plan)
	}
}

func TestPlanBatchWaitsForARunningRealizationOfTheRepository(t *testing.T) {
	cluster := batchCluster(t)
	base := time.Unix(100, 0)
	running := pendingChange("calc-s2c-0", "calc", base)
	running.Status.Phase = specapi.PhaseRunning
	apply(t, cluster, running)
	apply(t, cluster, pendingChange("cmd-calc-s2c-1", "cmd-calc", base.Add(time.Second)))
	controller := testController(cluster)

	plan, err := controller.planBatch(context.Background(), specapi.DefaultNamespace,
		readChange(t, cluster, "cmd-calc-s2c-1"), readRepository(t, cluster, "calc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.members) != 0 || !plan.deferred {
		t.Errorf("plan = %+v, want the repository busy", plan)
	}
}

func TestSiblingLandedSeesACommitAnotherChangeRecorded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", dir}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "first")
	head := run("rev-parse", "HEAD")

	cluster := newFakeCluster()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: dir, Branch: "main"},
	})
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
	}))
	landed := pendingChange("calc-s2c-1", "calc", time.Unix(100, 0))
	landed.Status.Phase = specapi.PhaseSucceeded
	landed.Status.Commit = head
	apply(t, cluster, landed)
	controller := testController(cluster)

	moved, recorded := controller.movedSinceBase(context.Background(), specapi.DefaultNamespace, readRepository(t, cluster, "calc"), "base", nil)
	if !moved || !recorded {
		t.Errorf("moved, recorded = %v, %v, want the branch moved by a recorded sibling", moved, recorded)
	}
	if moved, recorded := controller.movedSinceBase(context.Background(), specapi.DefaultNamespace, readRepository(t, cluster, "calc"), "base", []string{"calc-s2c-1"}); !moved || recorded {
		t.Errorf("moved, recorded = %v, %v, want the batch's own change not counted as a sibling", moved, recorded)
	}
	if moved, _ := controller.movedSinceBase(context.Background(), specapi.DefaultNamespace, readRepository(t, cluster, "calc"), head, nil); moved {
		t.Error("a branch that did not move was reported as moved")
	}
}

func TestPlanBatchOrdersADependentContextAfterItsDependency(t *testing.T) {
	cluster := batchCluster(t)
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
		systemContext.Spec.DependsOn = []string{"sc.cmd-calc"}
	}))
	base := time.Unix(100, 0)
	apply(t, cluster, pendingChange("calc-s2c-1", "calc", base))
	apply(t, cluster, pendingChange("cmd-calc-s2c-1", "cmd-calc", base.Add(time.Second)))
	controller := testController(cluster)
	controller.opts.BatchWindow = 0

	plan, err := controller.planBatch(context.Background(), specapi.DefaultNamespace,
		readChange(t, cluster, "calc-s2c-1"), readRepository(t, cluster, "calc"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, member := range plan.members {
		names = append(names, member.Name)
	}
	if strings.Join(names, ",") != "cmd-calc-s2c-1,calc-s2c-1" {
		t.Errorf("members = %v, want the dependency before its dependent", names)
	}
}
