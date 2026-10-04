package evalrun

import (
	"strings"
	"testing"
)

// TestMergeKeyedListKeepsWhatTheScenarioDoesNotName is the rule a scenario
// patch lives by: it changes the fields it names, keeps the ones it does not
// (a stored code ref is canonical and cannot be written by hand), and a
// {"$patch": "delete"} entry takes its key away.
func TestMergeKeyedListKeepsWhatTheScenarioDoesNotName(t *testing.T) {
	current := []any{
		map[string]any{"id": "r.add", "level": "MUST", "text": "Add sums.", "codeRefs": []any{"function:0a1b"}},
		map[string]any{"id": "r.sum", "level": "MUST", "text": "Sum totals.", "codeRefs": []any{"function:2c3d"}},
	}
	patch := []any{
		map[string]any{"id": "r.add", "text": "Add returns the sum of two integers."},
		map[string]any{"$patch": "delete", "id": "r.sum"},
		map[string]any{"id": "r.abs", "level": "SHOULD", "text": "Abs is absolute."},
	}
	merged, err := mergeKeyedList(current, patch, "id")
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want the deleted entry gone", merged)
	}
	first := merged[0].(map[string]any)
	if first["text"] != "Add returns the sum of two integers." || first["level"] != "MUST" {
		t.Errorf("the patched entry = %+v, want the new text over the stored level", first)
	}
	if refs, ok := first["codeRefs"].([]any); !ok || len(refs) != 1 || refs[0] != "function:0a1b" {
		t.Errorf("the stored code ref was lost: %+v", first["codeRefs"])
	}
	second := merged[1].(map[string]any)
	if second["id"] != "r.abs" {
		t.Errorf("the added entry did not land at the end: %+v", merged)
	}
}

func TestMergeKeyedListRefusesAnEntryWithNoKey(t *testing.T) {
	if _, err := mergeKeyedList(nil, []any{map[string]any{"text": "no id"}}, "id"); err == nil {
		t.Fatal("an entry with no key was accepted")
	}
}

// TestMergeKeyedListDeletesByTextWhenTheKeyIsTheModels is the case a live
// code -> spec half creates: the requirement ids are the model's, so a scenario
// that means "the requirement about Save" names the words. A delete that
// matches nothing has to fail loudly, or a scenario that removed nothing would
// be measured as a smaller delta.
func TestMergeKeyedListDeletesByTextWhenTheKeyIsTheModels(t *testing.T) {
	current := []any{
		map[string]any{"id": "r.save-json", "text": "Save writes the store's entries to a JSON file."},
		map[string]any{"id": "r.load-json", "text": "Load reads the entries back."},
	}
	patch := []any{map[string]any{"$patch": "delete", "id": "r.save", "text": "Save writes"}}
	merged, err := mergeKeyedList(current, patch, "id")
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || merged[0].(map[string]any)["id"] != "r.load-json" {
		t.Fatalf("merged = %+v, want the Save requirement gone by its words", merged)
	}

	_, err = mergeKeyedList(current, []any{map[string]any{"$patch": "delete", "id": "r.gone", "text": "nothing says this"}}, "id")
	if err == nil || !strings.Contains(err.Error(), "no entry matched") {
		t.Fatalf("err = %v, want a delete that matched nothing to fail", err)
	}
	if !strings.Contains(err.Error(), "r.save-json") {
		t.Errorf("err = %v, want it to name what the spec holds", err)
	}
}

// TestCheckPatchRefsRefusesABareName is the guard that names the scenario
// instead of the object: a server side write stores a ref exactly as written,
// and a bare name is refused later by the validator with an error about the
// SystemContext.
func TestCheckPatchRefsRefusesABareName(t *testing.T) {
	patch := map[string]any{"requirements": []any{
		map[string]any{"id": "r.x", "codeRefs": []any{"function:Add", "file:calc/calc.go"}},
	}}
	if err := checkPatchRefs("calc", patch); err != nil {
		t.Fatalf("a prefixed ref was refused: %v", err)
	}
	bare := map[string]any{"requirements": []any{
		map[string]any{"id": "r.x", "codeRefs": []any{"Add"}},
	}}
	err := checkPatchRefs("calc", bare)
	if err == nil {
		t.Fatal("a bare code ref was accepted")
	}
	if !strings.Contains(err.Error(), `"Add"`) || !strings.Contains(err.Error(), "calc") {
		t.Errorf("err = %v, want it to name the ref and the context", err)
	}
}

func TestScenarioResolvedBuildsTheSingleTargetShorthand(t *testing.T) {
	single := Scenario{
		Context:              "calc",
		SpecPatch:            map[string]any{"interfaces": []any{}},
		ExpectedInterfaces:   []string{"Subtract"},
		ExpectedDeltaEntries: 2,
		Realize:              nil,
	}
	targets := single.Resolved()
	if len(targets) != 1 || targets[0].Context != "calc" || targets[0].ExpectedDeltaEntries != 2 {
		t.Fatalf("targets = %+v", targets)
	}
	multi := Scenario{Context: "domain", Targets: []Target{{Context: "domain"}, {Context: "storage"}}}
	if got := multi.Resolved(); len(got) != 2 || got[1].Context != "storage" {
		t.Fatalf("targets = %+v", got)
	}
}
