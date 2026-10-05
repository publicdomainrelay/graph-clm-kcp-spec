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

const gateRepository = "greenfield-market"

// TestPolicyGateDeniesASpecBeforeRealize drives the spec-time gate on a
// spec-only repository: a SpecToCode change that declares the host reaching
// into the guest is denied against the declared must-never before any code is
// written, and the fixed spec passes the gate and realizes.
func TestPolicyGateDeniesASpecBeforeRealize(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "greenfield-market", gateRepository)
	names := []string{"guest", "host"}
	forgetObjects(t, ctx, client, []string{gateRepository}, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{gateRepository}, names)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: gateRepository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Agent:  &spec.AgentSpec{Kind: "scripted:" + gateScenario(t)},
		},
	})

	cleanGuest := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "guest", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: gateRepository,
			Upstream:   spec.RefSelf,
			Intent:     "The guest boots from the host's cloud-init and reports its own network information out.",
			Interactions: []spec.Interaction{
				{ID: "i.report", Peer: "host", Initiator: spec.InitiatorSelf, Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust},
				{ID: "i.no-reach-in", Peer: "host", Initiator: spec.InitiatorPeer, Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust, Forbidden: true},
			},
		},
	}
	cleanHost := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: gateRepository,
			Upstream:   spec.RefSelf,
			Intent:     "The host provisions a guest and accepts the report the guest sends it.",
			Interactions: []spec.Interaction{
				{ID: "i.report-in", Peer: "guest", Initiator: spec.InitiatorPeer, Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust},
			},
		},
	}
	applyTyped(t, ctx, client, &cleanGuest)
	applyTyped(t, ctx, client, &cleanHost)
	// A context that has already been realized: the baseline a later edit is a
	// delta against. Nothing is ingested here, so the test writes it.
	for _, clean := range []spec.SystemContext{cleanGuest, cleanHost} {
		hash, err := spec.HashSystemContextSpec(clean.Spec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.PatchStatus(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, clean.Name, map[string]any{
			"realizedSpecHash": hash,
			"realizedSpec":     &clean.Spec,
		}); err != nil {
			t.Fatalf("settle %s: %v", clean.Name, err)
		}
	}

	controller, err := specd.New(specd.Options{
		Kubeconfig:    e2eKubeconfig,
		Workspace:     e2eWorkspace,
		Namespace:     specapi.DefaultNamespace,
		QPS:           50,
		Burst:         100,
		Resync:        500 * time.Millisecond,
		MaxAttempts:   1,
		RetryBackoff:  time.Second,
		PolicyLibrary: filepath.Join(root, "policies", "packs", "conformance"),
		Log:           logging.Discard(),
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

	baseCommit := headOf(t, repoPath)
	gateEdit(t, ctx, client, map[string]any{
		"interactions": []any{
			gateInteraction("i.report-in", "guest", "peer"),
			gateInteraction("i.reach-in", "guest", "self"),
		},
	})

	var denied spec.SpecChange
	waitFor(t, ctx, "the spec-time denial", func() bool {
		pending := changesFor(liveSpecChanges(t, ctx, client), "host", specapi.DirectionSpecToCode)
		if len(pending) != 1 || pending[0].Status.Phase != specapi.PhaseFailed {
			return false
		}
		denied = pending[0]
		return true
	})
	if !strings.Contains(denied.Status.Message, "policy denied at spec time") {
		t.Errorf("message = %q, want the policy denial", denied.Status.Message)
	}
	if !strings.Contains(denied.Status.Message, "declared must-never") {
		t.Errorf("message = %q, want the must-never named", denied.Status.Message)
	}
	foundCondition := false
	for _, entry := range denied.Status.Conditions {
		if entry.Type != specapi.ConditionPolicyValid {
			continue
		}
		foundCondition = true
		if entry.Status != "False" || entry.Reason != specapi.ReasonPolicyDeniedAtSpec {
			t.Errorf("PolicyValid = %s/%s, want False/%s", entry.Status, entry.Reason, specapi.ReasonPolicyDeniedAtSpec)
		}
	}
	if !foundCondition {
		t.Errorf("conditions = %+v, want PolicyValid", denied.Status.Conditions)
	}
	if denied.Status.Commit != "" {
		t.Errorf("commit = %q, want none: nothing may be realized", denied.Status.Commit)
	}
	if head := headOf(t, repoPath); head != baseCommit {
		t.Errorf("HEAD = %s, want the tree untouched at %s", head, baseCommit)
	}
	if branches := gitOutput(t, repoPath, "branch", "--list", "spec/*"); strings.TrimSpace(branches) != "" {
		t.Errorf("branches = %q, want none: the denied change opened no worktree", branches)
	}

	gateEdit(t, ctx, client, map[string]any{
		"interactions": []any{
			gateInteraction("i.report-in", "guest", "peer"),
			gateInteraction("i.ledger-in", "guest", "peer"),
		},
	})

	waitFor(t, ctx, "the fixed spec to realize", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), "host", specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseSucceeded {
				return true
			}
		}
		return false
	})
	if head := headOf(t, repoPath); head == baseCommit {
		t.Error("the fixed spec realized nothing")
	}
	if _, err := os.Stat(filepath.Join(repoPath, "market", "host-report.md")); err != nil {
		t.Errorf("the realized file is missing: %v", err)
	}
	assertNoSpecArtefacts(t, repoPath)
}

func gateEdit(t *testing.T, ctx context.Context, client *kcpclient.Client, specFields map[string]any) {
	t.Helper()
	patch := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "host", "namespace": specapi.DefaultNamespace},
		"spec":       specFields,
	}}
	if _, err := client.ServerSideApply(ctx, patch, "human"); err != nil {
		t.Fatalf("apply the spec edit: %v", err)
	}
}

func gateInteraction(id, peer, initiator string) map[string]any {
	return map[string]any{
		"id": id, "peer": peer, "initiator": initiator,
		"channel": "relay", "carries": []any{"network-info"},
		"purpose": "network-discovery", "level": "MUST",
	}
}

func gateScenario(t *testing.T) string {
	t.Helper()
	source := filepath.Join(repoRoot(t), "examples", "policy-gate", "scenario-greenfield.yaml")
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "scenario-greenfield.yaml")
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}
