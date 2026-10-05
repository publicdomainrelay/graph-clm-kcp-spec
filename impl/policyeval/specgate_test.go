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
	object, err := policy.Unstructured(data)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func violatingContexts() []spec.SystemContext {
	return []spec.SystemContext{
		guestContext([]spec.Interaction{noReachIn()}),
		hostContext([]spec.Interaction{{
			ID: "i.reach-in", Peer: "guest", Initiator: spec.InitiatorSelf,
			Channel: "relay", Carries: []string{"network-info"},
			Purpose: "network-discovery", Level: spec.LevelMust,
		}}),
	}
}

func checkWithPolicy(t *testing.T, contexts []spec.SystemContext, repository policy.RepositoryPolicy, overrides []policy.Override) policyeval.SpecResult {
	t.Helper()
	result, err := policyeval.CheckSpecs(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   contexts,
		Binding:    conformanceBinding(),
		Library:    conformanceLibrary(t),
		Policy:     repository,
		Overrides:  overrides,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCheckSpecsHonorsDisabledAndTheEnforcementCap(t *testing.T) {
	for _, repository := range []policy.RepositoryPolicy{
		{Disabled: true},
		{Enforcement: policy.EnforcementWarn},
		{Enforcement: policy.EnforcementDryRun},
	} {
		result := checkWithPolicy(t, violatingContexts(), repository, nil)
		if result.Decision.Blocked {
			t.Errorf("%+v still denied at spec time: %+v", repository, result.Messages())
		}
	}
	if result := checkWithPolicy(t, violatingContexts(), policy.RepositoryPolicy{}, nil); !result.Decision.Blocked {
		t.Fatal("the plain repository policy did not deny")
	}
}

func TestCheckSpecsHonorsAnOverride(t *testing.T) {
	plain := checkWithPolicy(t, violatingContexts(), policy.RepositoryPolicy{}, nil)
	if len(plain.Report.Violations) == 0 {
		t.Fatal("the fixture produced no violation")
	}
	constraint := plain.Report.Violations[0].Constraint
	result := checkWithPolicy(t, violatingContexts(), policy.RepositoryPolicy{},
		[]policy.Override{{Constraint: constraint, Reason: "migration", By: "operator"}})
	if result.Decision.Blocked {
		t.Fatalf("an override did not waive %s: %+v", constraint, result.Messages())
	}
	if len(result.Decision.Waived) == 0 {
		t.Errorf("the waived violation was not recorded: %+v", result.Decision)
	}
}

func TestCheckSpecsBaselineInheritsWhatTheBaseAlreadyHad(t *testing.T) {
	base := violatingContexts()
	inherited, err := policyeval.CheckSpecsBaseline(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   base,
		Applied:    map[string]spec.SystemContextSpec{"host": base[1].Spec},
		Binding:    conformanceBinding(),
		Library:    conformanceLibrary(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inherited.Decision.Blocked {
		t.Fatalf("a violation the base already carried blocked the change: %+v", inherited.Messages())
	}
	if len(inherited.Decision.Inherited) == 0 {
		t.Fatalf("the pre-existing violation was not reported as inherited: %+v", inherited.Decision)
	}
}

func TestCheckSpecsBaselineBlocksAViolationTheBaseDidNotHave(t *testing.T) {
	marker := guestContext([]spec.Interaction{noReachIn()})
	quietHost := hostContext(nil)
	reachIn := hostContext([]spec.Interaction{{
		ID: "i.reach-in", Peer: "guest", Initiator: spec.InitiatorSelf,
		Channel: "relay", Carries: []string{"network-info"},
		Purpose: "network-discovery", Level: spec.LevelMust,
	}})
	result, err := policyeval.CheckSpecsBaseline(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   []spec.SystemContext{marker, quietHost},
		Applied:    map[string]spec.SystemContextSpec{"host": reachIn.Spec},
		Binding:    conformanceBinding(),
		Library:    conformanceLibrary(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision.Blocked {
		t.Fatalf("a violation the change introduced did not block: %+v", result.Decision)
	}
	if len(result.Decision.Denied) == 0 || len(result.Decision.Inherited) != 0 {
		t.Errorf("denied %d, inherited %d; want the new violation denied",
			len(result.Decision.Denied), len(result.Decision.Inherited))
	}
}

func TestCheckSpecsBaselineNoneGatesTheWholeRepository(t *testing.T) {
	result, err := policyeval.CheckSpecsBaseline(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   violatingContexts(),
		Applied:    map[string]spec.SystemContextSpec{"host": violatingContexts()[1].Spec},
		Binding:    conformanceBinding(),
		Library:    conformanceLibrary(t),
		Policy:     policy.RepositoryPolicy{Baseline: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision.Blocked {
		t.Fatalf("baseline none did not gate the whole repository: %+v", result.Decision)
	}
}

func TestCheckSpecsBaselineComparesAgainstThePreChangeSpec(t *testing.T) {
	// The kcp object has already been edited to carry the change, so the base
	// must come from Base, not from the context as it stands.
	marker := guestContext([]spec.Interaction{noReachIn()})
	reachIn := hostContext([]spec.Interaction{{
		ID: "i.reach-in", Peer: "guest", Initiator: spec.InitiatorSelf,
		Channel: "relay", Carries: []string{"network-info"},
		Purpose: "network-discovery", Level: spec.LevelMust,
	}})
	result, err := policyeval.CheckSpecsBaseline(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   []spec.SystemContext{marker, reachIn},
		Applied:    map[string]spec.SystemContextSpec{"host": reachIn.Spec},
		Base:       map[string]spec.SystemContextSpec{"host": hostContext(nil).Spec},
		Binding:    conformanceBinding(),
		Library:    conformanceLibrary(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision.Blocked {
		t.Fatalf("the change was not denied against its pre-change spec: %+v", result.Decision)
	}
}

func TestCheckSpecsReportsAMemberItCannotResolve(t *testing.T) {
	library := conformanceLibrary(t)
	library.Manifest.Members = []policy.Member{{Name: "elsewhere", URL: "file:///nonexistent-member-repository"}}
	_, err := policyeval.CheckSpecs(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   violatingContexts(),
		Binding:    conformanceBinding(),
		Library:    library,
	})
	if err == nil || !strings.Contains(err.Error(), "member") {
		t.Fatalf("an unresolvable member was ignored: %v", err)
	}
}

// A durable exception on the policy branch waives its site at the spec-time
// gate exactly as it does in an offline evaluation: the violation is reported
// as waived, not dropped, and it does not block.
func TestCheckSpecsHonoursADurableException(t *testing.T) {
	contexts := []spec.SystemContext{
		guestContext([]spec.Interaction{noReachIn()}),
		hostContext([]spec.Interaction{{
			ID: "i.reach-in", Peer: "guest", Initiator: spec.InitiatorSelf,
			Channel: "relay", Carries: []string{"network-info"},
			Purpose: "network-discovery", Level: spec.LevelMust,
		}}),
	}
	library := conformanceLibrary(t)
	library.Manifest.Exceptions = []policy.Exception{{
		Constraint: "conformance-forbidden-flow",
		Reason:     "accepted for this measurement",
		Owner:      "tester",
	}}
	result, err := policyeval.CheckSpecs(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts:   contexts,
		Binding:    conformanceBinding(),
		Library:    library,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Blocked {
		t.Fatalf("the exception did not waive the violation: %+v", result.Messages())
	}
	if len(result.Decision.Waived) == 0 {
		t.Fatalf("the violation was dropped instead of waived: %+v", result.Report.Violations)
	}
	if len(result.Report.Violations) == 0 {
		t.Fatal("the waived violation is not in the report")
	}
}
