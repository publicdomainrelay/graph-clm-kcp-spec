package spec

import (
	"reflect"
	"testing"
)

func canonicalBase() SystemContextSpec {
	return SystemContextSpec{
		Repository: "calc",
		Upstream:   RefSelf,
		Intent:     "Arithmetic on two integers.",
		Requirements: []Requirement{
			{ID: "r.multiply", Level: LevelMust, Text: "Multiply returns the product.", CodeRefs: []string{"function:Multiply", "file:calc/calc.go"}},
			{ID: "r.add", Level: LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"function:Add"}},
		},
		Interfaces: []Interface{
			{Name: "Multiply", Kind: "function"},
			{Name: "Add", Kind: "function"},
		},
		CodeRefs:   []string{"function:Add", "file:calc/calc.go", "function:Add"},
		Overlay:    []string{"sc.base"},
		DependsOn:  []string{"sc.other"},
		Introduces: []string{"sc.new"},
	}
}

func TestCanonicalizeOrdersKeyedListsAndSets(t *testing.T) {
	canonical := Canonicalize(canonicalBase())
	if got := []string{canonical.Requirements[0].ID, canonical.Requirements[1].ID}; !reflect.DeepEqual(got, []string{"r.add", "r.multiply"}) {
		t.Errorf("requirements = %v, want them ordered by id", got)
	}
	if got := []string{canonical.Interfaces[0].Name, canonical.Interfaces[1].Name}; !reflect.DeepEqual(got, []string{"Add", "Multiply"}) {
		t.Errorf("interfaces = %v, want them ordered by name", got)
	}
	if want := []string{"file:calc/calc.go", "function:Add"}; !reflect.DeepEqual(canonical.CodeRefs, want) {
		t.Errorf("codeRefs = %v, want a sorted set", canonical.CodeRefs)
	}
	if want := []string{"file:calc/calc.go", "function:Multiply"}; !reflect.DeepEqual(canonical.Requirements[1].CodeRefs, want) {
		t.Errorf("requirement codeRefs = %v, want a sorted set", canonical.Requirements[1].CodeRefs)
	}
}

func TestHashIgnoresTheOrderOfKeyedLists(t *testing.T) {
	first, err := HashSystemContextSpec(canonicalBase())
	if err != nil {
		t.Fatal(err)
	}
	reordered := canonicalBase()
	reordered.Requirements[0], reordered.Requirements[1] = reordered.Requirements[1], reordered.Requirements[0]
	reordered.Interfaces[0], reordered.Interfaces[1] = reordered.Interfaces[1], reordered.Interfaces[0]
	reordered.CodeRefs = []string{"file:calc/calc.go", "function:Add"}
	second, err := HashSystemContextSpec(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the hash moved on a reorder: %s != %s", first, second)
	}

	edited := reordered
	edited.Intent = "Something else."
	third, err := HashSystemContextSpec(edited)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Error("a real edit hashed the same")
	}
}

func TestSameDeclaredStateIgnoresTheDerivedCodeRefs(t *testing.T) {
	withFiles := canonicalBase()
	withoutFiles := canonicalBase()
	withoutFiles.CodeRefs = nil

	if !SameDeclaredState(withFiles, withoutFiles) {
		t.Error("a difference in the derived code refs is not a human edit")
	}

	edited := withoutFiles
	edited.Intent = "A human edit."
	if SameDeclaredState(withFiles, edited) {
		t.Error("an intent edit was read as the same state")
	}

	reordered := canonicalBase()
	reordered.Requirements[0], reordered.Requirements[1] = reordered.Requirements[1], reordered.Requirements[0]
	if !SameDeclaredState(withFiles, reordered) {
		t.Error("a reorder of a keyed list is not an edit")
	}
}
