package authority

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func baseSpec() spec.SystemContextSpec {
	return spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   spec.RefSelf,
		Intent:     "Arithmetic on two integers.",
		Requirements: []spec.Requirement{
			{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"function:a"}},
			{ID: "r.multiply", Level: spec.LevelMust, Text: "Multiply returns the product."},
		},
		Interfaces: []spec.Interface{{Name: "Add", Kind: "function"}},
	}
}

func TestParseAndFormatRoundTrip(t *testing.T) {
	units := NewSet(RequirementUnit("r.add"), UnitIntent, InterfaceUnit("Add"))
	formatted := Format(units)
	if formatted != `["intent","interface/Add","requirement/r.add"]` {
		t.Fatalf("Format = %s", formatted)
	}
	parsed, err := Parse(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Equal(units) {
		t.Errorf("Parse(Format) = %v, want %v", parsed.Sorted(), units.Sorted())
	}
	if empty, err := Parse(""); err != nil || len(empty) != 0 {
		t.Errorf("Parse empty = %v %v", empty, err)
	}
	if Format(Set{}) != "" {
		t.Error("an empty set formats to the empty annotation")
	}
}

func TestParseRejectsWhatIsNotAUnit(t *testing.T) {
	for _, bad := range []string{`{"a":1}`, `["requirement/"]`, `["spec.intent"]`} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%s) accepted it", bad)
		}
	}
}

func TestChangedIgnoresCodeRefOnlyRequirementChanges(t *testing.T) {
	from := baseSpec()
	to := baseSpec()
	to.Requirements[0].CodeRefs = []string{"function:b"}
	if changed := Changed(from, to); len(changed) != 0 {
		t.Errorf("Changed = %v, want none for a code ref move", changed.Sorted())
	}
	to.Requirements[1].Text = "Multiply returns the product, or overflows."
	to.Intent = "Integer arithmetic."
	to.Interfaces = nil
	to.Requirements = append(to.Requirements, spec.Requirement{ID: "r.sub", Level: spec.LevelShould, Text: "Subtract."})
	want := NewSet(UnitIntent, RequirementUnit("r.multiply"), RequirementUnit("r.sub"), InterfaceUnit("Add"))
	if changed := Changed(from, to); !changed.Equal(want) {
		t.Errorf("Changed = %v, want %v", changed.Sorted(), want.Sorted())
	}
}

func TestDeclaredAddsTheUnrealizedEdit(t *testing.T) {
	realized := baseSpec()
	current := baseSpec()
	current.Requirements[1].Level = spec.LevelShould
	declared := Declared(NewSet(UnitIntent), current, &realized)
	want := NewSet(UnitIntent, RequirementUnit("r.multiply"))
	if !declared.Equal(want) {
		t.Errorf("Declared = %v, want %v", declared.Sorted(), want.Sorted())
	}
	if only := Declared(NewSet(UnitIntent), current, nil); !only.Equal(NewSet(UnitIntent)) {
		t.Errorf("Declared without a realized spec = %v", only.Sorted())
	}
}

func TestGuardKeepsDeclaredUnitsAndProposes(t *testing.T) {
	current := baseSpec()
	current.Requirements = append(current.Requirements, spec.Requirement{
		ID: "r.human", Level: spec.LevelMust, Text: "Division by zero returns an error.",
	})
	drafted := baseSpec()
	drafted.Intent = "The model's paragraph."
	drafted.Requirements[0].Text = "Add returns a+b."
	drafted.Requirements = append(drafted.Requirements, spec.Requirement{
		ID: "r.model", Level: spec.LevelShould, Text: "Something the model saw.",
	})

	declared := NewSet(RequirementUnit("r.human"), UnitIntent, RequirementUnit("r.gone"))
	merged, proposals := Guard(current, drafted, declared)

	if merged.Intent != current.Intent {
		t.Errorf("intent = %q, want the declared one kept", merged.Intent)
	}
	ids := []string{}
	for _, requirement := range spec.CanonicalRequirements(merged.Requirements) {
		ids = append(ids, requirement.ID)
	}
	if strings.Join(ids, ",") != "r.add,r.human,r.model,r.multiply" {
		t.Errorf("requirements = %v", ids)
	}
	for _, requirement := range merged.Requirements {
		if requirement.ID == "r.add" && requirement.Text != "Add returns a+b." {
			t.Errorf("r.add = %q, want the derived unit rewritten", requirement.Text)
		}
	}

	units := []string{}
	for _, proposal := range proposals {
		units = append(units, proposal.Unit)
	}
	if strings.Join(units, ",") != "intent,requirement/r.human" {
		t.Fatalf("proposals = %+v", proposals)
	}
	if proposals[0].Current != current.Intent || proposals[0].Proposed != drafted.Intent {
		t.Errorf("intent proposal = %+v", proposals[0])
	}
	if proposals[1].Proposed != "" || !strings.Contains(proposals[1].Current, "Division by zero") {
		t.Errorf("removal proposal = %+v", proposals[1])
	}
}

func TestGuardKeepsADeclaredRemoval(t *testing.T) {
	current := baseSpec()
	current.Requirements = current.Requirements[:1]
	drafted := baseSpec()
	merged, proposals := Guard(current, drafted, NewSet(RequirementUnit("r.multiply")))
	if len(merged.Requirements) != 1 || merged.Requirements[0].ID != "r.add" {
		t.Errorf("requirements = %+v, want the person's removal kept", merged.Requirements)
	}
	if len(proposals) != 1 || proposals[0].Current != "" || proposals[0].Proposed == "" {
		t.Errorf("proposals = %+v, want one re-add proposal", proposals)
	}
}

func TestGuardWithNothingDeclaredIsTheDraft(t *testing.T) {
	current := baseSpec()
	drafted := baseSpec()
	drafted.Intent = "new"
	merged, proposals := Guard(current, drafted, Set{})
	if merged.Intent != "new" || len(proposals) != 0 {
		t.Errorf("merged intent %q proposals %+v", merged.Intent, proposals)
	}
}

func TestSubstituteTakesTheRealizedSideForDeclaredUnits(t *testing.T) {
	realized := baseSpec()
	merged := baseSpec()
	merged.Requirements[1].Text = "the person's pending edit"
	merged.Intent = "the model's new paragraph"
	out := Substitute(merged, realized, NewSet(RequirementUnit("r.multiply")))
	if out.Intent != "the model's new paragraph" {
		t.Errorf("intent = %q, want the derived unit untouched", out.Intent)
	}
	if out.Requirements[1].Text != realized.Requirements[1].Text {
		t.Errorf("r.multiply = %q, want the realized text", out.Requirements[1].Text)
	}
}

func TestMergeLeavesAPendingEditUnrealized(t *testing.T) {
	realized := baseSpec()
	current := baseSpec()
	current.Requirements[1].Text = "Multiply saturates on overflow."
	drafted := baseSpec()
	drafted.Intent = "The model's paragraph."

	write := Merge(Set{}, current, &realized, drafted)
	if !write.Declared.Equal(NewSet(RequirementUnit("r.multiply"))) {
		t.Fatalf("declared = %v", write.Declared.Sorted())
	}
	if write.Spec.Requirements[1].Text != "Multiply saturates on overflow." {
		t.Errorf("spec r.multiply = %q, want the pending edit kept", write.Spec.Requirements[1].Text)
	}
	if write.Realized.Requirements[1].Text != realized.Requirements[1].Text {
		t.Errorf("realized r.multiply = %q, want the realized text", write.Realized.Requirements[1].Text)
	}
	if write.Spec.Intent != "The model's paragraph." || write.Realized.Intent != write.Spec.Intent {
		t.Errorf("intent spec %q realized %q, want the derived unit in both", write.Spec.Intent, write.Realized.Intent)
	}
	if len(write.Proposals) != 1 || write.Proposals[0].Unit != RequirementUnit("r.multiply") {
		t.Errorf("proposals = %+v", write.Proposals)
	}
}
