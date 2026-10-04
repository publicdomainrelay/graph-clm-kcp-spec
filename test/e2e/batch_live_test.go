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

const plan2bRepository = "plan2b-calc"

const plan2bBatchWindow = 8 * time.Second

func TestPlan2bOneSpecEditToTwoContextsLandsAsOneCommit(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", plan2bRepository)
	names := []string{"calc", "cmd-calc", plan2bRepository}
	repositories := []string{plan2bRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, plan2bRepositoryFor(t, repoPath, "batch.yaml"))
	applyTyped(t, ctx, client, plan2bCalcBaseline())
	applyTyped(t, ctx, client, plan2bCLIBaseline())

	startPlan2bController(t, ctx, specd.DefaultMaxAttempts, plan2bBatchWindow)

	waitFor(t, ctx, "the first ingest of both contexts", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != "" &&
			plan2bRealizedHash(t, ctx, client, "cmd-calc") != ""
	})
	base := headOf(t, repoPath)

	applySpecEdit(t, ctx, client, plan2bSubtractEdit())
	applySpecEdit(t, ctx, client, plan2bCLISubtractEdit())

	waitFor(t, ctx, "both changes of the batch to succeed", func() bool {
		changes := liveSpecChanges(t, ctx, client)
		calc := changesFor(changes, "calc", specapi.DirectionSpecToCode)
		cli := changesFor(changes, "cmd-calc", specapi.DirectionSpecToCode)
		return len(calc) == 1 && len(cli) == 1 &&
			calc[0].Status.Phase == specapi.PhaseSucceeded &&
			cli[0].Status.Phase == specapi.PhaseSucceeded
	})

	changes := liveSpecChanges(t, ctx, client)
	calc := changesFor(changes, "calc", specapi.DirectionSpecToCode)
	cli := changesFor(changes, "cmd-calc", specapi.DirectionSpecToCode)
	if len(calc) != 1 || len(cli) != 1 {
		t.Fatalf("spec to code changes = %d for calc and %d for cmd-calc, want one each: the batch retried",
			len(calc), len(cli))
	}
	first, second := calc[0], cli[0]
	if first.Status.Commit == "" {
		t.Fatalf("the batch reports no commit: %+v", first.Status)
	}
	if second.Status.Commit != first.Status.Commit {
		t.Errorf("commits = %q and %q, want one commit for the whole batch", first.Status.Commit, second.Status.Commit)
	}
	if first.Status.VerifyExitCode != 0 || second.Status.VerifyExitCode != 0 {
		t.Errorf("verify exit codes = %d and %d, want 0", first.Status.VerifyExitCode, second.Status.VerifyExitCode)
	}
	if strings.Join(first.Status.FilesTouched, ",") != strings.Join(second.Status.FilesTouched, ",") {
		t.Errorf("filesTouched differ: %v and %v", first.Status.FilesTouched, second.Status.FilesTouched)
	}
	for _, want := range []string{"calc/calc.go", "calc/subtract_test.go", "cmd/calc/subtract_test.go"} {
		if !contains(first.Status.FilesTouched, want) {
			t.Errorf("filesTouched = %v, want %s", first.Status.FilesTouched, want)
		}
	}
	if count := gitOutput(t, repoPath, "rev-list", "--count", base+"..HEAD"); count != "1" {
		t.Errorf("the branch advanced by %s commit(s), want 1: the batch landed one change", count)
	}
	message := gitOutput(t, repoPath, "log", "-1", "--format=%B", first.Status.Commit)
	for _, member := range []spec.SpecChange{first, second} {
		if !strings.Contains(message, "Spec-Change: "+member.Name) {
			t.Errorf("the commit message lacks the trailer of %s:\n%s", member.Name, message)
		}
		if len(member.Status.Progress) == 0 {
			t.Errorf("%s carries no progress record", member.Name)
			continue
		}
		note := member.Status.Progress[len(member.Status.Progress)-1].Note
		if !strings.Contains(note, "batch of 2") {
			t.Errorf("progress note of %s = %q, want the batch", member.Name, note)
		}
	}
	if headOf(t, repoPath) != first.Status.Commit {
		t.Errorf("HEAD = %s, want the batch commit %s", headOf(t, repoPath), first.Status.Commit)
	}
	assertNoSpecArtefacts(t, repoPath)
	runGoTest(t, repoPath)
}

func TestPlan2bTwoEditsApartAreRealizedOneAfterTheOther(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", plan2bRepository)
	names := []string{"calc", "cmd-calc", plan2bRepository}
	repositories := []string{plan2bRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, plan2bRepositoryFor(t, repoPath, "batch.yaml"))
	applyTyped(t, ctx, client, plan2bCalcBaseline())
	applyTyped(t, ctx, client, plan2bCLIBaseline())

	startPlan2bController(t, ctx, specd.DefaultMaxAttempts, plan2bBatchWindow)

	waitFor(t, ctx, "the first ingest of both contexts", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != "" &&
			plan2bRealizedHash(t, ctx, client, "cmd-calc") != ""
	})
	base := headOf(t, repoPath)

	applySpecEdit(t, ctx, client, plan2bSubtractEdit())
	waitFor(t, ctx, "the first edit to land", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseSucceeded
	})
	first := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0]
	afterFirst := headOf(t, repoPath)
	if afterFirst == base {
		t.Fatal("the first edit did not move the branch")
	}

	applyTyped(t, ctx, client, plan2bRepositoryFor(t, repoPath, "divide.yaml"))
	applySpecEdit(t, ctx, client, plan2bDivideEdit())
	waitFor(t, ctx, "the second edit to land", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 2 && found[0].Status.Phase == specapi.PhaseSucceeded && found[1].Status.Phase == specapi.PhaseSucceeded
	})

	found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
	if len(found) != 2 {
		t.Fatalf("spec to code changes = %d, want 2", len(found))
	}
	second := found[0]
	if second.Status.Commit != headOf(t, repoPath) {
		second = found[1]
	}
	if second.Status.Commit == "" || second.Status.Commit == first.Status.Commit {
		t.Fatalf("the second edit committed %q, want its own commit after %q", second.Status.Commit, first.Status.Commit)
	}
	if parent := gitOutput(t, repoPath, "rev-parse", second.Status.Commit+"^"); parent != first.Status.Commit {
		t.Errorf("the second commit's parent = %s, want the first commit %s", parent, first.Status.Commit)
	}
	if count := gitOutput(t, repoPath, "rev-list", "--count", base+"..HEAD"); count != "2" {
		t.Errorf("the branch advanced by %s commit(s), want 2", count)
	}
	for _, change := range liveSpecChanges(t, ctx, client) {
		if change.Status.Phase == specapi.PhaseFailed {
			t.Errorf("%s failed: %s", change.Name, change.Status.Message)
		}
	}
	if headOf(t, repoPath) != second.Status.Commit {
		t.Errorf("HEAD = %s, want the second commit %s", headOf(t, repoPath), second.Status.Commit)
	}
	if branches := gitOutput(t, repoPath, "branch", "--list", "spec/*"); branches != "" {
		t.Errorf("leftover realize branches: %q", branches)
	}
	assertNoSpecArtefacts(t, repoPath)
	runGoTest(t, repoPath)
}

func TestPlan2bAVerifyFailureOnAMovedBranchRetriesOnce(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", plan2bRepository)
	names := []string{"calc", "cmd-calc", plan2bRepository}
	repositories := []string{plan2bRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: plan2bRepository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{plan2bMovedBranchVerifier(t), repoPath},
			Agent:  &spec.AgentSpec{Kind: "scripted:" + plan2bScenario(t, "retry.yaml")},
		},
	})
	applyTyped(t, ctx, client, plan2bCalcBaseline())

	startPlan2bController(t, ctx, 1, plan2bBatchWindow)

	waitFor(t, ctx, "the first ingest", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != ""
	})
	base := headOf(t, repoPath)

	applySpecEdit(t, ctx, client, plan2bSubtractEdit())
	waitFor(t, ctx, "the change to land after the retry", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseSucceeded
	})

	found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
	if len(found) != 1 {
		t.Fatalf("spec to code changes = %d, want one: the retry must not count against the attempt cap", len(found))
	}
	change := found[0]
	if change.Status.VerifyExitCode != 0 {
		t.Errorf("verifyExitCode = %d, want the retry's zero", change.Status.VerifyExitCode)
	}
	subject := gitOutput(t, repoPath, "log", "-1", "--format=%s", change.Status.Commit+"^")
	if subject != "a human adds Subtract" {
		t.Errorf("the change's parent is %q, want the commit that landed while the attempt ran", subject)
	}
	if count := gitOutput(t, repoPath, "rev-list", "--count", base+"..HEAD"); count != "2" {
		t.Errorf("the branch advanced by %s commit(s), want the sibling's and the retry's", count)
	}
	if headOf(t, repoPath) != change.Status.Commit {
		t.Errorf("HEAD = %s, want the change's commit %s", headOf(t, repoPath), change.Status.Commit)
	}
	runGoTest(t, repoPath)
}

func plan2bMovedBranchVerifier(t *testing.T) string {
	t.Helper()
	script := `#!/bin/sh
set -e
repo="$1"
if ! grep -q 'func Subtract' "$repo/calc/calc.go"; then
	printf '\n// Subtract returns the difference of two integers.\nfunc Subtract(a, b int) int {\n\treturn a - b\n}\n' >> "$repo/calc/calc.go"
	git -C "$repo" add -A
	git -C "$repo" -c user.email=human@example.com -c user.name=human commit -qm "a human adds Subtract"
fi
go test ./...
`
	target := filepath.Join(t.TempDir(), "verify.sh")
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func applySpecEdit(t *testing.T, ctx context.Context, client *kcpclient.Client, edit *unstructured.Unstructured) {
	t.Helper()
	if _, err := client.ServerSideApply(ctx, edit, "human"); err != nil {
		t.Fatalf("apply the spec edit %s: %v", edit.GetName(), err)
	}
}

func plan2bRealizedHash(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) string {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		return ""
	}
	realized, _, _ := unstructured.NestedString(object.Object, "status", "realizedSpecHash")
	return realized
}

func plan2bRepositoryFor(t *testing.T, repoPath, scenario string) *spec.Repository {
	t.Helper()
	return &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: plan2bRepository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{"go", "test", "./..."},
			Agent:  &spec.AgentSpec{Kind: "scripted:" + plan2bScenario(t, scenario)},
		},
	}
}

func plan2bScenario(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join(repoRoot(t), "examples", "plan2b", name)
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

func startPlan2bController(t *testing.T, ctx context.Context, maxAttempts int, window time.Duration) {
	t.Helper()
	controller, err := specd.New(specd.Options{
		Kubeconfig:   e2eKubeconfig,
		Workspace:    e2eWorkspace,
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		MaxAttempts:  maxAttempts,
		RetryBackoff: time.Second,
		BatchWindow:  window,
		Persist:      true,
		PersistDelay: 200 * time.Millisecond,
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
		case err := <-stopped:
			if err != nil {
				t.Errorf("the controller stopped with %v", err)
			}
		case <-time.After(60 * time.Second):
			t.Error("the controller did not stop")
		}
	})
}

func plan2bCalcBaseline() *spec.SystemContext {
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: plan2bRepository,
			Upstream:   spec.RefSelf,
			Intent:     "Arithmetic on two integers, as a library the CLI wraps.",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum of two integers.", CodeRefs: []string{"function:Add", "file:calc/calc.go"}},
				{ID: "r.multiply", Level: spec.LevelMust, Text: "Multiply returns the product of two integers.", CodeRefs: []string{"function:Multiply", "file:calc/calc.go"}},
			},
			Interfaces: []spec.Interface{
				{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"},
				{Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go"},
			},
			CodeRefs: []string{"file:calc/calc.go"},
		},
	}
}

func plan2bCLIBaseline() *spec.SystemContext {
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: plan2bRepository,
			Upstream:   spec.RefSelf,
			Intent:     "A command line front end over the calc package.",
			Requirements: []spec.Requirement{{
				ID: "r.read-two-operands", Level: spec.LevelMust,
				Text:     "The CLI reads two integers and an operator from the arguments.",
				CodeRefs: []string{"file:cmd/calc/main.go"},
			}},
			CodeRefs: []string{"file:cmd/calc/main.go"},
		},
	}
}

func plan2bSubtractEdit() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "calc", "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"interfaces": []any{map[string]any{
				"name": "Subtract", "kind": "function",
				"signature": "func Subtract(a, b int) int", "file": "calc/calc.go",
			}},
			"requirements": []any{map[string]any{
				"id": "r.subtract", "level": "MUST", "text": "Subtract returns the difference of two integers.",
				"codeRefs": []any{"function:Subtract", "file:calc/calc.go"},
			}},
		},
	}}
}

func plan2bCLISubtractEdit() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "cmd-calc", "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"requirements": []any{map[string]any{
				"id": "r.cli-subtract", "level": "MUST",
				"text":     "The command line front end has a test that calls calc.Subtract.",
				"codeRefs": []any{"file:cmd/calc/subtract_test.go"},
			}},
		},
	}}
}

func plan2bDivideEdit() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "calc", "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"interfaces": []any{map[string]any{
				"name": "Divide", "kind": "function",
				"signature": "func Divide(a, b int) int", "file": "calc/calc.go",
			}},
			"requirements": []any{map[string]any{
				"id": "r.divide", "level": "MUST", "text": "Divide returns the quotient of two integers.",
				"codeRefs": []any{"function:Divide", "file:calc/calc.go"},
			}},
		},
	}}
}
