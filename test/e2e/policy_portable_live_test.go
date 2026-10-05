package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const portableRepository = "greenfield-market"

// TestPortablePackGatesGreenfieldSpecs binds the rfp-guest-isolation pack to a
// spec-only repository and drives the whole loop: a SpecChange that declares
// the host reaching into the guest is denied at spec time by the pack, the
// fixed spec realizes with the scripted agent, and the code the agent writes
// is clean under the same pack when the audit reads the observed model.
func TestPortablePackGatesGreenfieldSpecs(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "greenfield-market", portableRepository)
	names := []string{"guest", "host"}
	forgetObjects(t, ctx, client, []string{portableRepository}, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{portableRepository}, names)
	})

	writePolicyBranch(t, root, repoPath)

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: portableRepository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Agent:  &spec.AgentSpec{Kind: "scripted:" + portableScenario(t)},
		},
	})

	cleanGuest := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "guest", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: portableRepository,
			Upstream:   spec.RefSelf,
			Intent:     "The guest boots from the host's cloud-init and reports its own network information out.",
			Interactions: []spec.Interaction{
				{ID: "i.report", Peer: "host", Initiator: spec.InitiatorSelf, Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust},
			},
		},
	}
	cleanHost := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: portableRepository,
			Upstream:   spec.RefSelf,
			Intent:     "The host provisions a guest and accepts the report the guest sends it.",
			Interactions: []spec.Interaction{
				{ID: "i.report-in", Peer: "guest", Initiator: spec.InitiatorPeer, Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust},
			},
		},
	}
	applyTyped(t, ctx, client, &cleanGuest)
	applyTyped(t, ctx, client, &cleanHost)
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
		Kubeconfig:   e2eKubeconfig,
		Workspace:    e2eWorkspace,
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

	baseCommit := headOf(t, repoPath)
	gateEdit(t, ctx, client, map[string]any{
		"interactions": []any{
			gateInteraction("i.report-in", "guest", "peer"),
			gateInteraction("i.reach-in", "guest", "self"),
		},
	})

	var denied spec.SpecChange
	waitFor(t, ctx, "the portable pack to deny the spec", func() bool {
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
	if !strings.Contains(denied.Status.Message, "reaches into the guest") {
		t.Errorf("message = %q, want the pack's reach-in rule", denied.Status.Message)
	}
	if head := headOf(t, repoPath); head != baseCommit {
		t.Errorf("HEAD = %s, want the tree untouched at %s", head, baseCommit)
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
	for _, path := range []string{"lib/guest/report.ts", "lib/host/report.ts"} {
		if _, err := os.Stat(filepath.Join(repoPath, path)); err != nil {
			t.Errorf("the realized file %s is missing: %v", path, err)
		}
	}

	// The audit reads the observed model of the code the agent wrote and must
	// find the pack satisfied: the guest's report is its own outbound call and
	// the host emits the report from the handler that receives it.
	waitFor(t, ctx, "the audit to read a clean model", func() bool {
		found, err := client.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, portableRepository)
		if err != nil {
			return false
		}
		typed, err := kcpclient.Typed(found)
		if err != nil {
			return false
		}
		repository, ok := typed.(*spec.Repository)
		if !ok || repository.Status.Policy == nil {
			return false
		}
		return repository.Status.Policy.Message == "clean" && repository.Status.Policy.Totals[string(policy.EnforcementDeny)] == 0
	})
	assertNoSpecArtefacts(t, repoPath)
}

// writePolicyBranch puts the portable pack's binding on the repository's own
// policy branch, so the spec gate, the realize gate and the audit read the
// same library without a --policy-library override.
func writePolicyBranch(t *testing.T, root, repoPath string) {
	t.Helper()
	library, err := policyeval.Load(filepath.Join(root, "examples", "policies", portableRepository))
	if err != nil {
		t.Fatal(err)
	}
	store := oagit.Store{Repo: repoPath}
	ref := policy.RefFor(portableRepository, "main", "main")
	if _, _, err := policygit.Init(context.Background(), store, portableRepository, ref, library.Manifest, policyeval.Lib(), policyeval.LibTest()); err != nil {
		t.Fatalf("write the policy branch: %v", err)
	}
}

func portableScenario(t *testing.T) string {
	t.Helper()
	source := filepath.Join(repoRoot(t), "examples", "policy-gate", "scenario-greenfield-portable.yaml")
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "scenario-greenfield-portable.yaml")
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}
