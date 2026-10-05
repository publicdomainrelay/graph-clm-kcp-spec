package policyeval_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

func conformanceLibrary(t *testing.T) policy.Library {
	t.Helper()
	library, err := policyeval.Load(filepath.Join(root, "policies", "packs", "conformance"))
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func conformanceBinding() policy.Binding {
	return policy.Binding{Roles: map[string]policy.RoleBinding{
		"guest": {Contexts: []string{"guest"}},
		"host":  {Contexts: []string{"host"}},
	}}
}

func guestContext(interactions []spec.Interaction) spec.SystemContext {
	return spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "guest", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository:   "greenfield-market",
			Upstream:     spec.RefSelf,
			Interactions: interactions,
		},
	}
}

func hostContext(interactions []spec.Interaction) spec.SystemContext {
	return spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository:   "greenfield-market",
			Upstream:     spec.RefSelf,
			Interactions: interactions,
		},
	}
}

// noReachIn is the guest's declared must-never: the host never initiates a
// network-discovery flow toward the guest.
func noReachIn() spec.Interaction {
	return spec.Interaction{
		ID: "i.no-reach-in", Peer: "host", Initiator: spec.InitiatorPeer,
		Channel: "relay", Carries: []string{"network-info"},
		Purpose: "network-discovery", Level: spec.LevelMust, Forbidden: true,
	}
}

func check(t *testing.T, contexts []spec.SystemContext, applied map[string]spec.SystemContextSpec) policyeval.SpecResult {
	t.Helper()
	result, err := policyeval.CheckSpecs(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   contexts,
		Applied:    applied,
		Binding:    conformanceBinding(),
		Library:    conformanceLibrary(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCheckSpecsDeniesAFlowAgainstADeclaredMustNever(t *testing.T) {
	result := check(t, []spec.SystemContext{
		guestContext([]spec.Interaction{noReachIn()}),
		hostContext([]spec.Interaction{{
			ID: "i.reach-in", Peer: "guest", Initiator: spec.InitiatorSelf,
			Channel: "relay", Carries: []string{"network-info"},
			Purpose: "network-discovery", Level: spec.LevelMust,
		}}),
	}, nil)
	if !result.Decision.Blocked {
		t.Fatalf("the reach-in was not denied: %+v", result.Report.Violations)
	}
	joined := strings.Join(result.Messages(), "\n")
	if !strings.Contains(joined, "declared must-never") {
		t.Errorf("messages = %q, want the must-never named", joined)
	}
	if len(result.Model.Spec.Flows) != 2 {
		t.Fatalf("flows = %+v, want the marker and the violating flow", result.Model.Spec.Flows)
	}
}

func TestCheckSpecsAllowsTheGuestInitiatedFlow(t *testing.T) {
	result := check(t, []spec.SystemContext{
		guestContext([]spec.Interaction{noReachIn()}),
		hostContext([]spec.Interaction{{
			ID: "i.report", Peer: "guest", Initiator: spec.InitiatorPeer,
			Channel: "relay", Carries: []string{"network-info"},
			Purpose: "network-discovery", Level: spec.LevelMust,
		}}),
	}, nil)
	if result.Decision.Blocked {
		t.Fatalf("the guest-initiated report was denied: %+v", result.Messages())
	}
	declared := result.Model.FlowsWhere(policy.FlowFilter{Source: policy.SourceDeclared})
	found := false
	for _, flow := range declared {
		if flow.Forbidden {
			found = true
		}
	}
	if !found {
		t.Fatalf("the declared must-never is not in the model: %+v", result.Model.Spec.Flows)
	}
}

func TestCheckSpecsReadsThePostDeltaSpec(t *testing.T) {
	clean := guestContext(nil)
	applied := clean.Spec
	applied.Interactions = []spec.Interaction{noReachIn()}
	result := check(t, []spec.SystemContext{clean, hostContext(nil)}, map[string]spec.SystemContextSpec{"guest": applied})
	flows := result.Model.FlowsWhere(policy.FlowFilter{From: "guest", To: "host"})
	if len(flows) != 1 || !flows[0].Forbidden {
		t.Fatalf("flows = %+v, want the post-delta marker", flows)
	}
	if result.Decision.Blocked {
		t.Fatalf("a lone declared must-never was denied: %+v", result.Messages())
	}
}

func TestCheckSpecsDeniesAnObservedFlowThatMatchesAMustNever(t *testing.T) {
	model := policy.ArchitectureModel{
		APIVersion: policy.APIVersion,
		Kind:       policy.ArchitectureModelKind,
		Metadata:   policy.ObjectMeta{Name: "greenfield-market", Namespace: specapi.DefaultNamespace},
		Spec: policy.ArchitectureModelSpec{
			Repository: "greenfield-market",
			Effects:    []policy.Effect{{ID: "e1", Kind: policy.EffectContainerExec, Component: "host", File: "host/mod.ts", Line: 4}},
			Flows: []policy.ModelFlow{
				{From: "guest", To: "host", Initiator: "host", Channel: "relay", Purpose: "network-discovery", Level: "MUST", Forbidden: true, Source: policy.SourceDeclared},
				{From: "guest", To: "host", Initiator: "host", Channel: "relay", Purpose: "network-discovery", Source: policy.SourceObserved, Evidence: []string{"e1"}},
			},
		},
	}
	report, err := policyeval.Evaluate(context.Background(), policyeval.Evaluation{
		Library:    conformanceLibrary(t),
		Repository: "greenfield-market",
		Reviewed:   []*unstructured.Unstructured{mustUnstructured(t, model)},
		Inventory:  []*unstructured.Unstructured{mustUnstructured(t, model)},
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := policy.Decide(report, policy.RepositoryPolicy{}, nil)
	if !decision.Blocked {
		t.Fatalf("an observed flow matching a declared must-never was not denied: %+v", report.Violations)
	}
}

func mustUnstructured(t *testing.T, value any) *unstructured.Unstructured {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := policyeval.Unstructured(data)
	if err != nil {
		t.Fatal(err)
	}
	return object
}
