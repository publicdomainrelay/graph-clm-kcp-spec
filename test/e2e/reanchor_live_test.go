package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

// Plan 0007 item 6 (plan 0005 A, plan 0004 E2): CodeGraph ids are
// line-sensitive, so a realize that inserts lines above a symbol moves the id
// of every symbol below it. The re-anchoring ingest runs must keep an untouched
// requirement's code ref resolving, and the context CodeSynced, in a Go package
// and in a Deno / TypeScript package.
func TestReanchorKeepsUntouchedRequirementsCodeSyncedLive(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "deno")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	goPath := fixture.Copy(t, "calc")
	tsPath := fixture.Copy(t, "greet")
	repositories := []string{"calc", "greet"}
	names := []string{"calc", "cmd-calc", "greet", "format"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   "self",
			Intent:     "the calc library",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"Add"}},
			},
		},
	})
	applyTyped(t, ctx, client, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "greet", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "greet",
			Upstream:   "self",
			Intent:     "the greet module",
			Requirements: []spec.Requirement{
				{ID: "r.greet", Level: spec.LevelMust, Text: "greet returns a greeting.", CodeRefs: []string{"greet"}},
			},
		},
	})

	if _, err := ingest.Run(ctx, client, ingest.Options{RepoPath: goPath, RepositoryName: "calc"}); err != nil {
		t.Fatalf("first calc ingest: %v", err)
	}
	if _, err := ingest.Run(ctx, client, ingest.Options{RepoPath: tsPath, RepositoryName: "greet"}); err != nil {
		t.Fatalf("first greet ingest: %v", err)
	}

	goBefore := observedID(t, ctx, client, "calc", "Add")
	tsBefore := observedID(t, ctx, client, "greet", "greet")

	// The harness writes the id the index reported, as specctl clm apply and the
	// summarize agent do; the requirement no longer names the symbol bare. Every
	// observed interface is declared too, so CodeSynced is about the refs. The
	// second ingest recomputes the conditions against the declared spec.
	seedDeclared(t, ctx, client, "calc", "r.add", []string{goBefore})
	seedDeclared(t, ctx, client, "greet", "r.greet", []string{tsBefore})
	if _, err := ingest.Run(ctx, client, ingest.Options{RepoPath: goPath, RepositoryName: "calc"}); err != nil {
		t.Fatalf("calc ingest after the declaration: %v", err)
	}
	if _, err := ingest.Run(ctx, client, ingest.Options{RepoPath: tsPath, RepositoryName: "greet"}); err != nil {
		t.Fatalf("greet ingest after the declaration: %v", err)
	}
	if ref := requirementRefs(t, ctx, client, "calc", "r.add"); len(ref) != 1 || ref[0] != goBefore {
		t.Fatalf("calc r.add refs = %v, want the canonical id of Add", ref)
	}
	if ref := requirementRefs(t, ctx, client, "greet", "r.greet"); len(ref) != 1 || ref[0] != tsBefore {
		t.Fatalf("greet r.greet refs = %v, want the canonical id of greet", ref)
	}
	assertCodeSynced(t, ctx, client, "calc")
	assertCodeSynced(t, ctx, client, "greet")

	insertLineAtTop(t, filepath.Join(goPath, "calc", "calc.go"))
	insertLineAtTop(t, filepath.Join(tsPath, "mod.ts"))
	fixture.Commit(t, goPath, "a realize inserted a line above every symbol")
	fixture.Commit(t, tsPath, "a realize inserted a line above every symbol")

	if _, err := ingest.Run(ctx, client, ingest.Options{RepoPath: goPath, RepositoryName: "calc"}); err != nil {
		t.Fatalf("second calc ingest: %v", err)
	}
	if _, err := ingest.Run(ctx, client, ingest.Options{RepoPath: tsPath, RepositoryName: "greet"}); err != nil {
		t.Fatalf("second greet ingest: %v", err)
	}

	goAfter := observedID(t, ctx, client, "calc", "Add")
	tsAfter := observedID(t, ctx, client, "greet", "greet")
	if goAfter == goBefore {
		t.Errorf("the Go codegraph id did not move, so the test does not exercise re-anchoring")
	}
	if tsAfter == tsBefore {
		t.Errorf("the TypeScript codegraph id did not move, so the test does not exercise re-anchoring")
	}
	if ref := requirementRefs(t, ctx, client, "calc", "r.add"); len(ref) != 1 || ref[0] != goAfter {
		t.Errorf("calc r.add refs = %v, want the moved id %s", ref, goAfter)
	}
	if ref := requirementRefs(t, ctx, client, "greet", "r.greet"); len(ref) != 1 || ref[0] != tsAfter {
		t.Errorf("greet r.greet refs = %v, want the moved id %s", ref, tsAfter)
	}
	assertCodeSynced(t, ctx, client, "calc")
	assertCodeSynced(t, ctx, client, "greet")
}

func insertLineAtTop(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte("// reanchor probe: a realize inserted this line\n"), data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedDeclared declares every interface the index observed and points one
// requirement at the id the index reported, which is the shape a realize's
// harvested spec has.
func seedDeclared(t *testing.T, ctx context.Context, client *kcpclient.Client, context, id string, refs []string) {
	t.Helper()
	systemContext := readLiveContext(t, ctx, client, context)
	for _, observed := range systemContext.Status.Observed.Interfaces {
		systemContext.Spec.Interfaces = append(systemContext.Spec.Interfaces, spec.Interface{
			Name: observed.Name,
			Kind: observed.Kind,
		})
	}
	found := false
	for index := range systemContext.Spec.Requirements {
		if systemContext.Spec.Requirements[index].ID == id {
			systemContext.Spec.Requirements[index].CodeRefs = refs
			found = true
		}
	}
	if !found {
		t.Fatalf("%s has no requirement %s", context, id)
	}
	applyTyped(t, ctx, client, systemContext)
}

func observedID(t *testing.T, ctx context.Context, client *kcpclient.Client, context, name string) string {
	t.Helper()
	systemContext := readLiveContext(t, ctx, client, context)
	for _, entry := range systemContext.Status.Observed.Interfaces {
		if entry.Name == name {
			if entry.CodegraphID == "" {
				t.Fatalf("%s/%s has no codegraph id", context, name)
			}
			return entry.CodegraphID
		}
	}
	t.Fatalf("%s has no observed interface %s: %+v", context, name, systemContext.Status.Observed.Interfaces)
	return ""
}

func requirementRefs(t *testing.T, ctx context.Context, client *kcpclient.Client, context, id string) []string {
	t.Helper()
	systemContext := readLiveContext(t, ctx, client, context)
	for _, requirement := range systemContext.Spec.Requirements {
		if requirement.ID == id {
			return requirement.CodeRefs
		}
	}
	t.Fatalf("%s has no requirement %s", context, id)
	return nil
}

func readLiveContext(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) *spec.SystemContext {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return typed.(*spec.SystemContext)
}

func assertCodeSynced(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, entry := range conditions {
		condition, _ := entry.(map[string]any)
		if condition["type"] != specapi.ConditionCodeSynced {
			continue
		}
		if condition["status"] != "True" {
			t.Errorf("%s CodeSynced = %v (%v): %v", name, condition["status"], condition["reason"], condition["message"])
		}
		return
	}
	t.Errorf("%s carries no CodeSynced condition", name)
}
