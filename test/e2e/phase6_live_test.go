package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

const phase6Repository = "phase6-calc"

func TestPhase6KeyedListsAreDeltaAble(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	const name = "phase6-keyed"
	_ = client.Delete(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Delete(cleanupCtx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	})

	baseline := &spec.SystemContext{
		TypeMeta:   metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.SystemContextKind},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   spec.RefSelf,
			Intent:     "the intent a patch must not touch",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"function:Add"}},
				{ID: "r.multiply", Level: spec.LevelMust, Text: "Multiply returns the product.", CodeRefs: []string{"function:Multiply"}},
			},
			Interfaces: []spec.Interface{
				{Name: "Add", Kind: "function", File: "calc/calc.go"},
				{Name: "Multiply", Kind: "function", File: "calc/calc.go"},
			},
			CodeRefs: []string{"function:Add", "file:calc/calc.go"},
		},
	}
	created, err := kcpclient.Unstructured(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ServerSideApply(ctx, created, "specd-baseline"); err != nil {
		t.Fatalf("server side apply the baseline: %v", err)
	}

	patch := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": name, "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"requirements": []any{map[string]any{
				"id": "r.subtract", "level": "SHOULD", "text": "Subtract returns the difference.",
				"codeRefs": []any{"function:Subtract"},
			}},
			"interfaces": []any{map[string]any{
				"name": "Subtract", "kind": "function", "file": "calc/calc.go",
			}},
		},
	}}
	if _, err := client.ServerSideApply(ctx, patch, "human"); err != nil {
		t.Fatalf("server side apply the edit: %v", err)
	}

	after := getContext(t, ctx, client, name)
	if intent := nestedString(t, after, "spec", "intent"); intent != baseline.Spec.Intent {
		t.Errorf("intent = %q, want it untouched by the patch", intent)
	}
	if got := nestedNames(t, after, "spec", "requirements"); strings.Join(got, ",") != "r.add,r.multiply,r.subtract" {
		t.Errorf("requirements = %v, want the two originals plus the new one", got)
	}
	if got := nestedNames(t, after, "spec", "interfaces"); strings.Join(got, ",") != "Add,Multiply,Subtract" {
		t.Errorf("interfaces = %v, want the two originals plus the new one", got)
	}
	refs, found, err := unstructured.NestedStringSlice(after.Object, "spec", "codeRefs")
	if err != nil || !found {
		t.Fatalf("codeRefs: %v %v", err, found)
	}
	sorted := append([]string{}, refs...)
	sort.Strings(sorted)
	if strings.Join(sorted, ",") != "file:calc/calc.go,function:Add" {
		t.Errorf("codeRefs = %v, want the set untouched by the patch", refs)
	}

	replace := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": name, "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"requirements": []any{map[string]any{
				"id": "r.subtract", "level": "MUST", "text": "Subtract returns the difference, and must.",
				"codeRefs": []any{"function:Subtract"},
			}},
		},
	}}
	if _, err := client.ServerSideApply(ctx, replace, "human"); err != nil {
		t.Fatalf("server side apply the replacement: %v", err)
	}
	replaced := getContext(t, ctx, client, name)
	if got := nestedNames(t, replaced, "spec", "requirements"); strings.Join(got, ",") != "r.add,r.multiply,r.subtract" {
		t.Errorf("requirements = %v, want the key replaced, not duplicated", got)
	}
	level := requirementLevel(t, replaced, "r.subtract")
	if level != "MUST" {
		t.Errorf("r.subtract level = %q, want the replacement", level)
	}
}

func TestPhase6SpecToCodeWithTheScriptedAgent(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
			Agent:  &spec.AgentSpec{Kind: "scripted:" + phase6Scenario(t, "scenario.yaml")},
		},
	})
	applyTyped(t, ctx, client, phase6Baseline())

	controller, err := specd.New(specd.Options{
		Kubeconfig:   filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:    "root:specs",
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		MaxAttempts:  3,
		RetryBackoff: time.Second,
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

	waitFor(t, ctx, "the first ingest", func() bool {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
		if err != nil {
			return false
		}
		fingerprint, _, _ := unstructured.NestedString(object.Object, "status", "observed", "fingerprint")
		realized, _, _ := unstructured.NestedString(object.Object, "status", "realizedSpecHash")
		return fingerprint != "" && realized != ""
	})
	baseCommit := headOf(t, repoPath)
	before := getContext(t, ctx, client, "calc")
	beforeRealized := nestedString(t, before, "status", "realizedSpecHash")

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

	var change spec.SpecChange
	waitFor(t, ctx, "the SpecToCode change", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		if len(found) != 1 {
			return false
		}
		change = found[0]
		return true
	})

	if change.Spec.Delta == nil {
		t.Fatal("the change carries no delta")
	}
	if got := deltaCounts(change.Spec.Delta); got != (deltaTally{added: 2}) {
		t.Errorf("delta = %+v, want exactly two added entries", got)
	}
	if len(change.Spec.Delta.Interfaces) != 1 || change.Spec.Delta.Interfaces[0].Name != "Subtract" {
		t.Errorf("delta interfaces = %+v", change.Spec.Delta.Interfaces)
	}
	if len(change.Spec.Delta.Requirements) != 1 || change.Spec.Delta.Requirements[0].ID != "r.subtract" {
		t.Errorf("delta requirements = %+v", change.Spec.Delta.Requirements)
	}
	if change.Spec.FromSpecHash != beforeRealized {
		t.Errorf("fromSpecHash = %q, want the realized hash %q", change.Spec.FromSpecHash, beforeRealized)
	}

	waitFor(t, ctx, "the SpecToCode change to succeed", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseSucceeded
	})
	succeeded := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0]

	if succeeded.Status.Commit == "" {
		t.Fatalf("the change reports no commit: %+v", succeeded.Status)
	}
	if !strings.HasPrefix(succeeded.Status.Branch, "spec/calc/") {
		t.Errorf("branch = %q, want spec/calc/<hash8>", succeeded.Status.Branch)
	}
	if succeeded.Status.VerifyExitCode != 0 {
		t.Errorf("verifyExitCode = %d, want 0", succeeded.Status.VerifyExitCode)
	}
	if strings.Join(succeeded.Status.FilesTouched, ",") != "calc/calc.go,calc/subtract_test.go" {
		t.Errorf("filesTouched = %v, want the code only", succeeded.Status.FilesTouched)
	}
	if message := gitOutput(t, repoPath, "log", "-1", "--format=%B", succeeded.Status.Commit); !strings.Contains(message, "Spec-Change: "+succeeded.Name) || !strings.Contains(message, "Open-Architecture: open-architecture/"+phase6Repository) {
		t.Errorf("the code commit lacks its trailers:\n%s", message)
	}
	assertNoSpecArtefacts(t, repoPath)
	waitForOrphanSpec(t, repoPath, phase6Repository, "calc", "Subtract", succeeded.Status.Commit)

	head := headOf(t, repoPath)
	if head != succeeded.Status.Commit {
		t.Errorf("HEAD = %s, want the change's commit %s", head, succeeded.Status.Commit)
	}
	if head == baseCommit {
		t.Error("HEAD did not advance")
	}
	if got := gitOutput(t, repoPath, "log", "-1", "--format=%an <%ae>"); got != "specd <specd@localhost>" {
		t.Errorf("author = %q, want the tool's own", got)
	}
	if got := gitOutput(t, repoPath, "show", "--stat", "--format=", "-1"); !strings.Contains(got, "calc/calc.go") {
		t.Errorf("the commit does not touch the code:\n%s", got)
	}
	contents, err := os.ReadFile(filepath.Join(repoPath, "calc", "calc.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "func Subtract(a, b int) int") {
		t.Errorf("calc.go does not carry Subtract:\n%s", contents)
	}
	runGoTest(t, repoPath)

	settled := getContext(t, ctx, client, "calc")
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
	if current.Status.RealizedSpec == nil {
		t.Error("status.realizedSpec is empty; the next delta has no old side")
	}
	if condition := conditionOf(t, settled, specapi.ConditionCodeSynced); condition == nil || condition["status"] != "True" {
		t.Errorf("CodeSynced = %+v, want True", condition)
	}
	if condition := conditionOf(t, settled, specapi.ConditionDrifted); condition == nil || condition["status"] != "False" {
		t.Errorf("Drifted = %+v, want False", condition)
	}
	if changes := liveSpecChanges(t, ctx, client); len(changes) != 1 {
		t.Errorf("changes = %+v, want the one that was worked off and no other", changes)
	}

	quiet := phase4Snapshot(t, ctx, client, names)
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(6 * time.Second):
	}
	if after := phase4Snapshot(t, ctx, client, names); after != quiet {
		t.Errorf("the loop did not settle:\nbefore %s\nafter  %s", quiet, after)
	}
	if headOf(t, repoPath) != head {
		t.Error("the tree moved again after the change succeeded")
	}
}

func TestPhase6FailingVerifyKeepsTheBranch(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
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
			Agent:  &spec.AgentSpec{Kind: "scripted:" + phase6Scenario(t, "scenario-failing.yaml")},
		},
	})
	applyTyped(t, ctx, client, phase6Baseline())

	controller, err := specd.New(specd.Options{
		Kubeconfig:   filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:    "root:specs",
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
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
	beforeRealized := nestedString(t, getContext(t, ctx, client, "calc"), "status", "realizedSpecHash")

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
				"codeRefs": []any{"function:Subtract"},
			}},
		},
	}}
	if _, err := client.ServerSideApply(ctx, edit, "human"); err != nil {
		t.Fatalf("apply the spec edit: %v", err)
	}

	waitFor(t, ctx, "the SpecToCode change to fail", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseFailed
	})
	failed := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0]

	if !strings.Contains(failed.Status.Message, "verify exited 1") {
		t.Errorf("message = %q, want the verify exit code", failed.Status.Message)
	}
	if !strings.Contains(failed.Status.AgentLog, "Subtract(5, 3) = 8") {
		t.Errorf("agentLog = %q, want the verify output that rejected the attempt", failed.Status.AgentLog)
	}
	if failed.Status.VerifyExitCode != 1 {
		t.Errorf("verifyExitCode = %d, want 1", failed.Status.VerifyExitCode)
	}
	if failed.Status.Commit != "" {
		t.Errorf("commit = %q, want none: nothing may land", failed.Status.Commit)
	}
	branches := gitOutput(t, repoPath, "branch", "--list", "spec/*")
	if !strings.Contains(branches, "spec/calc/") {
		t.Errorf("branches = %q, want the failed change's branch kept", branches)
	}
	if head := headOf(t, repoPath); head != baseCommit {
		t.Errorf("HEAD = %s, want the managed branch untouched at %s", head, baseCommit)
	}
	after := getContext(t, ctx, client, "calc")
	if realized := nestedString(t, after, "status", "realizedSpecHash"); realized != beforeRealized {
		t.Errorf("realizedSpecHash = %q, want the spec untouched at %q", realized, beforeRealized)
	}
	if condition := conditionOf(t, after, specapi.ConditionDrifted); condition == nil || condition["status"] != "False" {
		t.Errorf("Drifted = %+v, want False: the code did not move", condition)
	}
	if condition := conditionOf(t, after, specapi.ConditionCodeSynced); condition == nil || condition["status"] != "False" {
		t.Errorf("CodeSynced = %+v, want False: Subtract is declared and not there", condition)
	}
}

func phase6Baseline() *spec.SystemContext {
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: phase6Repository,
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

func phase6Scenario(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join(repoRoot(t), "examples", "phase6", name)
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

type deltaTally struct {
	added   int
	removed int
	changed int
}

func deltaCounts(change *spec.Delta) deltaTally {
	counts := change.Count()
	return deltaTally{added: counts.Added, removed: counts.Removed, changed: counts.Changed}
}

func requirementLevel(t *testing.T, object *unstructured.Unstructured, id string) string {
	t.Helper()
	entries, found, err := unstructured.NestedSlice(object.Object, "spec", "requirements")
	if err != nil || !found {
		t.Fatalf("no requirements on %s: %v", object.GetName(), err)
	}
	for _, entry := range entries {
		mapping, ok := entry.(map[string]any)
		if ok && mapping["id"] == id {
			level, _ := mapping["level"].(string)
			return level
		}
	}
	return ""
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	return gitOutput(t, dir, "rev-parse", "--verify", "HEAD")
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(output))
}

func runGoTest(t *testing.T, dir string) {
	t.Helper()
	command := exec.Command("go", "test", "./...")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go test ./... in %s: %v\n%s", dir, err, output)
	}
}

func waitForOrphanSpec(t *testing.T, repoPath, repository, context, want, codeCommit string) {
	t.Helper()
	branch := "open-architecture/" + repository
	deadline := time.Now().Add(30 * time.Second)
	for {
		spec, specErr := exec.Command("git", "-C", repoPath, "show", branch+":specs/"+context+".yaml").CombinedOutput()
		log, logErr := exec.Command("git", "-C", repoPath, "log", "--format=%B", branch).CombinedOutput()
		if specErr == nil && logErr == nil && strings.Contains(string(spec), want) && strings.Contains(string(log), "Code-Commit: "+codeCommit) {
			if out, err := exec.Command("git", "-C", repoPath, "merge-base", "HEAD", branch).CombinedOutput(); err == nil {
				t.Fatalf("%s shares history with the code: %s", branch, out)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never carried %q and Code-Commit %s:\n%s\n%s", branch, want, codeCommit, spec, log)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
