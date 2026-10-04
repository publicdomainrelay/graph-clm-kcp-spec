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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const phase8ModelRepository = "phase8-model-calc"

func TestPhase8LiveModelRealizesWithTheMod(t *testing.T) {
	requireLiveModel(t, "deepseek-claude", "claude", "kcp", "kine", "kubectl", "go")
	requireLive(t, "kcp", "kine", "kubectl")
	root := repoRoot(t)
	startCluster(t, root)

	modFolder := filepath.Join(root, "cc-clm-mod")
	if _, err := os.Stat(filepath.Join(modFolder, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("the mod folder is not there: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", phase8ModelRepository)
	names := []string{"calc", "cmd-calc", phase8ModelRepository}
	repositories := []string{phase8ModelRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: phase8ModelRepository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{"go", "test", "./..."},
			Agent:  &spec.AgentSpec{Kind: "claude-mod"},
		},
	})

	applyTyped(t, ctx, client, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: phase8ModelRepository,
			Upstream:   spec.RefSelf,
			Intent:     "Arithmetic on two integers.",
			Interfaces: []spec.Interface{
				{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"},
				{Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go"},
			},
			CodeRefs: []string{"file:calc/calc.go"},
		},
	})

	specctl := buildSpecctl(t, root)
	controller, err := specd.New(specd.Options{
		Kubeconfig:   filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		Workspace:    "root:specs",
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		MaxAttempts:  2,
		RetryBackoff: time.Second,
		Tool:         "codegraph",
		ClmMod:       modFolder,
		AgentTimeout: 8 * time.Minute,
		AgentEnv: map[string]string{
			"SPECD_SPECCTL": specctl,
			"KUBECONFIG":    filepath.Join(root, ".kcp-specd", "admin.kubeconfig"),
		},
		Log: logging.Discard(),
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

	waitFor(t, ctx, "the first ingest of calc", func() bool {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
		if err != nil {
			return false
		}
		realized, _, _ := unstructured.NestedString(object.Object, "status", "realizedSpecHash")
		return realized != ""
	})

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
				"codeRefs": []any{"file:calc/calc.go"},
			}},
		},
	}}
	if _, err := client.ServerSideApply(ctx, edit, "human"); err != nil {
		t.Fatalf("apply the spec edit: %v", err)
	}

	progressDuringRun := false
	waitFor(t, ctx, "the change to run with progress", func() bool {
		changes := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		if len(changes) != 1 {
			return false
		}
		change := changes[0]
		if change.Status.Phase == specapi.PhaseRunning && len(change.Status.Progress) > 0 {
			progressDuringRun = true
			return true
		}
		return change.Status.Phase == specapi.PhaseSucceeded && len(change.Status.Progress) > 0
	})

	final := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0]
	t.Logf("change %s ended %s with %d progress record(s)", final.Name, final.Status.Phase, len(final.Status.Progress))
	if !progressDuringRun {
		t.Error("the change settled without a single progress record")
	}
	filesReported := map[string]bool{}
	for _, record := range final.Status.Progress {
		for _, file := range record.Files {
			filesReported[file] = true
		}
	}
	if !filesReported["calc/calc.go"] {
		t.Errorf("the progress records name %v, want calc/calc.go", filesReported)
	}

	waitFor(t, ctx, "the change to succeed", func() bool {
		changes := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return changes[0].Status.Phase == specapi.PhaseSucceeded
	})
	succeeded := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0]
	if succeeded.Status.Commit == "" {
		t.Fatalf("the change reports no commit: %+v", succeeded.Status)
	}
	if succeeded.Status.VerifyExitCode != 0 {
		t.Errorf("verifyExitCode = %d, want 0 (%s)", succeeded.Status.VerifyExitCode, succeeded.Status.Message)
	}

	document := contextDocPath(phase8ModelRepository, "calc")
	if _, err := os.Stat(document); err != nil {
		t.Errorf("the mod did not render %s: %v", document, err)
	}
	assertNoSpecArtefacts(t, repoPath)
	if shown := gitOutput(t, repoPath, "show", "--stat", "--format=", "-1"); !strings.Contains(shown, "calc/calc.go") {
		t.Errorf("the commit does not touch the code:\n%s", shown)
	}
	runGoTest(t, repoPath)

	settled := getContext(t, ctx, client, "calc")
	if condition := conditionOf(t, settled, specapi.ConditionCodeSynced); condition == nil || condition["status"] != "True" {
		t.Errorf("CodeSynced = %+v, want True", condition)
	}
	if condition := conditionOf(t, settled, specapi.ConditionDrifted); condition != nil && condition["status"] == "True" {
		t.Errorf("Drifted = %+v, want False", condition)
	}
}
