package plan

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func req(id string) spec.Requirement {
	return spec.Requirement{ID: id, Level: spec.LevelMust, Text: "the " + id + " holds"}
}

func addReq(context, id string) spec.PlanEdit {
	return spec.PlanEdit{Context: context, Requirements: []spec.Requirement{req(id)}}
}

func fixturePlan(approved bool, items ...spec.PlanItem) spec.Plan {
	out := spec.Plan{}
	out.Spec = spec.PlanSpec{Repository: "hydradb", Approved: approved, Items: items}
	return out
}

func fixtureContexts(name string, contextSpec spec.SystemContextSpec) map[string]spec.SystemContext {
	return map[string]spec.SystemContext{name: {Spec: contextSpec}}
}

func itemStatus(t *testing.T, got Evaluation, id string) spec.PlanItemStatus {
	t.Helper()
	for _, status := range got.Items {
		if status.ID == id {
			return status
		}
	}
	t.Fatalf("no status for item %q in %+v", id, got.Items)
	return spec.PlanItemStatus{}
}

func condition(t *testing.T, got Evaluation, kind string) metav1.Condition {
	t.Helper()
	for _, declared := range got.Conditions {
		if declared.Type == kind {
			return declared
		}
	}
	t.Fatalf("no %s condition in %+v", kind, got.Conditions)
	return metav1.Condition{}
}

func TestValidate(t *testing.T) {
	acceptance := []spec.AcceptanceStep{{Name: "test", Command: []string{"go", "test"}}}
	item := func(id string, mutate func(*spec.PlanItem)) spec.PlanItem {
		out := spec.PlanItem{ID: id, Edits: []spec.PlanEdit{addReq("x", "r1")}, Acceptance: acceptance}
		if mutate != nil {
			mutate(&out)
		}
		return out
	}
	valid := func(id string, dependsOn ...string) spec.PlanItem {
		return item(id, func(out *spec.PlanItem) { out.DependsOn = dependsOn })
	}

	cases := []struct {
		name     string
		plan     spec.PlanSpec
		contains []string
	}{
		{
			name:     "an item without an id",
			plan:     spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{item("", nil)}},
			contains: []string{"no id"},
		},
		{
			name: "a duplicate item id",
			plan: spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{
				item("a", nil), item("a", nil),
			}},
			contains: []string{`"a"`, "twice"},
		},
		{
			name: "an edit without a context",
			plan: spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{
				item("a", func(out *spec.PlanItem) { out.Edits = []spec.PlanEdit{{Requirements: []spec.Requirement{req("r1")}}} }),
			}},
			contains: []string{`"a"`, "context"},
		},
		{
			name: "an acceptance step without a name",
			plan: spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{
				item("a", func(out *spec.PlanItem) { out.Acceptance = []spec.AcceptanceStep{{Command: []string{"go", "test"}}} }),
			}},
			contains: []string{`"a"`, "acceptance"},
		},
		{
			name: "an acceptance step without a command",
			plan: spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{
				item("a", func(out *spec.PlanItem) { out.Acceptance = []spec.AcceptanceStep{{Name: "test"}} }),
			}},
			contains: []string{`"a"`, "acceptance"},
		},
		{
			name: "a dependency the plan does not hold",
			plan: spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{
				valid("a", "ghost"),
			}},
			contains: []string{`"a"`, `"ghost"`, "does not hold"},
		},
		{
			name: "a cycle",
			plan: spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{
				valid("a", "b"), valid("b", "a"),
			}},
			contains: []string{"cycle"},
		},
		{
			name: "a chain a <- b <- c",
			plan: spec.PlanSpec{Repository: "hydradb", Items: []spec.PlanItem{
				valid("a"), valid("b", "a"), valid("c", "b"),
			}},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := Validate(testCase.plan)
			if len(testCase.contains) == 0 {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate = nil, want an error")
			}
			for _, fragment := range testCase.contains {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("Validate = %q, want it to contain %q", err.Error(), fragment)
				}
			}
		})
	}
}

func TestApplyEdit(t *testing.T) {
	base := func() spec.SystemContextSpec {
		return spec.SystemContextSpec{
			Intent:       "the base intent",
			Requirements: []spec.Requirement{req("r1"), req("r2")},
			Interfaces:   []spec.Interface{{Name: "Do", Kind: "function", Signature: "func Do()"}},
			Interactions: []spec.Interaction{{ID: "flow", Peer: "peer", Initiator: spec.InitiatorSelf}},
		}
	}

	cases := []struct {
		name string
		edit spec.PlanEdit
		want func(spec.SystemContextSpec) bool
	}{
		{
			name: "replaces a requirement in place",
			edit: spec.PlanEdit{Context: "x", Requirements: []spec.Requirement{
				{ID: "r1", Level: spec.LevelShould, Text: "the r1 changed"},
			}},
			want: func(out spec.SystemContextSpec) bool {
				return len(out.Requirements) == 2 &&
					out.Requirements[0].ID == "r1" && out.Requirements[0].Text == "the r1 changed" &&
					out.Requirements[1].ID == "r2"
			},
		},
		{
			name: "appends a new requirement at the end",
			edit: spec.PlanEdit{Context: "x", Requirements: []spec.Requirement{req("r3")}},
			want: func(out spec.SystemContextSpec) bool {
				return len(out.Requirements) == 3 &&
					out.Requirements[0].ID == "r1" && out.Requirements[1].ID == "r2" && out.Requirements[2].ID == "r3"
			},
		},
		{
			name: "drops a removed requirement",
			edit: spec.PlanEdit{Context: "x", RemoveRequirements: []string{"r1"}},
			want: func(out spec.SystemContextSpec) bool {
				return len(out.Requirements) == 1 && out.Requirements[0].ID == "r2"
			},
		},
		{
			name: "treats an interface by name",
			edit: spec.PlanEdit{Context: "x", Interfaces: []spec.Interface{
				{Name: "Do", Kind: "method", Signature: "func Do(int)"},
				{Name: "New", Kind: "function"},
			}},
			want: func(out spec.SystemContextSpec) bool {
				return len(out.Interfaces) == 2 &&
					out.Interfaces[0].Name == "Do" && out.Interfaces[0].Signature == "func Do(int)" &&
					out.Interfaces[1].Name == "New"
			},
		},
		{
			name: "drops a removed interface by name",
			edit: spec.PlanEdit{Context: "x", RemoveInterfaces: []string{"Do"}},
			want: func(out spec.SystemContextSpec) bool {
				return len(out.Interfaces) == 0
			},
		},
		{
			name: "treats an interaction by id",
			edit: spec.PlanEdit{Context: "x", Interactions: []spec.Interaction{
				{ID: "flow", Peer: "other", Initiator: spec.InitiatorSelf},
				{ID: "second", Peer: "peer", Initiator: spec.InitiatorPeer},
			}},
			want: func(out spec.SystemContextSpec) bool {
				return len(out.Interactions) == 2 &&
					out.Interactions[0].ID == "flow" && out.Interactions[0].Peer == "other" &&
					out.Interactions[1].ID == "second"
			},
		},
		{
			name: "drops a removed interaction by id",
			edit: spec.PlanEdit{Context: "x", RemoveInteractions: []string{"flow"}},
			want: func(out spec.SystemContextSpec) bool {
				return len(out.Interactions) == 0
			},
		},
		{
			name: "leaves the intent when the edit states none",
			edit: spec.PlanEdit{Context: "x", Requirements: []spec.Requirement{req("r3")}},
			want: func(out spec.SystemContextSpec) bool {
				return out.Intent == "the base intent"
			},
		},
		{
			name: "replaces the intent when the edit states one",
			edit: spec.PlanEdit{Context: "x", Intent: "a new intent"},
			want: func(out spec.SystemContextSpec) bool {
				return out.Intent == "a new intent"
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			current := base()
			before := base()
			got := ApplyEdit(current, testCase.edit)
			if !testCase.want(got) {
				t.Fatalf("ApplyEdit = %+v, want the edited spec", got)
			}
			if !reflect.DeepEqual(current, before) {
				t.Fatalf("ApplyEdit mutated the base: %+v, want %+v", current, before)
			}
		})
	}
}

func TestEditUnits(t *testing.T) {
	edit := spec.PlanEdit{
		Context:            "x",
		Intent:             "an intent",
		Requirements:       []spec.Requirement{req("r1")},
		RemoveRequirements: []string{"r2"},
		Interfaces:         []spec.Interface{{Name: "Do"}},
		RemoveInterfaces:   []string{"Gone"},
		Interactions:       []spec.Interaction{{ID: "flow"}},
		RemoveInteractions: []string{"dead"},
	}
	want := authority.NewSet(
		authority.UnitIntent,
		authority.RequirementUnit("r1"), authority.RequirementUnit("r2"),
		authority.InterfaceUnit("Do"), authority.InterfaceUnit("Gone"),
		authority.InteractionUnit("flow"), authority.InteractionUnit("dead"),
	)
	if got := EditUnits(edit); !got.Equal(want) {
		t.Fatalf("EditUnits = %v, want %v", got.Sorted(), want.Sorted())
	}
	if got := EditUnits(spec.PlanEdit{Context: "x"}); !got.Equal(authority.NewSet()) {
		t.Fatalf("EditUnits of an empty edit = %v, want the empty set", got.Sorted())
	}
}

func TestCarries(t *testing.T) {
	edit := spec.PlanEdit{
		Context:      "x",
		Intent:       "the intent",
		Requirements: []spec.Requirement{req("r1")},
	}
	base := spec.SystemContextSpec{}
	if Carries(base, edit) {
		t.Fatal("Carries on a spec that does not hold the edit = true, want false")
	}

	applied := ApplyEdit(base, edit)
	if !Carries(applied, edit) {
		t.Fatal("Carries on the applied spec = false, want true")
	}

	anchored := applied
	anchored.Requirements = []spec.Requirement{{
		ID: "r1", Level: spec.LevelMust, Text: "the r1 holds",
		CodeRefs: []string{"file:abc/plan/plan.go"},
	}}
	if !Carries(anchored, edit) {
		t.Fatal("Carries on a spec that differs only in codeRefs = false, want true")
	}
}

func TestPreview(t *testing.T) {
	edit := addReq("x", "r1")
	base := spec.SystemContextSpec{}
	applied := ApplyEdit(base, edit)

	if got := Preview(applied, edit); got != "x: already says this" {
		t.Fatalf("Preview = %q, want %q", got, "x: already says this")
	}
	got := Preview(base, edit)
	if !strings.HasPrefix(got, "x: ") {
		t.Fatalf("Preview = %q, want it to start with %q", got, "x: ")
	}
	if got == "x: already says this" {
		t.Fatalf("Preview = %q, want the change it would make", got)
	}
}

func TestAcceptancePassed(t *testing.T) {
	pass := spec.AcceptanceResult{Name: "test", Passed: true}
	fail := spec.AcceptanceResult{Name: "lint", Passed: false}

	cases := []struct {
		name    string
		results []spec.AcceptanceResult
		want    bool
	}{
		{"no results", nil, false},
		{"one result that passed", []spec.AcceptanceResult{pass}, true},
		{"one result that failed", []spec.AcceptanceResult{fail}, false},
		{"every result passed", []spec.AcceptanceResult{pass, pass}, true},
		{"one result of several failed", []spec.AcceptanceResult{pass, fail}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := AcceptancePassed(testCase.results); got != testCase.want {
				t.Fatalf("AcceptancePassed = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestFinish(t *testing.T) {
	passed := []spec.AcceptanceResult{
		{Name: "test", Passed: true},
		{Name: "lint", Passed: true},
	}
	status := Finish(spec.PlanItemStatus{ID: "a", Phase: ItemVerifying}, "head1", passed)
	if status.Phase != ItemDone {
		t.Fatalf("Finish phase = %q, want %q", status.Phase, ItemDone)
	}
	if status.Commit != "head1" {
		t.Fatalf("Finish commit = %q, want %q", status.Commit, "head1")
	}
	if !reflect.DeepEqual(status.Acceptance, passed) {
		t.Fatalf("Finish acceptance = %+v, want %+v", status.Acceptance, passed)
	}

	failed := []spec.AcceptanceResult{
		{Name: "test", Passed: true},
		{Name: "lint", Passed: false},
		{Name: "policy", Passed: false},
	}
	status = Finish(spec.PlanItemStatus{ID: "a", Phase: ItemVerifying}, "head2", failed)
	if status.Phase != ItemVerifying {
		t.Fatalf("Finish phase = %q, want the incoming %q", status.Phase, ItemVerifying)
	}
	if status.Commit != "head2" {
		t.Fatalf("Finish commit = %q, want %q", status.Commit, "head2")
	}
	if !reflect.DeepEqual(status.Acceptance, failed) {
		t.Fatalf("Finish acceptance = %+v, want %+v", status.Acceptance, failed)
	}
	for _, name := range []string{"lint", "policy"} {
		if !strings.Contains(status.Message, name) {
			t.Fatalf("Finish message = %q, want it to name %q", status.Message, name)
		}
	}
}

func TestEvaluateDraftPlan(t *testing.T) {
	plan := fixturePlan(false,
		spec.PlanItem{ID: "a", Edits: []spec.PlanEdit{addReq("x", "r1")}},
		spec.PlanItem{ID: "b", Edits: []spec.PlanEdit{addReq("x", "r2"), addReq("y", "r3")}},
	)
	got := Evaluate(Input{
		Plan:       plan,
		Repository: "hydradb",
		Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
		Head:       "head1",
	})

	if got.Phase != PhaseDraft {
		t.Fatalf("plan phase = %q, want %q", got.Phase, PhaseDraft)
	}
	if len(got.Actions) != 0 {
		t.Fatalf("actions = %+v, want none before approval", got.Actions)
	}
	for id, previews := range map[string]int{"a": 1, "b": 2} {
		status := itemStatus(t, got, id)
		if status.Phase != ItemPending {
			t.Fatalf("item %s phase = %q, want %q", id, status.Phase, ItemPending)
		}
		if len(status.Preview) != previews {
			t.Fatalf("item %s preview = %+v, want %d line(s)", id, status.Preview, previews)
		}
	}
	if approved := condition(t, got, ConditionApproved); approved.Status != metav1.ConditionFalse {
		t.Fatalf("approved condition = %v, want %v", approved.Status, metav1.ConditionFalse)
	}
}

func TestEvaluateAppliesAnEditTheContextDoesNotCarry(t *testing.T) {
	edit := addReq("x", "r1")
	plan := fixturePlan(true, spec.PlanItem{ID: "a", Edits: []spec.PlanEdit{edit}})
	got := Evaluate(Input{
		Plan:       plan,
		Repository: "hydradb",
		Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
		Head:       "head1",
	})

	if len(got.Actions) != 1 {
		t.Fatalf("actions = %+v, want one", got.Actions)
	}
	action := got.Actions[0]
	if action.Kind != ActionApply || action.Item != "a" {
		t.Fatalf("action = %+v, want an %s action for item a", action, ActionApply)
	}
	if !reflect.DeepEqual(action.Edits, []spec.PlanEdit{edit}) {
		t.Fatalf("action edits = %+v, want the pending edit %+v", action.Edits, edit)
	}
	if status := itemStatus(t, got, "a"); status.Phase != ItemRealizing {
		t.Fatalf("item phase = %q, want %q", status.Phase, ItemRealizing)
	}
	if got.Phase != PhaseRunning {
		t.Fatalf("plan phase = %q, want %q", got.Phase, PhaseRunning)
	}
}

func TestEvaluateBlocksOnAnUnfinishedDependency(t *testing.T) {
	plan := fixturePlan(true,
		spec.PlanItem{ID: "a", Edits: []spec.PlanEdit{addReq("x", "r1")}},
		spec.PlanItem{ID: "b", DependsOn: []string{"a"}},
	)
	got := Evaluate(Input{
		Plan:       plan,
		Repository: "hydradb",
		Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
		Head:       "head1",
	})

	status := itemStatus(t, got, "b")
	if status.Phase != ItemBlocked {
		t.Fatalf("item b phase = %q, want %q", status.Phase, ItemBlocked)
	}
	if !strings.Contains(status.Message, "a") {
		t.Fatalf("item b message = %q, want it to name its dependency", status.Message)
	}
}

func TestEvaluateBlocksOnAContextAnotherItemIsRealizing(t *testing.T) {
	plan := fixturePlan(true,
		spec.PlanItem{ID: "a", Edits: []spec.PlanEdit{addReq("x", "r1")}},
		spec.PlanItem{ID: "b", Edits: []spec.PlanEdit{addReq("x", "r2")}},
	)
	plan.Status.Items = []spec.PlanItemStatus{{ID: "a", Phase: ItemRealizing}}
	got := Evaluate(Input{
		Plan:       plan,
		Repository: "hydradb",
		Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
		Head:       "head1",
	})

	status := itemStatus(t, got, "b")
	if status.Phase != ItemBlocked {
		t.Fatalf("item b phase = %q, want %q", status.Phase, ItemBlocked)
	}
	if !strings.Contains(status.Message, "a") {
		t.Fatalf("item b message = %q, want it to name item a", status.Message)
	}
}

func TestEvaluateFollowsTheSpecToCodeChange(t *testing.T) {
	edit := addReq("x", "r1")
	plan := fixturePlan(true, spec.PlanItem{
		ID:         "a",
		Edits:      []spec.PlanEdit{edit},
		Acceptance: []spec.AcceptanceStep{{Name: "test", Command: []string{"go", "test"}}},
	})
	plan.Status.Items = []spec.PlanItemStatus{{
		ID:                "a",
		Phase:             ItemRealizing,
		AppliedSpecHashes: map[string]string{"x": "hash1"},
	}}
	change := func(phase string) spec.SpecChange {
		out := spec.SpecChange{}
		out.Name = "x-s2c-hash1"
		out.Spec = spec.SpecChangeSpec{
			SystemContext: "x",
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    "hash1",
		}
		out.Status.Phase = phase
		return out
	}
	realized := ApplyEdit(spec.SystemContextSpec{}, edit)
	carrying := map[string]spec.SystemContext{"x": {
		Spec:   spec.SystemContextSpec{},
		Status: spec.SystemContextStatus{RealizedSpec: &realized},
	}}

	t.Run("a running change keeps the item realizing", func(t *testing.T) {
		got := Evaluate(Input{
			Plan:       plan,
			Repository: "hydradb",
			Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
			Changes:    []spec.SpecChange{change(specapi.PhaseRunning)},
			Head:       "head1",
		})
		status := itemStatus(t, got, "a")
		if status.Phase != ItemRealizing {
			t.Fatalf("item phase = %q, want %q", status.Phase, ItemRealizing)
		}
		if !reflect.DeepEqual(status.Changes, []string{"x-s2c-hash1"}) {
			t.Fatalf("item changes = %+v, want the change that realizes the edit", status.Changes)
		}
	})

	t.Run("a succeeded change waits for the realized spec", func(t *testing.T) {
		got := Evaluate(Input{
			Plan:       plan,
			Repository: "hydradb",
			Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
			Changes:    []spec.SpecChange{change(specapi.PhaseSucceeded)},
			Head:       "head1",
		})
		status := itemStatus(t, got, "a")
		if status.Phase != ItemRealizing {
			t.Fatalf("item phase = %q, want %q", status.Phase, ItemRealizing)
		}
		if !strings.Contains(status.Message, "x") {
			t.Fatalf("item message = %q, want it to name the context it waits for", status.Message)
		}
	})

	t.Run("a realized spec carries the item to verifying", func(t *testing.T) {
		got := Evaluate(Input{
			Plan:       plan,
			Repository: "hydradb",
			Contexts:   carrying,
			Changes:    []spec.SpecChange{change(specapi.PhaseSucceeded)},
			Head:       "head1",
		})
		if status := itemStatus(t, got, "a"); status.Phase != ItemVerifying {
			t.Fatalf("item phase = %q, want %q", status.Phase, ItemVerifying)
		}
	})

	t.Run("failed attempts fail the item", func(t *testing.T) {
		got := Evaluate(Input{
			Plan:        plan,
			Repository:  "hydradb",
			Contexts:    fixtureContexts("x", spec.SystemContextSpec{}),
			Changes:     []spec.SpecChange{change(specapi.PhaseFailed)},
			Head:        "head1",
			MaxAttempts: 1,
		})
		status := itemStatus(t, got, "a")
		if status.Phase != ItemFailed {
			t.Fatalf("item phase = %q, want %q", status.Phase, ItemFailed)
		}
		if !reflect.DeepEqual(status.Changes, []string{"x-s2c-hash1"}) {
			t.Fatalf("item changes = %+v, want the failed change", status.Changes)
		}
	})
}

func TestEvaluateVerifying(t *testing.T) {
	acceptance := []spec.AcceptanceStep{{Name: "test", Command: []string{"go", "test"}}}
	plan := fixturePlan(true,
		spec.PlanItem{ID: "a", Acceptance: acceptance},
		spec.PlanItem{ID: "b"},
	)
	plan.Status.Items = []spec.PlanItemStatus{
		{ID: "a", Phase: ItemVerifying, Commit: "old"},
		{ID: "b", Phase: ItemVerifying},
	}

	t.Run("acceptance runs when the commit moved", func(t *testing.T) {
		got := Evaluate(Input{
			Plan:       plan,
			Repository: "hydradb",
			Contexts:   map[string]spec.SystemContext{},
			Head:       "head1",
		})
		actions := []Action{}
		for _, action := range got.Actions {
			if action.Kind == ActionAcceptance {
				actions = append(actions, action)
			}
		}
		if len(actions) != 1 {
			t.Fatalf("acceptance actions = %+v, want one", got.Actions)
		}
		if actions[0].Item != "a" || !reflect.DeepEqual(actions[0].Acceptance, acceptance) {
			t.Fatalf("acceptance action = %+v, want item a with its steps", actions[0])
		}
		if status := itemStatus(t, got, "b"); status.Phase != ItemDone || status.Commit != "head1" {
			t.Fatalf("item b = %+v, want Done at head1", status)
		}
	})

	t.Run("acceptance does not rerun at the same commit", func(t *testing.T) {
		settled := plan
		settled.Status.Items = []spec.PlanItemStatus{
			{ID: "a", Phase: ItemVerifying, Commit: "head1"},
			{ID: "b", Phase: ItemVerifying},
		}
		got := Evaluate(Input{
			Plan:       settled,
			Repository: "hydradb",
			Contexts:   map[string]spec.SystemContext{},
			Head:       "head1",
		})
		if len(got.Actions) != 0 {
			t.Fatalf("actions = %+v, want none at the commit acceptance already ran on", got.Actions)
		}
	})
}

func TestEvaluateItemElsewhere(t *testing.T) {
	plan := fixturePlan(true, spec.PlanItem{
		ID:         "a",
		Repository: "other",
		Edits:      []spec.PlanEdit{addReq("x", "r1")},
	})
	got := Evaluate(Input{
		Plan:       plan,
		Repository: "hydradb",
		Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
		Head:       "head1",
	})

	status := itemStatus(t, got, "a")
	if status.Phase != ItemElsewhere {
		t.Fatalf("item phase = %q, want %q", status.Phase, ItemElsewhere)
	}
	if len(got.Actions) != 0 {
		t.Fatalf("actions = %+v, want none for an item another instance acts on", got.Actions)
	}
}

func TestEvaluatePlanPhase(t *testing.T) {
	items := []spec.PlanItem{{ID: "a"}, {ID: "b"}}

	done := fixturePlan(true, items...)
	done.Status.Items = []spec.PlanItemStatus{
		{ID: "a", Phase: ItemDone, Commit: "head1"},
		{ID: "b", Phase: ItemDone, Commit: "head1"},
	}
	got := Evaluate(Input{Plan: done, Repository: "hydradb", Head: "head1"})
	if got.Phase != PhaseComplete {
		t.Fatalf("plan phase = %q, want %q", got.Phase, PhaseComplete)
	}
	if complete := condition(t, got, ConditionComplete); complete.Status != metav1.ConditionTrue {
		t.Fatalf("complete condition = %v, want %v", complete.Status, metav1.ConditionTrue)
	}

	failed := fixturePlan(true, items...)
	failed.Status.Items = []spec.PlanItemStatus{
		{ID: "a", Phase: ItemFailed},
		{ID: "b", Phase: ItemDone, Commit: "head1"},
	}
	got = Evaluate(Input{Plan: failed, Repository: "hydradb", Head: "head1"})
	if got.Phase != PhaseFailed {
		t.Fatalf("plan phase = %q, want %q", got.Phase, PhaseFailed)
	}
}

func TestEvaluatePlansAndResolvesFindings(t *testing.T) {
	plan := fixturePlan(true,
		spec.PlanItem{ID: "a", Findings: []string{"f1"}},
		spec.PlanItem{ID: "b", Findings: []string{"f2"}, Edits: []spec.PlanEdit{addReq("x", "r1")}},
	)
	plan.Status.Items = []spec.PlanItemStatus{{ID: "a", Phase: ItemDone, Commit: "commit1"}}
	got := Evaluate(Input{
		Plan:       plan,
		Repository: "hydradb",
		Contexts:   fixtureContexts("x", spec.SystemContextSpec{}),
		Head:       "head1",
	})

	if got.Planned["f1"] != "a" || got.Planned["f2"] != "b" {
		t.Fatalf("planned = %+v, want each finding keyed to its item", got.Planned)
	}
	if got.Resolved["f1"] != "commit1" {
		t.Fatalf("resolved = %+v, want f1 at the done item's commit", got.Resolved)
	}
	if _, ok := got.Resolved["f2"]; ok {
		t.Fatalf("resolved = %+v, want no entry for a finding whose item is not done", got.Resolved)
	}
}
