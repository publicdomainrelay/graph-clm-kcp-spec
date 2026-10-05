package specd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// deadProcesses makes every pid look dead for the duration of a test, so a
// Running change owned by "another process" is unambiguously orphaned.
func deadProcesses(t *testing.T) {
	t.Helper()
	saved := processAlive
	processAlive = func(int) bool { return false }
	t.Cleanup(func() { processAlive = saved })
}

func liveProcesses(t *testing.T) {
	t.Helper()
	saved := processAlive
	processAlive = func(int) bool { return true }
	t.Cleanup(func() { processAlive = saved })
}

func runningChange(name, context, hash string) *spec.SpecChange {
	return &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: context,
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    hash,
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseRunning, Owner: "another", OwnerPid: 999999},
	}
}

func draftingPolicyChange(owner string, pid int) *policy.PolicyChange {
	return &policy.PolicyChange{
		TypeMeta: metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: policy.PolicyChangeKind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-calc",
			Namespace: specapi.DefaultNamespace,
		},
		Spec: policy.PolicyChangeSpec{Repository: "calc", Prompt: "author a rule"},
		Status: policy.PolicyChangeStatus{
			Phase:    policy.PolicyPhaseDrafting,
			Owner:    owner,
			OwnerPid: pid,
		},
	}
}

func phases(t *testing.T, cluster *fakeCluster) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range cluster.names(specapi.SpecChangeGVR) {
		out[name] = readChange(t, cluster, name).Status.Phase
	}
	return out
}

func TestRunningChangeWithADeadOwnerIsRequeued(t *testing.T) {
	deadProcesses(t)
	cluster := newFakeCluster()
	apply(t, cluster, runningChange("calc-s2c-aaaa", "calc", "aaaa"))
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-aaaa"); err != nil {
		t.Fatal(err)
	}

	orphan := readChange(t, cluster, "calc-s2c-aaaa")
	if orphan.Status.Phase != specapi.PhaseFailed {
		t.Fatalf("the orphan is %q, want Failed", orphan.Status.Phase)
	}
	if !strings.Contains(orphan.Status.Message, RecoveredReason) {
		t.Errorf("the orphan message = %q, want %q", orphan.Status.Message, RecoveredReason)
	}
	got := phases(t, cluster)
	if got["calc-s2c-aaaa-a2"] != specapi.PhasePending {
		t.Fatalf("phases = %v, want the next attempt Pending", got)
	}
	next := readChange(t, cluster, "calc-s2c-aaaa-a2")
	if next.Status.Attempt != 2 {
		t.Errorf("attempt = %d, want 2: the restart counts", next.Status.Attempt)
	}
	if !strings.Contains(next.Status.Message, RecoveredReason) {
		t.Errorf("the requeued message = %q, want the reason", next.Status.Message)
	}
	if next.Spec.ToSpecHash != "aaaa" {
		t.Errorf("the requeued change carries %q, want the orphan's spec", next.Spec.ToSpecHash)
	}
	if controller.QueueDepth() == 0 {
		t.Error("the requeued change was not enqueued")
	}
}

func TestRunningChangeWithNoOwnerIsRequeued(t *testing.T) {
	deadProcesses(t)
	cluster := newFakeCluster()
	change := runningChange("calc-s2c-aaaa", "calc", "aaaa")
	change.Status.Owner = ""
	change.Status.OwnerPid = 0
	apply(t, cluster, change)
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-aaaa"); err != nil {
		t.Fatal(err)
	}
	if got := phases(t, cluster); got["calc-s2c-aaaa"] != specapi.PhaseFailed || got["calc-s2c-aaaa-a2"] != specapi.PhasePending {
		t.Fatalf("phases = %v, want the pre-owner change recovered", got)
	}
}

func TestRunningChangeOwnedByThisProcessIsLeftAlone(t *testing.T) {
	liveProcesses(t)
	cluster := newFakeCluster()
	controller := testController(cluster)
	change := runningChange("calc-s2c-aaaa", "calc", "aaaa")
	change.Status.Owner = controller.owner.Token
	change.Status.OwnerPid = controller.owner.Pid
	apply(t, cluster, change)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-aaaa"); err != nil {
		t.Fatal(err)
	}
	if got := phases(t, cluster); got["calc-s2c-aaaa"] != specapi.PhaseRunning || len(got) != 1 {
		t.Fatalf("phases = %v, want the running change untouched and no new change", got)
	}
}

func TestRunningChangeOwnedByALiveProcessIsLeftAlone(t *testing.T) {
	liveProcesses(t)
	cluster := newFakeCluster()
	apply(t, cluster, runningChange("calc-s2c-aaaa", "calc", "aaaa"))
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-aaaa"); err != nil {
		t.Fatal(err)
	}
	if got := phases(t, cluster); got["calc-s2c-aaaa"] != specapi.PhaseRunning || len(got) != 1 {
		t.Fatalf("phases = %v, want the other live specd's change untouched", got)
	}
}

func TestRecoveringCountsTheAttemptAndKeepsTheEpisodeName(t *testing.T) {
	deadProcesses(t)
	cluster := newFakeCluster()
	apply(t, cluster, runningChange("calc-s2c-aaaa", "calc", "aaaa"))
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-aaaa-a2", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionSpecToCode, ToSpecHash: "aaaa",
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseFailed},
	})
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-aaaa"); err != nil {
		t.Fatal(err)
	}
	next := readChange(t, cluster, "calc-s2c-aaaa-a3")
	if next.Status.Phase != specapi.PhasePending || next.Status.Attempt != 3 {
		t.Fatalf("next attempt = %+v, want a3 Pending at attempt 3", next.Status)
	}
}

func TestRecoveringDoesNotQueueASecondAttemptWhenOneAlreadyWaits(t *testing.T) {
	deadProcesses(t)
	cluster := newFakeCluster()
	apply(t, cluster, runningChange("calc-s2c-aaaa", "calc", "aaaa"))
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-aaaa-a2", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionSpecToCode, ToSpecHash: "aaaa",
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	})
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-aaaa"); err != nil {
		t.Fatal(err)
	}
	got := phases(t, cluster)
	if got["calc-s2c-aaaa"] != specapi.PhaseFailed || got["calc-s2c-aaaa-a2"] != specapi.PhasePending {
		t.Fatalf("phases = %v, want the orphan failed and the queued attempt kept", got)
	}
	if len(got) != 2 {
		t.Fatalf("phases = %v, want no third change: an attempt already waits", got)
	}
}

func TestRecoveringPrunesTheStaleWorktreeAndBranch(t *testing.T) {
	deadProcesses(t)
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.email", "t@t")
	git(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-qm", "init")
	branch := realizeBranch("calc", "aaaa")
	root, err := os.MkdirTemp("", "specd-worktree-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	stale := filepath.Join(root, filepath.Base(repo))
	git(t, repo, "worktree", "add", "--force", "-B", branch, stale, "HEAD")

	cluster := newFakeCluster()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "recovery-calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repo, Branch: "main"},
	})
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "recovery-calc"
	}))
	apply(t, cluster, runningChange("calc-s2c-aaaa", "calc", "aaaa"))
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-aaaa"); err != nil {
		t.Fatal(err)
	}

	worktrees, err := gitrepo.Worktrees(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(worktrees) != 1 || worktrees[0] != repo {
		t.Fatalf("worktrees = %v, want only %s: the stale realize worktree has to go", worktrees, repo)
	}
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "refs/heads/"+branch).CombinedOutput(); err == nil {
		t.Errorf("the realize branch %s survived: %s", branch, out)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale worktree directory %s survived", stale)
	}

	// The repository accepts a fresh worktree on the branch again.
	if err := gitrepo.WorktreeAdd(context.Background(), repo, filepath.Join(t.TempDir(), "next"), branch, "HEAD"); err != nil {
		t.Fatalf("a worktree on the recovered branch: %v", err)
	}
}

func TestRecoverOrphansSweepsSpecAndPolicyChanges(t *testing.T) {
	deadProcesses(t)
	cluster := newFakeCluster()
	apply(t, cluster, runningChange("calc-s2c-aaaa", "calc", "aaaa"))
	apply(t, cluster, draftingPolicyChange("another", 999999))
	controller := testController(cluster)

	controller.recoverOrphans(context.Background())

	if got := phases(t, cluster); got["calc-s2c-aaaa"] != specapi.PhaseFailed || got["calc-s2c-aaaa-a2"] != specapi.PhasePending {
		t.Fatalf("spec change phases = %v, want the orphan recovered", got)
	}
	object, err := cluster.Get(context.Background(), specapi.PolicyChangeGVR, specapi.DefaultNamespace, "policy-calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	released := typed.(*policy.PolicyChange)
	if released.Status.Owner != "" {
		t.Errorf("the policy claim survived: %+v", released.Status)
	}
	if released.Status.Phase != policy.PolicyPhaseDrafting {
		t.Errorf("the policy phase = %q, want Drafting still", released.Status.Phase)
	}
	if controller.QueueDepth() < 2 {
		t.Errorf("queue depth = %d, want both recovered objects enqueued", controller.QueueDepth())
	}
}

func TestPolicyChangeOwnedByALiveProcessIsLeftAlone(t *testing.T) {
	liveProcesses(t)
	cluster := newFakeCluster()
	apply(t, cluster, draftingPolicyChange("another", 999999))
	controller := testController(cluster)

	if _, err := controller.reconcilePolicyChange(context.Background(), specapi.DefaultNamespace, "policy-calc"); err != nil {
		t.Fatal(err)
	}
	object, err := cluster.Get(context.Background(), specapi.PolicyChangeGVR, specapi.DefaultNamespace, "policy-calc")
	if err != nil {
		t.Fatal(err)
	}
	if phase, _, _ := unstructured.NestedString(object.Object, "status", "phase"); phase != policy.PolicyPhaseDrafting {
		t.Errorf("phase = %q, want Drafting: another live specd drives it", phase)
	}
	if controller.QueueDepth() != 0 {
		t.Error("the other process's change was enqueued")
	}
}

func TestReleasingAPolicyClaimSaysTheProcessDied(t *testing.T) {
	for _, phase := range []string{policy.PolicyPhaseDrafting, policy.PolicyPhaseTesting, policy.PolicyPhaseEvaluated} {
		t.Run(phase, func(t *testing.T) {
			deadProcesses(t)
			cluster := newFakeCluster()
			change := draftingPolicyChange("another", 999999)
			change.Status.Phase = phase
			apply(t, cluster, change)
			controller := testController(cluster)

			object, err := cluster.Get(context.Background(), specapi.PolicyChangeGVR, specapi.DefaultNamespace, "policy-calc")
			if err != nil {
				t.Fatal(err)
			}
			typed, err := kcpclient.Typed(object)
			if err != nil {
				t.Fatal(err)
			}
			controller.releasePolicyClaim(context.Background(), specapi.DefaultNamespace, typed.(*policy.PolicyChange))

			after, err := cluster.Get(context.Background(), specapi.PolicyChangeGVR, specapi.DefaultNamespace, "policy-calc")
			if err != nil {
				t.Fatal(err)
			}
			if owner, _, _ := unstructured.NestedString(after.Object, "status", "owner"); owner != "" {
				t.Errorf("owner = %q, want released", owner)
			}
			if message, _, _ := unstructured.NestedString(after.Object, "status", "message"); !strings.Contains(message, RecoveredPolicyReason) {
				t.Errorf("message = %q, want %q", message, RecoveredPolicyReason)
			}
			if kept, _, _ := unstructured.NestedString(after.Object, "status", "phase"); kept != phase {
				t.Errorf("phase = %q, want %q kept", kept, phase)
			}
		})
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
