package specd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func policyChange(name string, mutate func(*policy.PolicyChange)) *policy.PolicyChange {
	change := &policy.PolicyChange{
		ObjectMeta: objectMetaFor(name),
		Spec: policy.PolicyChangeSpec{
			Repository: "calc",
			Slug:       "relay-only",
			Prompt:     "the tests reach the guest only through the relay",
		},
	}
	if mutate != nil {
		mutate(change)
	}
	change.SetDefaults()
	return change
}

func policyChangeStatus(t *testing.T, cluster *fakeCluster, name string) (string, map[string]any) {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.PolicyChangeGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change, ok := typed.(*policy.PolicyChange)
	if !ok {
		t.Fatalf("%s is not a PolicyChange", name)
	}
	status, _, _ := unstructured.NestedMap(object.Object, "status")
	return change.Status.Phase, status
}

func scriptedPolicyController(t *testing.T, cluster Cluster, scenario string) *Controller {
	t.Helper()
	controller := testController(cluster)
	agents, err := agentfactory.New(agentfactory.Options{Kind: agentfactory.Scripted + ":" + scenario})
	if err != nil {
		t.Fatal(err)
	}
	controller.agents = agents
	return controller
}

func writeScenarioFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPolicyChangeWithoutARepositoryFails(t *testing.T) {
	cluster := newFakeCluster()
	controller := scriptedPolicyController(t, cluster, writeScenarioFile(t, "policies: []\n"))
	apply(t, cluster, policyChange("calc-relay-only", nil))

	if _, err := controller.reconcilePolicyChange(context.Background(), specapi.DefaultNamespace, "calc-relay-only"); err != nil {
		t.Fatal(err)
	}
	phase, status := policyChangeStatus(t, cluster, "calc-relay-only")
	if phase != policy.PolicyPhaseFailed {
		t.Fatalf("the phase is %q, want %s (%v)", phase, policy.PolicyPhaseFailed, status)
	}
	message, _ := status["message"].(string)
	if message == "" {
		t.Error("the failure carries no message")
	}
}

func TestPolicyChangeWithoutAnAgentFails(t *testing.T) {
	cluster := newFakeCluster()
	controller := testController(cluster)
	apply(t, cluster, policyChange("calc-relay-only", nil))

	if _, err := controller.reconcilePolicyChange(context.Background(), specapi.DefaultNamespace, "calc-relay-only"); err != nil {
		t.Fatal(err)
	}
	phase, status := policyChangeStatus(t, cluster, "calc-relay-only")
	if phase != policy.PolicyPhaseFailed {
		t.Fatalf("the phase is %q, want %s (%v)", phase, policy.PolicyPhaseFailed, status)
	}
}

func TestPolicyChangeAppliedDoesNothing(t *testing.T) {
	cluster := newFakeCluster()
	controller := testController(cluster)
	change := policyChange("calc-relay-only", func(change *policy.PolicyChange) {
		change.Status.Phase = policy.PolicyPhaseApplied
	})
	apply(t, cluster, change)

	if _, err := controller.reconcilePolicyChange(context.Background(), specapi.DefaultNamespace, "calc-relay-only"); err != nil {
		t.Fatal(err)
	}
	phase, _ := policyChangeStatus(t, cluster, "calc-relay-only")
	if phase != policy.PolicyPhaseApplied {
		t.Errorf("an applied change moved to %q", phase)
	}
}

func TestPolicyChangeModeAndSlug(t *testing.T) {
	generated := policy.PolicyChangeSpec{Repository: "calc", Slug: "relay-only"}
	if generated.Mode() != policy.GenerateModePolicy || generated.TemplateSlug() != "relay-only" {
		t.Errorf("a prompt change is %q/%q", generated.Mode(), generated.TemplateSlug())
	}
	bound := policy.PolicyChangeSpec{Repository: "calc", Pack: "rfp-guest-isolation"}
	if bound.Mode() != policy.GenerateModeBind || bound.TemplateSlug() != "calc" {
		t.Errorf("a pack change is %q/%q", bound.Mode(), bound.TemplateSlug())
	}
}

func TestGeneratedContextsSelectAndNumberRequirements(t *testing.T) {
	contexts := []spec.SystemContext{
		{ObjectMeta: objectMetaFor("lib-requester"), Spec: spec.SystemContextSpec{
			Repository:   "calc",
			Requirements: []spec.Requirement{{ID: "r.relay", Level: spec.LevelMust, Text: "over the relay"}},
		}},
		{ObjectMeta: objectMetaFor("lib-bidder"), Spec: spec.SystemContextSpec{Repository: "calc"}},
	}
	selected := generateContexts(contexts, []string{"lib-requester"})
	if len(selected) != 1 || selected[0].Name != "lib-requester" {
		t.Fatalf("the selection is %+v", selected)
	}
	if len(selected[0].Requirements) != 1 || selected[0].Requirements[0] != "lib-requester#r.relay" {
		t.Errorf("the requirements are %v", selected[0].Requirements)
	}
	if all := generateContexts(contexts, nil); len(all) != 2 {
		t.Errorf("an empty selection kept %d contexts, want 2", len(all))
	}
}

func objectMetaFor(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace}
}
