package policy

import (
	"slices"
	"testing"
)

func modelFixture() ArchitectureModel {
	return ArchitectureModel{
		Kind: ArchitectureModelKind,
		Spec: ArchitectureModelSpec{
			Repository: "example",
			Roles:      []string{"guest", "host", "test"},
			Vocabulary: Vocabulary{
				Channels: map[string][]string{"relay": {"websocat"}},
				Payloads: map[string][]string{"network-info": {"address"}},
				Purposes: map[string][]string{"network-discovery": {"onNetwork"}},
				Events:   map[string][]string{"network-report": {"vm.onNetwork"}},
				Routes:   map[string][]string{"report": {"/v1/on-network"}},
			},
			Components: []ModelComponent{
				{Name: "bidder", Roles: []string{"host"}, Source: SourceObserved},
				{Name: "cloud-init", Roles: []string{"guest"}, Source: SourceDeclared},
				{Name: "tests", Roles: []string{"test"}, Source: SourceObserved},
			},
			Effects: []Effect{
				{ID: "e1", Kind: EffectHTTPHandle, Component: "bidder", File: "bidder/mod.ts", Line: 10, Attrs: map[string]string{"path": "/v1/on-network"}},
				{ID: "e2", Kind: EffectEventEmit, Component: "bidder", File: "bidder/mod.ts", Line: 20, Attrs: map[string]string{"type": "vm.onNetwork"}},
				{ID: "e3", Kind: EffectHTTPRequest, Component: "cloud-init", File: "cloud-init/mod.ts", Line: 30, Attrs: map[string]string{"url": "/v1/on-network"}},
			},
			Flows: []ModelFlow{
				{From: "cloud-init", To: "bidder", Initiator: "guest", Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Source: SourceObserved, Evidence: []string{"e3"}},
			},
			Triggers: []ModelTrigger{{From: "e1", To: "e2"}},
		},
	}
}

func vocabularyFixture() MutationVocabulary {
	return MutationVocabulary{
		HostRole: "host", GuestRole: "guest", TestRole: "test",
		Channel: "relay", Payload: "network-info", Purpose: "network-discovery",
		Event: "network-report", Route: "report",
	}
}

func TestMutationsDeriveEveryInvariantBreaker(t *testing.T) {
	model := modelFixture()
	mutations := Mutations(model, vocabularyFixture())
	names := []string{}
	for _, mutation := range mutations {
		names = append(names, mutation.Name)
	}
	want := []string{"host-reaches-in", "guest-report-dropped", "emission-from-the-lifecycle", "unrelayed-ssh", "test-dials-the-guest"}
	for _, name := range want {
		if !slices.Contains(names, name) {
			t.Errorf("the mutations do not carry %q: %v", name, names)
		}
	}
	if len(model.Spec.Flows) != 1 || len(model.Spec.Effects) != 3 || len(model.Spec.Triggers) != 1 {
		t.Fatalf("a mutation changed the input model: %d flows, %d effects, %d triggers",
			len(model.Spec.Flows), len(model.Spec.Effects), len(model.Spec.Triggers))
	}
}

func TestMutationHostReachesInCarriesTheRoleAndThePayload(t *testing.T) {
	mutations := Mutations(modelFixture(), vocabularyFixture())
	mutation := mutationNamed(t, mutations, "host-reaches-in")
	flows := mutation.Model.FlowsWhere(FlowFilter{From: "host", To: "guest"})
	if len(flows) != 1 {
		t.Fatalf("the mutation carries %d host to guest flows, want 1", len(flows))
	}
	flow := flows[0]
	if flow.Initiator != "host" || flow.Purpose != "network-discovery" || !slices.Contains(flow.Carries, "network-info") {
		t.Errorf("the derived flow is not a network-discovery reach-in: %+v", flow)
	}
	found := false
	for _, effect := range mutation.Model.Spec.Effects {
		if effect.Component == "bidder" && effect.Kind == EffectContainerExec {
			found = true
		}
	}
	if !found {
		t.Error("the mutation carries no container.exec effect in the host component")
	}
}

func TestMutationDropsEveryGuestReport(t *testing.T) {
	mutations := Mutations(modelFixture(), vocabularyFixture())
	mutation := mutationNamed(t, mutations, "guest-report-dropped")
	for _, flow := range mutation.Model.Spec.Flows {
		if flow.Initiator == "guest" {
			t.Fatalf("the mutation still carries the guest's report: %+v", flow)
		}
	}
}

func TestMutationDrivesTheEmissionFromTheLifecycle(t *testing.T) {
	mutations := Mutations(modelFixture(), vocabularyFixture())
	mutation := mutationNamed(t, mutations, "emission-from-the-lifecycle")
	triggers := mutation.Model.TriggeredBy("e2")
	if len(triggers) != 1 {
		t.Fatalf("the mutation carries %d triggers into the emit, want 1", len(triggers))
	}
	handler, ok := effectByID(mutation.Model, triggers[0].From)
	if !ok || handler.Kind != EffectProcExec {
		t.Errorf("the mutation's trigger root is %+v, want a proc.exec", handler)
	}
}

func TestMutationsSkipARoleTheModelLacks(t *testing.T) {
	model := modelFixture()
	model.Spec.Components = model.Spec.Components[:1]
	mutations := Mutations(model, vocabularyFixture())
	for _, mutation := range mutations {
		switch mutation.Name {
		case "unrelayed-ssh", "test-dials-the-guest", "host-reaches-in":
			t.Errorf("%s was derived without its components", mutation.Name)
		}
	}
}

func TestMutationsAreEmptyWithoutRoles(t *testing.T) {
	if mutations := Mutations(modelFixture(), MutationVocabulary{}); len(mutations) != 0 {
		t.Errorf("a vocabulary without roles derived %d mutations", len(mutations))
	}
}

func TestMutationVocabularyOfNamesTheConventionalClasses(t *testing.T) {
	binding := Binding{
		Roles:      map[string]RoleBinding{"host": {}, "guest": {}, "test": {}},
		Vocabulary: modelFixture().Spec.Vocabulary,
	}
	vocabulary := MutationVocabularyOf(binding, []string{"host", "guest", "test"})
	if vocabulary.HostRole != "host" || vocabulary.GuestRole != "guest" || vocabulary.TestRole != "test" {
		t.Errorf("the roles are %+v", vocabulary)
	}
	if vocabulary.Channel != "relay" || vocabulary.Payload != "network-info" || vocabulary.Purpose != "network-discovery" {
		t.Errorf("the classes are %+v", vocabulary)
	}
	if vocabulary.Event != "network-report" || vocabulary.Route != "report" {
		t.Errorf("the event classes are %+v", vocabulary)
	}
}

func TestMutationVocabularyOfFallsBackToAnyClassName(t *testing.T) {
	binding := Binding{Vocabulary: Vocabulary{Channels: map[string][]string{"tunnel": {"ssh -R"}}}}
	vocabulary := MutationVocabularyOf(binding, []string{"provider"})
	if vocabulary.Channel != "tunnel" {
		t.Errorf("the channel class is %q, want tunnel", vocabulary.Channel)
	}
	if vocabulary.HostRole != "" {
		t.Errorf("a role the pack does not name resolved to %q", vocabulary.HostRole)
	}
}

func TestForbiddenIdentifiersDropsVocabularyLikeSegments(t *testing.T) {
	binding := Binding{Roles: map[string]RoleBinding{
		"guest": {Globs: []string{"lib/common/cloud-init-common/**", "lib/market-bidder-agent/**"}},
		"host":  {Symbols: []string{"createMarketBidder"}},
	}}
	forbidden := ForbiddenIdentifiers(binding, "atproto-market", []string{"lib-requester", "atproto-market"})
	for _, want := range []string{"atproto-market", "lib-requester", "cloud-init-common", "market-bidder-agent"} {
		if !slices.Contains(forbidden, want) {
			t.Errorf("%q is not forbidden: %v", want, forbidden)
		}
	}
	if slices.Contains(forbidden, "common") || slices.Contains(forbidden, "lib") {
		t.Errorf("a generic segment is forbidden: %v", forbidden)
	}
	if slices.Contains(forbidden, "createMarketBidder") {
		t.Errorf("a role symbol is forbidden, but a symbol may be a vocabulary term: %v", forbidden)
	}
}

func TestPortabilityFindingsReportWhatTheSourceMentions(t *testing.T) {
	findings := PortabilityFindings("package x\n\nbad { input.name == \"atproto-market\" }", []string{"atproto-market", "lib-requester"})
	if len(findings) != 1 || findings[0] != "atproto-market" {
		t.Errorf("the findings are %v", findings)
	}
}

func TestCheckBindingReportsEmptyRolesAndUnmatchedClasses(t *testing.T) {
	model := modelFixture()
	model.Spec.Components = model.Spec.Components[:2]
	pack := &PackManifest{
		Name:       "rfp-guest-isolation",
		Roles:      []string{"guest", "host", "test"},
		Vocabulary: []string{"channels/relay", "payloads/network-info", "purposes/network-discovery"},
	}
	binding := Binding{
		Roles: map[string]RoleBinding{
			"guest": {Globs: []string{"cloud-init/**"}},
			"host":  {Globs: []string{"bidder/**"}},
		},
		Vocabulary: Vocabulary{
			Channels: map[string][]string{"relay": {"websocat"}},
			Payloads: map[string][]string{"endpoint-token": {"no-such-term"}},
		},
	}
	report := CheckBinding(model, binding, pack, []string{"an onNetwork report"})
	if report.OK() {
		t.Fatal("the binding is missing a role and a class, but the report is clean")
	}
	if !slices.Contains(report.EmptyRoles, "test") {
		t.Errorf("the empty roles are %v", report.EmptyRoles)
	}
	if !slices.Contains(report.Missing, "role:test") {
		t.Errorf("the missing declarations are %v", report.Missing)
	}
	if !slices.Contains(report.UnmatchedVocabulary, "payloads/endpoint-token") {
		t.Errorf("the unmatched classes are %v", report.UnmatchedVocabulary)
	}
	if slices.Contains(report.UnmatchedVocabulary, "channels/relay") {
		t.Errorf("the relay channel is carried by no flow? %v", report.UnmatchedVocabulary)
	}
	if names := report.Selectors["host"]; len(names) != 1 || names[0] != "bidder" {
		t.Errorf("the host role selects %v", names)
	}
}

func TestCheckBindingAcceptsAMatchingBinding(t *testing.T) {
	model := modelFixture()
	pack := &PackManifest{
		Name:       "rfp-guest-isolation",
		Roles:      []string{"guest", "host"},
		Vocabulary: []string{"events/network-report"},
	}
	binding := Binding{
		Roles:      map[string]RoleBinding{"guest": {}, "host": {}},
		Vocabulary: Vocabulary{Events: map[string][]string{"network-report": {"vm.onNetwork"}}},
	}
	report := CheckBinding(model, binding, pack, nil)
	if !report.OK() {
		t.Errorf("a matching binding is refused: %v", report.Messages())
	}
	if len(report.Messages()) != 0 {
		t.Errorf("a clean report carries messages: %v", report.Messages())
	}
}

func TestTemplatesForRequirementNamesTheEnforcingTemplate(t *testing.T) {
	library := Library{Templates: []Template{
		{Name: "relayonly", Requirements: []string{"lib-requester#r.relay", "other#x"}},
		{Name: "reports", Requirements: []string{"lib-requester#r.report"}},
		{Name: "unrelated", Requirements: []string{"host#r.other"}},
	}}
	names := TemplatesForRequirement(library, "lib-requester")
	if len(names) != 2 || names[0] != "relayonly" || names[1] != "reports" {
		t.Errorf("the enforcing templates are %v", names)
	}
}

func TestParseRequirementRefusesAMalformedRef(t *testing.T) {
	if _, err := ParseRequirement("lib-requester"); err == nil {
		t.Error("a ref without an id parsed")
	}
	if _, err := ParseRequirement("#r.relay"); err == nil {
		t.Error("a ref without a context parsed")
	}
	parsed, err := ParseRequirement("lib-requester#r.relay")
	if err != nil || parsed.Context != "lib-requester" || parsed.ID != "r.relay" {
		t.Errorf("the parsed ref is %+v (%v)", parsed, err)
	}
}

func mutationNamed(t *testing.T, mutations []ModelMutation, name string) ModelMutation {
	t.Helper()
	for _, mutation := range mutations {
		if mutation.Name == name {
			return mutation
		}
	}
	t.Fatalf("no mutation is named %q", name)
	return ModelMutation{}
}

func effectByID(model ArchitectureModel, id string) (Effect, bool) {
	for _, effect := range model.Spec.Effects {
		if effect.ID == id {
			return effect, true
		}
	}
	return Effect{}, false
}

func TestRequirementRefsKeepTheOrder(t *testing.T) {
	refs, err := RequirementRefs([]string{"a#1", "b#2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].Context != "a" || refs[1].ID != "2" {
		t.Errorf("the refs are %+v", refs)
	}
	if _, err := RequirementRefs([]string{"broken"}); err == nil {
		t.Error("a malformed ref list parsed")
	}
}
