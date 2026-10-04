package specd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func tempGitRepo(t *testing.T, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "first")
	return repo
}

func sourceRepository(repo, branch string) *spec.Repository {
	repository := &spec.Repository{}
	repository.Name = "calc"
	repository.Namespace = specapi.DefaultNamespace
	repository.Spec.Source = &spec.RepositorySource{Path: repo}
	repository.Spec.Branch = branch
	repository.Spec.Populate = &spec.RepositoryPopulate{Partition: spec.PartitionDirectory}
	return repository
}

func TestAMismatchedCheckoutIsNotIndexed(t *testing.T) {
	repo := tempGitRepo(t, "main")
	cluster := newFakeCluster()
	apply(t, cluster, sourceRepository(repo, "feature"))
	controller := testController(cluster)
	ctx := context.Background()

	requeue, err := controller.reconcileRepository(ctx, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if requeue == 0 {
		t.Fatal("a mismatched checkout was not asked for again")
	}
	object, err := cluster.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if status := conditionStatus(t, object, specapi.ConditionBranchMismatch); status != "True" {
		t.Fatalf("BranchMismatch = %q, want True", status)
	}
	if phase, _, _ := unstructured.NestedString(object.Object, "status", "phase"); phase != "" {
		t.Fatalf("the repository was indexed while the checkout was on another branch: phase %q", phase)
	}
}

func TestTheBranchMismatchConditionClearsWhenTheyMatchAgain(t *testing.T) {
	repo := tempGitRepo(t, "main")
	cluster := newFakeCluster()
	apply(t, cluster, sourceRepository(repo, "feature"))
	controller := testController(cluster)
	ctx := context.Background()

	repository := readRepositoryObject(t, cluster)
	if status := controller.branchStatus(ctx, repository); !status.Mismatch {
		t.Fatal("main against feature is not a mismatch")
	}
	if err := controller.setBranchCondition(ctx, repository, specapi.DefaultNamespace, controller.branchStatus(ctx, repository)); err != nil {
		t.Fatal(err)
	}
	object, err := cluster.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if status := conditionStatus(t, object, specapi.ConditionBranchMismatch); status != "True" {
		t.Fatalf("BranchMismatch = %q, want True", status)
	}

	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "-b", "feature").CombinedOutput(); err != nil {
		t.Fatalf("checkout feature: %v\n%s", err, out)
	}
	repository = readRepositoryObject(t, cluster)
	status := controller.branchStatus(ctx, repository)
	if status.Mismatch {
		t.Fatalf("feature against feature is a mismatch: %s", status.Message)
	}
	if err := controller.setBranchCondition(ctx, repository, specapi.DefaultNamespace, status); err != nil {
		t.Fatal(err)
	}
	object, err = cluster.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if cleared := conditionStatus(t, object, specapi.ConditionBranchMismatch); cleared != "False" {
		t.Fatalf("BranchMismatch = %q, want False after the checkout moved", cleared)
	}
}

func TestAMismatchedCheckoutIsNotPersisted(t *testing.T) {
	repo := tempGitRepo(t, "main")
	cluster := newFakeCluster()
	apply(t, cluster, sourceRepository(repo, "feature"))
	controller := testController(cluster)
	ctx := context.Background()

	if _, err := controller.reconcilePersist(ctx, specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", "refs/heads/open-architecture/calc").CombinedOutput(); err == nil {
		t.Fatalf("a mismatched checkout was persisted: %s", out)
	}
}

func readRepositoryObject(t *testing.T, cluster *fakeCluster) *spec.Repository {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		t.Fatalf("%s is not a Repository", object.GetName())
	}
	return repository
}
