package e2e

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/persist"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const restoreRepository = "restore-calc"

// Plan 0007 item 5: a branch's change history is written by persist and comes
// back when a fresh kcp is restored from it.
func TestRestoreRebuildsTheChangeHistoryLive(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", restoreRepository)
	names := []string{"calc", "cmd-calc", restoreRepository}
	repositories := []string{restoreRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: restoreRepository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repoPath, Branch: "main"},
	})
	applyTyped(t, ctx, client, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: restoreRepository,
			Upstream:   "self",
			Intent:     "Integer arithmetic.",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum."},
			},
		},
	})
	changeName := "calc-s2c-aaaaaaaaaaaa-a3"
	applyTyped(t, ctx, client, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: changeName, Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc",
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    "aaaaaaaaaaaa",
		},
	})
	if _, err := client.PatchStatus(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, changeName, map[string]any{
		"phase":   specapi.PhaseSucceeded,
		"commit":  "c0ffee",
		"attempt": int64(3),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := persist.Persist(ctx, persist.Options{Cluster: client, Repository: restoreRepository, RepoPath: repoPath}); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, changeName); err != nil {
		t.Fatal(err)
	}

	result, err := persist.Restore(ctx, persist.RestoreOptions{Cluster: client, Repository: restoreRepository, RepoPath: repoPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0] != changeName {
		t.Fatalf("restored changes = %v, want %s", result.Changes, changeName)
	}
	object, err := client.Get(ctx, specapi.SpecChangeGVR, specapi.DefaultNamespace, changeName)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := typed.(*spec.SpecChange)
	if !ok {
		t.Fatalf("%s is not a SpecChange", changeName)
	}
	if restored.Status.Phase != specapi.PhaseSucceeded || restored.Status.Commit != "c0ffee" || restored.Status.Attempt != 3 {
		t.Fatalf("restored change = %+v", restored.Status)
	}
}
