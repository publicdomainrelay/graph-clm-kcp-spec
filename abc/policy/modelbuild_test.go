package policy

import (
	"testing"
)

func modelGraph() CodeGraph {
	return CodeGraph{
		APIVersion: APIVersion,
		Kind:       CodeGraphKind,
		Metadata:   ObjectMeta{Name: "sample"},
		Spec: CodeGraphSpec{
			Repository: "sample",
			Files: []CodeGraphFile{
				{Path: "lib/host/mod.ts", Language: "typescript", Context: "host-ctx"},
				{Path: "lib/common/cloud-init-common/mod.ts", Language: "typescript"},
				{Path: "test/integration_test.ts", Language: "typescript", Test: true},
			},
			Nodes: []CodeGraphNode{
				{ID: "n-handle", Kind: "route", Name: "POST /v1/on-network", File: "lib/host/mod.ts", StartLine: 10, EndLine: 12, Context: "host-ctx"},
				{ID: "n-reach", Kind: "function", Name: "emitIdentity", QualifiedName: "emitIdentity", File: "lib/host/mod.ts", StartLine: 20, EndLine: 30, Context: "host-ctx", Text: "computeProvider.getNodeId(providerId).then((nodeId) => {"},
				{ID: "n-report", Kind: "function", Name: "report", File: "lib/common/cloud-init-common/mod.ts", StartLine: 4, EndLine: 9, Text: "reportUrl; curl -X POST \"$reportUrl\" -d address"},
				{ID: "n-ssh", Kind: "function", Name: "sshToGuest", File: "test/integration_test.ts", StartLine: 3, EndLine: 6, Text: "websocat"},
			},
			Edges: []CodeGraphEdge{
				{Source: "n-handle", Target: "n-reach", Kind: "calls", Line: 10},
			},
		},
	}
}

func modelBinding() Binding {
	return Binding{
		Roles: map[string]RoleBinding{
			"guest": {
				Globs:   []string{"lib/common/cloud-init-common/**"},
				Targets: &RoleTargets{Symbols: []string{"getNodeId"}},
			},
			"host":      {Contexts: []string{"host-ctx"}},
			"requester": {Contexts: []string{"requester-ctx"}},
			"test":      {Globs: []string{"test/**"}},
		},
		Vocabulary: Vocabulary{
			Channels: map[string][]string{"relay": {"websocat"}},
			Payloads: map[string][]string{"network-info": {"address", "nodeId"}},
			Purposes: map[string][]string{"network-discovery": {"nodeId"}},
		},
	}
}

func modelEffects() []Effect {
	return []Effect{
		{ID: "e-handle", Kind: EffectHTTPHandle, File: "lib/host/mod.ts", Line: 10, Node: "n-handle", Component: "host-ctx", Attrs: map[string]string{"method": "POST", "path": "/v1/on-network"}},
		{ID: "e-reach", Kind: EffectContainerExec, File: "lib/host/mod.ts", Line: 20, Node: "n-reach", Component: "host-ctx", Attrs: map[string]string{"verb": "getNodeId"}},
		{ID: "e-report", Kind: EffectHTTPRequest, File: "lib/common/cloud-init-common/mod.ts", Line: 5, Node: "n-report", Attrs: map[string]string{"path": "/v1/on-network"}},
		{ID: "e-ssh", Kind: EffectSSHConnect, File: "test/integration_test.ts", Line: 3, Node: "n-ssh", Attrs: map[string]string{"proxyCommand": "websocat --binary wss://relay/tunnel"}},
	}
}

func TestBuildModelComponentsAndRoles(t *testing.T) {
	model, err := BuildModel(ModelInput{
		Repository: "sample",
		Graph:      modelGraph(),
		Effects:    modelEffects(),
		Contexts:   []ModelContext{{Name: "host-ctx", Labels: map[string]string{RoleLabel: "host"}}},
		Binding:    modelBinding(),
	})
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	components := map[string]ModelComponent{}
	for _, component := range model.Spec.Components {
		components[component.Name] = component
	}
	host, ok := components["host-ctx"]
	if !ok {
		t.Fatalf("missing host-ctx component: %+v", model.Spec.Components)
	}
	if host.Context != "host-ctx" || host.Source != SourceBoth {
		t.Fatalf("host-ctx = %+v, want context host-ctx source both", host)
	}
	if len(host.Roles) != 1 || host.Roles[0] != "host" {
		t.Fatalf("host-ctx roles = %v, want [host]", host.Roles)
	}
	guest, ok := components["guest"]
	if !ok {
		t.Fatalf("missing guest component: %+v", model.Spec.Components)
	}
	if guest.Context != "" || guest.Source != SourceObserved {
		t.Fatalf("guest = %+v, want observed with no context", guest)
	}
	if !containsString(guest.Roles, "guest") {
		t.Fatalf("guest roles = %v, want guest", guest.Roles)
	}
	if model.ComponentsWithRole("guest")[0].Name != "guest" {
		t.Fatalf("ComponentsWithRole(guest) did not find the guest component")
	}
}

func TestBuildModelObservedFlows(t *testing.T) {
	model, err := BuildModel(ModelInput{
		Repository: "sample",
		Graph:      modelGraph(),
		Effects:    modelEffects(),
		Contexts:   []ModelContext{{Name: "host-ctx", Labels: map[string]string{RoleLabel: "host"}}},
		Binding:    modelBinding(),
	})
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	report := model.FlowsWhere(FlowFilter{From: "guest", To: "host"})
	if len(report) != 1 {
		t.Fatalf("guest->host flows = %+v, want one", model.Spec.Flows)
	}
	if report[0].Initiator != "guest" || report[0].Source != SourceObserved {
		t.Fatalf("report flow = %+v", report[0])
	}
	if !containsString(report[0].Evidence, "e-report") {
		t.Fatalf("report evidence = %v", report[0].Evidence)
	}
	if !containsString(report[0].Carries, "network-info") {
		t.Fatalf("report carries = %v, want network-info", report[0].Carries)
	}

	reach := model.FlowsWhere(FlowFilter{From: "host", To: "guest", Purpose: "network-discovery"})
	if len(reach) != 1 {
		t.Fatalf("host->guest discovery flows = %+v, want one", model.Spec.Flows)
	}
	if !containsString(reach[0].Evidence, "e-reach") || !containsString(reach[0].Carries, "network-info") {
		t.Fatalf("reach flow = %+v", reach[0])
	}

	ssh := model.FlowsWhere(FlowFilter{Channel: "relay"})
	if len(ssh) != 1 || ssh[0].From != "test" {
		t.Fatalf("relay channel flows = %+v, want one from test", model.Spec.Flows)
	}
}

func TestBuildModelTriggers(t *testing.T) {
	model, err := BuildModel(ModelInput{
		Repository: "sample",
		Graph:      modelGraph(),
		Effects:    modelEffects(),
		Contexts:   []ModelContext{{Name: "host-ctx", Labels: map[string]string{RoleLabel: "host"}}},
		Binding:    modelBinding(),
	})
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	triggers := model.TriggeredBy("e-reach")
	if len(triggers) != 1 || triggers[0].From != "e-handle" {
		t.Fatalf("triggered_by(e-reach) = %+v, want the handle", model.Spec.Triggers)
	}
}

func TestBuildModelDeclaredInteractions(t *testing.T) {
	model, err := BuildModel(ModelInput{
		Repository: "sample",
		Graph:      modelGraph(),
		Effects:    modelEffects(),
		Contexts: []ModelContext{
			{Name: "host-ctx", Labels: map[string]string{RoleLabel: "host"}},
			{Name: "requester-ctx", Labels: map[string]string{RoleLabel: "requester"}},
		},
		Binding: modelBinding(),
		Interactions: []DeclaredInteraction{
			{Self: "guest", Peer: "requester-ctx", Initiator: "self", Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery"},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	flows := model.FlowsWhere(FlowFilter{From: "guest", To: "requester", Initiator: "guest", Source: SourceDeclared})
	if len(flows) != 1 {
		t.Fatalf("declared flows = %+v", model.Spec.Flows)
	}
	if !Declared(flows[0]) || Observed(flows[0]) {
		t.Fatalf("declared(flow)=%v observed(flow)=%v", Declared(flows[0]), Observed(flows[0]))
	}
	observed := model.FlowsWhere(FlowFilter{From: "guest", To: "host"})
	if len(observed) != 1 || !Observed(observed[0]) || Declared(observed[0]) {
		t.Fatalf("observed flow = %+v", observed)
	}
}

func TestBuildModelMergesDeclaredAndObserved(t *testing.T) {
	model, err := BuildModel(ModelInput{
		Repository: "sample",
		Graph:      modelGraph(),
		Effects:    modelEffects(),
		Contexts:   []ModelContext{{Name: "host-ctx", Labels: map[string]string{RoleLabel: "host"}}},
		Binding:    modelBinding(),
		Interactions: []DeclaredInteraction{
			{Self: "guest", Peer: "host-ctx", Initiator: "self", Channel: "", Carries: []string{"network-info"}},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	flows := model.FlowsWhere(FlowFilter{From: "guest", To: "host"})
	if len(flows) != 1 {
		t.Fatalf("flows = %+v, want one merged declared+observed", flows)
	}
	if flows[0].Source != SourceBoth || !Declared(flows[0]) || !Observed(flows[0]) {
		t.Fatalf("flow = %+v, want source both", flows[0])
	}
	if !containsString(flows[0].Evidence, "e-report") {
		t.Fatalf("merged evidence = %v, want e-report", flows[0].Evidence)
	}
}
