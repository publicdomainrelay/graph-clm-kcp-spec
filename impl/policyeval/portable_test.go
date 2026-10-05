package policyeval_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

// greenfieldPortable is the rfp-guest-isolation pack bound to a spec-only
// repository: the roles are its contexts, and the vocabulary is the pack's.
func greenfieldPortable(t *testing.T) policy.Library {
	t.Helper()
	library, err := policyeval.Load(filepath.Join(root, "examples", "policies", "greenfield-market"))
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func portableCheck(t *testing.T, hostInteractions []spec.Interaction) policyeval.SpecResult {
	t.Helper()
	library := greenfieldPortable(t)
	result, err := policyeval.CheckSpecs(context.Background(), policyeval.SpecInput{
		Repository: "greenfield-market",
		Contexts: []spec.SystemContext{
			guestContext([]spec.Interaction{{
				ID: "i.report", Peer: "host", Initiator: spec.InitiatorSelf,
				Channel: "relay", Carries: []string{"network-info"},
				Purpose: "network-discovery", Level: spec.LevelMust,
			}}),
			hostContext(hostInteractions),
		},
		Binding: library.Manifest.Binding(),
		Library: library,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPortablePackDeniesAHostInitiatedDeclaredFlowAtSpecTime(t *testing.T) {
	result := portableCheck(t, []spec.Interaction{{
		ID: "i.reach-in", Peer: "guest", Initiator: spec.InitiatorSelf,
		Channel: "relay", Carries: []string{"network-info"},
		Purpose: "network-discovery", Level: spec.LevelMust,
	}})
	if !result.Decision.Blocked {
		t.Fatalf("the declared reach-in was not denied: %+v", result.Report.Violations)
	}
	joined := strings.Join(result.Messages(), "\n")
	if !strings.Contains(joined, "reaches into the guest") {
		t.Errorf("messages = %q, want the pack's reach-in rule", joined)
	}
	if strings.Contains(joined, "must-never") {
		t.Errorf("messages = %q, want the pack, not the conformance rule", joined)
	}
}

func TestPortablePackAllowsTheGuestInitiatedDeclaredFlow(t *testing.T) {
	result := portableCheck(t, []spec.Interaction{{
		ID: "i.report-in", Peer: "guest", Initiator: spec.InitiatorPeer,
		Channel: "relay", Carries: []string{"network-info"},
		Purpose: "network-discovery", Level: spec.LevelMust,
	}})
	if result.Decision.Blocked {
		t.Fatalf("the guest-initiated report was denied: %+v", result.Messages())
	}
}

// TestMembersMergeIntoOneModel pins the cross-repository model: a member's
// components are named after it, its roles merge with the library's, and a
// flow whose ends live in two checkouts is one flow.
func TestMembersMergeIntoOneModel(t *testing.T) {
	binding := policy.Binding{Roles: map[string]policy.RoleBinding{
		"host":  {Globs: []string{"lib/host/**"}},
		"guest": {Globs: []string{"lib/guest/**"}, Targets: &policy.RoleTargets{Attrs: []string{"inspectIp"}}},
	}}
	memberBinding := policy.Binding{Roles: map[string]policy.RoleBinding{
		"host": {Globs: []string{"lib/provider/**"}},
	}}
	model, err := policy.BuildModel(policy.ModelInput{
		Repository: "market",
		Binding:    binding,
		Graph: policy.CodeGraph{
			Spec: policy.CodeGraphSpec{
				Files: []policy.CodeGraphFile{{Path: "lib/host/app.ts"}},
				Nodes: []policy.CodeGraphNode{{ID: "n1", File: "lib/host/app.ts", Name: "provision", QualifiedName: "provision"}},
			},
		},
		Members: []policy.ModelMember{{
			Name:    "provider",
			Binding: memberBinding.Merge(policy.Binding{Roles: binding.Roles}),
			Graph: policy.CodeGraph{
				Spec: policy.CodeGraphSpec{
					Files: []policy.CodeGraphFile{{Path: "lib/provider/local.ts"}},
					Nodes: []policy.CodeGraphNode{{ID: "m1", File: "lib/provider/local.ts", Name: "provisionVM", QualifiedName: "provisionVM"}},
				},
			},
			Effects: []policy.Effect{{
				ID: "e1", Kind: policy.EffectContainerExec, File: "lib/provider/local.ts",
				Line: 381, Node: "m1", Attrs: map[string]string{"verb": "inspectIp"},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, component := range model.Spec.Components {
		if component.Name == "provider/host" {
			found = true
		}
	}
	if !found {
		t.Fatalf("components = %+v, want provider/host", model.Spec.Components)
	}
	flows := model.FlowsWhere(policy.FlowFilter{From: "host", To: "guest"})
	if len(flows) != 1 || len(flows[0].Evidence) != 1 || flows[0].Evidence[0] != "provider/e1" {
		t.Fatalf("flows = %+v, want the prefixed cross-repository reach-in", model.Spec.Flows)
	}
	if model.Spec.Repository != "market" {
		t.Errorf("repository = %q, want the importing library's", model.Spec.Repository)
	}
}
