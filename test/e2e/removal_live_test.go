package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const removalRepository = "removal-calc"

// TestRemovalGuardHoldsAServerSideApplyRemoval is plan 0013 item B2: a server
// side apply that drops a requirement the spec holds is not realized. The
// change stays Pending with the held message and a Proposal naming the unit, so
// no agent is asked to delete the code that implemented it.
func TestRemovalGuardHoldsAServerSideApplyRemoval(t *testing.T) {
	t.Parallel()
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", removalRepository)
	names := []string{"calc", "cmd-calc", removalRepository}
	forgetObjects(t, ctx, client, []string{removalRepository, "calc"}, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{removalRepository, "calc"}, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: removalRepository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: repoPath, Branch: "main", Verify: []string{"go", "test", "./..."}},
	})
	// The baseline and the edit are applied by the same field manager: a
	// server side apply drops a list entry only when the manager applying it
	// owns that entry, which is what makes the removal below real.
	baselineObject := removalBaseline()
	spec.SetDefaults(baselineObject)
	baseline, err := kcpclient.Unstructured(baselineObject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ServerSideApply(ctx, baseline, "human"); err != nil {
		t.Fatalf("apply the baseline: %v", err)
	}

	startRemovalController(t, ctx)

	waitFor(t, ctx, "the baseline to be realized", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != ""
	})
	settled := headOf(t, repoPath)

	if _, err := client.ServerSideApply(ctx, removalEdit(), "human"); err != nil {
		t.Fatalf("the person's edit: %v", err)
	}

	waitForState(t, ctx, "the removal to be held", func() bool {
		changes := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(changes) > 0 && heldRemovalChange(changes)
	}, func() string {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
		if err != nil {
			return "the context cannot be read: " + err.Error()
		}
		annotations := object.GetAnnotations()
		changes := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		lines := []string{"declared=" + annotations[specapi.DeclaredAnnotation], "origin=" + annotations[specapi.OriginAnnotation], fmt.Sprintf("requirements=%d", len(nestedNamesSafe(object, "spec", "requirements")))}
		for _, change := range changes {
			lines = append(lines, fmt.Sprintf("change %s %s %q", change.Name, change.Status.Phase, change.Status.Message))
		}
		return strings.Join(lines, "; ")
	})

	changes := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
	held := changes[len(changes)-1]
	if held.Status.Phase != specapi.PhasePending {
		t.Errorf("phase = %q, want Pending so a later declaration lets it run", held.Status.Phase)
	}
	if held.Status.Message != specd.HeldRemovalPrefix+"requirement/r.multiply" {
		t.Errorf("message = %q, want the held removal", held.Status.Message)
	}
	units := []string{}
	for _, proposal := range held.Status.Proposals {
		units = append(units, proposal.Unit)
	}
	if strings.Join(units, ",") != "requirement/r.multiply" {
		t.Errorf("status.proposals = %+v, want the removed unit named", held.Status.Proposals)
	}
	if len(held.Status.Proposals) == 1 && !strings.Contains(held.Status.Proposals[0].Current, "Multiply returns the product") {
		t.Errorf("proposal = %+v, want the value the spec held", held.Status.Proposals[0])
	}

	if held.Status.Commit != "" || len(held.Status.Progress) != 0 {
		t.Errorf("the held change carries work: commit %q, progress %+v", held.Status.Commit, held.Status.Progress)
	}
	if headOf(t, repoPath) != settled {
		t.Errorf("the code branch moved: %s, want the settled head %s", headOf(t, repoPath), settled)
	}

	declared, err := authority.Parse(getContext(t, ctx, client, "calc").GetAnnotations()[specapi.DeclaredAnnotation])
	if err != nil {
		t.Fatal(err)
	}
	if declared[authority.RequirementUnit("r.multiply")] {
		t.Error("the spec edit declared the requirement it removed, so the guard would never hold it")
	}
}

func heldRemovalChange(changes []spec.SpecChange) bool {
	for _, change := range changes {
		if strings.HasPrefix(change.Status.Message, specd.HeldRemovalPrefix) {
			return true
		}
	}
	return false
}

func startRemovalController(t *testing.T, ctx context.Context) {
	t.Helper()
	controller, err := specd.New(specd.Options{
		Kubeconfig:   e2eKubeconfig,
		Workspace:    clusterWorkspace(t),
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		Agent:        "scripted:" + phase5Scenario(t),
		MaxAttempts:  3,
		RetryBackoff: time.Second,
		BatchWindow:  2 * time.Second,
		Persist:      true,
		PersistDelay: 200 * time.Millisecond,
		Log:          testLog(t),
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

func removalBaseline() *spec.SystemContext {
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{
			Name: "calc", Namespace: specapi.DefaultNamespace,
			// Only r.add is declared: removing r.add would be allowed (a
			// declaration is permission), and removing the undeclared
			// r.multiply is what the guard must hold.
			Annotations: map[string]string{specapi.DeclaredAnnotation: authority.Format(authority.NewSet(
				authority.RequirementUnit("r.add"),
			))},
		},
		Spec: spec.SystemContextSpec{
			Repository: removalRepository,
			Upstream:   spec.RefSelf,
			Intent:     "Arithmetic on two integers.",
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

// removalEdit is the server side apply that drops r.multiply, written by the
// single field manager "human" the way an operator's kubectl apply writes it.
func removalEdit() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "calc", "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"repository": removalRepository,
			"upstream":   spec.RefSelf,
			"intent":     "Arithmetic on two integers.",
			"requirements": []any{map[string]any{
				"id": "r.add", "level": "MUST", "text": "Add returns the sum of two integers.",
				"codeRefs": []any{"function:Add", "file:calc/calc.go"},
			}},
			"interfaces": []any{
				map[string]any{"name": "Add", "kind": "function", "signature": "func Add(a, b int) int", "file": "calc/calc.go"},
				map[string]any{"name": "Multiply", "kind": "function", "signature": "func Multiply(a, b int) int", "file": "calc/calc.go"},
			},
			"codeRefs": []any{"file:calc/calc.go"},
		},
	}}
}

func nestedNamesSafe(object *unstructured.Unstructured, fields ...string) []any {
	entries, found, err := unstructured.NestedSlice(object.Object, fields...)
	if err != nil || !found {
		return nil
	}
	return entries
}
