package specd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestSlugClashRefusesATakenSlugOrName(t *testing.T) {
	library := policy.Library{Templates: []policy.Template{
		{Name: "relayonlyssh", Slug: "relay-only-ssh"},
		{Name: "guestreportreachin", Slug: "guest-report-reach-in"},
	}}
	taken := &policy.PolicyChange{Spec: policy.PolicyChangeSpec{Slug: "relay-only-ssh"}}
	if clash := slugClash(library, taken); clash == "" {
		t.Error("a taken slug is free")
	}
	byName := &policy.PolicyChange{Spec: policy.PolicyChangeSpec{Slug: "guest-report-reach-in"}}
	if clash := slugClash(library, byName); clash == "" {
		t.Error("a taken name is free")
	}
	free := &policy.PolicyChange{Spec: policy.PolicyChangeSpec{Slug: "relay-only-guest-ssh"}}
	if clash := slugClash(library, free); clash != "" {
		t.Errorf("a free slug clashes with %q", clash)
	}
	if slugToName("relay-only-guest-ssh") != "relayonlyguestssh" {
		t.Errorf("the name is %q", slugToName("relay-only-guest-ssh"))
	}
}

func TestPolicyChangeRecordDropsClusterBookkeeping(t *testing.T) {
	change := policyChange("calc-relay-only", nil)
	change.ResourceVersion = "42"
	change.UID = "uid"
	record := string(policyChangeRecord(change))
	for _, unwanted := range []string{"resourceVersion", "uid", "managedFields", "generation"} {
		if strings.Contains(record, unwanted) {
			t.Errorf("the record carries %q:\n%s", unwanted, record)
		}
	}
	if !strings.Contains(record, "kind: PolicyChange") {
		t.Errorf("the record is not a PolicyChange:\n%s", record)
	}
}

// TestRenderBindModelStripsTheBinding pins plan 0010 D1: the model a bind-mode
// prompt carries has no role, no glob and no vocabulary of the branch's
// existing binding.
func TestRenderBindModelStripsTheBinding(t *testing.T) {
	model := policy.ArchitectureModel{
		Spec: policy.ArchitectureModelSpec{
			Repository: "atproto-market",
			Roles:      []string{"guest", "host"},
			Vocabulary: policy.Vocabulary{Channels: map[string][]string{"relay": {"websocat"}}},
			Components: []policy.ModelComponent{
				{Name: "lib-market-bidder", Roles: []string{"host"}, Globs: []string{"lib/market-bidder/**"}, Source: policy.SourceObserved},
			},
			Effects:  []policy.Effect{{ID: "e1", Kind: "container.exec", Component: "lib-market-bidder", File: "lib/market-bidder/mod.ts", Line: 12}},
			Flows:    []policy.ModelFlow{{From: "lib-market-bidder", To: "lib-requester", Source: policy.SourceObserved}},
			Triggers: []policy.ModelTrigger{},
		},
	}
	rendered := renderBindModel(model)
	for _, unwanted := range []string{"roles:", "globs:", "websocat", "host", "relay:"} {
		if strings.Contains(rendered, unwanted) {
			t.Errorf("the rendered model carries %q:\n%s", unwanted, rendered)
		}
	}
	for _, want := range []string{"lib-market-bidder", "lib/market-bidder/mod.ts"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the rendered model drops the evidence %q:\n%s", want, rendered)
		}
	}
	if len(model.Spec.Components[0].Globs) != 1 || len(model.Spec.Roles) != 2 {
		t.Error("renderBindModel mutated the model it was given")
	}
}

// TestBindRequestCarriesNoBindingNorVocabulary pins plan 0010 D1: a bind-mode
// request hands the harness a model built without the branch's binding and an
// empty binding, so the prompt cannot contain the answer.
func TestBindRequestCarriesNoBindingNorVocabulary(t *testing.T) {
	controller := &Controller{}
	source := repositoryModelSource{
		repository: "atproto-market",
		graph: policy.CodeGraph{
			Spec: policy.CodeGraphSpec{
				Repository: "atproto-market",
				Files:      []policy.CodeGraphFile{{Path: "lib/market-bidder/mod.ts", Context: "lib-market-bidder"}},
			},
		},
	}
	change := policyChange("atproto-market-bind-rfp-guest-isolation", func(ch *policy.PolicyChange) {
		ch.Spec.Repository = "atproto-market"
		ch.Spec.Pack = "rfp-guest-isolation"
		ch.Spec.Slug = "rfp-guest-isolation"
	})
	binding := policy.Binding{
		Roles:      map[string]policy.RoleBinding{"host": {Globs: []string{"lib/market-bidder/**"}}},
		Vocabulary: policy.Vocabulary{Channels: map[string][]string{"relay": {"websocat"}}},
	}
	repository := &spec.Repository{ObjectMeta: objectMetaFor("atproto-market")}
	request, err := controller.policyGenerateRequest(change, repository, t.TempDir(), source, binding,
		&policy.PackManifest{Name: "rfp-guest-isolation", Version: "v1", Roles: []string{"guest", "host", "test"}},
		nil, policy.Library{}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Binding.RoleNames()) != 0 || len(request.Binding.Vocabulary.Classes()) != 0 {
		t.Errorf("the bind request carries the branch binding: %+v", request.Binding)
	}
	for _, unwanted := range []string{"roles:", "globs:", "websocat", "lib/market-bidder/**"} {
		if strings.Contains(request.Model, unwanted) {
			t.Errorf("the bind request model carries %q:\n%s", unwanted, request.Model)
		}
	}
	if request.Mode != policy.GenerateModeBind {
		t.Errorf("the request mode is %q", request.Mode)
	}
}
