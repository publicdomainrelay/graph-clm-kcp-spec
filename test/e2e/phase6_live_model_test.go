package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

func TestPhase6LiveModelRealizesSubtract(t *testing.T) {
	requireLiveModel(t, "deepseek-claude", "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	repoPath := fixture.CopyAs(t, "calc", phase6Repository)
	names := []string{"calc", "cmd-calc", phase6Repository}
	repositories := []string{phase6Repository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase6Repository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{"go", "test", "./..."},
		},
	})
	applyTyped(t, ctx, client, phase6Baseline())

	controller, err := specd.New(specd.Options{
		Kubeconfig:   e2eKubeconfig,
		Workspace:    e2eWorkspace,
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		Agent:        "claude",
		AgentTimeout: 10 * time.Minute,
		MaxAttempts:  1,
		RetryBackoff: time.Second,
		Log:          logging.Discard(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- controller.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case <-stopped:
		case <-time.After(60 * time.Second):
			t.Error("the controller did not stop")
		}
	})

	waitFor(t, ctx, "the first ingest", func() bool {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
		if err != nil {
			return false
		}
		realized, _, _ := unstructured.NestedString(object.Object, "status", "realizedSpecHash")
		return realized != ""
	})
	baseCommit := headOf(t, repoPath)

	edit := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "calc", "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"interfaces": []any{map[string]any{
				"name": "Subtract", "kind": "function",
				"signature": "func Subtract(a, b int) int", "file": "calc/calc.go",
			}},
			"requirements": []any{map[string]any{
				"id": "r.subtract", "level": "SHOULD", "text": "Subtract returns the difference of two integers.",
				"codeRefs": []any{"function:Subtract", "file:calc/calc.go"},
			}},
		},
	}}
	if _, err := client.ServerSideApply(ctx, edit, "human"); err != nil {
		t.Fatalf("apply the spec edit: %v", err)
	}

	waitFor(t, ctx, "the SpecToCode change to succeed", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseSucceeded
	})
	succeeded := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0]
	t.Logf("the model realized %s on %s, commit %s, files %v",
		succeeded.Spec.SystemContext, succeeded.Status.Branch, succeeded.Status.Commit, succeeded.Status.FilesTouched)

	if succeeded.Status.Commit == "" {
		t.Fatalf("the model produced no commit: %+v", succeeded.Status)
	}
	if head := headOf(t, repoPath); head != succeeded.Status.Commit {
		t.Errorf("HEAD = %s, want the model's commit %s", head, succeeded.Status.Commit)
	}
	if head := headOf(t, repoPath); head == baseCommit {
		t.Error("HEAD did not advance")
	}
	contents, err := os.ReadFile(filepath.Join(repoPath, "calc", "calc.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "func Subtract(a, b int) int") {
		t.Errorf("the model did not write Subtract:\n%s", contents)
	}
	runGoTest(t, repoPath)

	settled := getContext(t, ctx, client, "calc")
	if condition := conditionOf(t, settled, specapi.ConditionDrifted); condition == nil || condition["status"] != "False" {
		t.Errorf("Drifted = %+v, want False", condition)
	}
	typed, err := kcpclient.Typed(settled)
	if err != nil {
		t.Fatal(err)
	}
	current := typed.(*spec.SystemContext)
	hash, err := spec.HashSystemContextSpec(current.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status.RealizedSpecHash != hash {
		t.Errorf("realizedSpecHash = %q, want the hash of the spec %q", current.Status.RealizedSpecHash, hash)
	}
}
