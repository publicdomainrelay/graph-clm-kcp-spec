package specsync

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func fixtureFacts() Facts {
	return Facts{
		Commit: "abc123",
		Files: []SourceFile{
			{Path: "calc/calc.go", Language: "go"},
			{Path: "calc/calc_test.go", Language: "go"},
			{Path: "cmd/calc/main.go", Language: "go"},
		},
		Symbols: []Symbol{
			{ID: "function:add", Name: "Add", Kind: "function", Signature: "(a, b int) int", File: "calc/calc.go", Line: 4, Exported: true},
			{ID: "function:mul", Name: "Multiply", Kind: "function", Signature: "(a, b int) int", File: "calc/calc.go", Line: 9, Exported: true},
			{ID: "function:testadd", Name: "TestAdd", Kind: "function", Signature: "(t *testing.T)", File: "calc/calc_test.go", Line: 5, Exported: true},
			{ID: "function:main", Name: "main", Kind: "function", Signature: "()", File: "cmd/calc/main.go", Line: 11, Exported: false},
			{ID: "function:helper", Name: "Helper", Kind: "function", Signature: "()", File: "cmd/calc/main.go", Line: 30, Exported: true},
		},
	}
}

func TestPartitionFactsSplitsByDirectory(t *testing.T) {
	partitions := PartitionFacts(fixtureFacts(), "calc")
	if len(partitions) != 2 {
		t.Fatalf("got %d partitions, want 2: %+v", len(partitions), partitions)
	}
	if partitions[0].Name != "calc" || partitions[0].Directory != "calc" {
		t.Errorf("first partition = %+v, want the calc directory", partitions[0])
	}
	if partitions[1].Name != "cmd-calc" || partitions[1].Directory != "cmd/calc" {
		t.Errorf("second partition = %+v, want cmd-calc", partitions[1])
	}
	if len(partitions[0].Files) != 2 {
		t.Errorf("calc files = %v, want both calc files", partitions[0].Files)
	}
}

func TestPartitionFactsKeepsExportedNonTestSymbols(t *testing.T) {
	partitions := PartitionFacts(fixtureFacts(), "calc")
	observed := Observed(partitions[0])
	if len(observed.Interfaces) != 2 {
		t.Fatalf("interfaces = %+v, want Add and Multiply only", observed.Interfaces)
	}
	if observed.Interfaces[0].Name != "Add" || observed.Interfaces[0].Line != 4 {
		t.Errorf("first interface = %+v", observed.Interfaces[0])
	}
	if observed.Interfaces[0].CodegraphID != "function:add" {
		t.Errorf("codegraph id = %q, want function:add", observed.Interfaces[0].CodegraphID)
	}
	if observed.Files[0] != "calc/calc.go" || observed.Files[1] != "calc/calc_test.go" {
		t.Errorf("files = %v, want both sorted", observed.Files)
	}
}

func TestPartitionFactsNamesRootFilesAfterTheRepository(t *testing.T) {
	partitions := PartitionFacts(Facts{
		Files: []SourceFile{{Path: "main.go", Language: "go"}},
	}, "my-repo")
	if len(partitions) != 1 || partitions[0].Name != "my-repo" {
		t.Fatalf("partitions = %+v, want one named my-repo", partitions)
	}
}

func TestPartitionFactsAvoidsNameCollisions(t *testing.T) {
	partitions := PartitionFacts(Facts{
		Files: []SourceFile{
			{Path: "a/b/x.go", Language: "go"},
			{Path: "a-b/y.go", Language: "go"},
		},
	}, "repo")
	if len(partitions) != 2 {
		t.Fatalf("partitions = %+v", partitions)
	}
	if partitions[0].Name == partitions[1].Name {
		t.Fatalf("names collide: %+v", partitions)
	}
}

func TestFingerprintIsOrderIndependentAndContentSensitive(t *testing.T) {
	partitions := PartitionFacts(fixtureFacts(), "calc")
	first := Observed(partitions[0])

	reordered := Facts{
		Files:   []SourceFile{fixtureFacts().Files[1], fixtureFacts().Files[0], fixtureFacts().Files[2]},
		Symbols: []Symbol{fixtureFacts().Symbols[1], fixtureFacts().Symbols[0]},
	}
	second := Observed(PartitionFacts(reordered, "calc")[0])
	if first.Fingerprint != second.Fingerprint {
		t.Errorf("fingerprints differ for the same facts: %s != %s", first.Fingerprint, second.Fingerprint)
	}

	changed := first
	changed.Interfaces = append([]spec.ObservedInterface{}, first.Interfaces...)
	changed.Interfaces[0].Signature = "(a, b int64) int64"
	if Fingerprint(changed) == first.Fingerprint {
		t.Error("a changed signature must change the fingerprint")
	}
}

func TestObservedIsStableAcrossRuns(t *testing.T) {
	first := Observed(PartitionFacts(fixtureFacts(), "calc")[0])
	second := Observed(PartitionFacts(fixtureFacts(), "calc")[0])
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("two runs disagree: %s != %s", first.Fingerprint, second.Fingerprint)
	}
}

func decideWith(observed spec.ObservedFacts, synced string, declared []spec.Interface, requirements ...spec.Requirement) Decision {
	return Decide(Input{
		Name:              "calc",
		Generation:        1,
		Spec:              spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf, Interfaces: declared, Requirements: requirements},
		Observed:          observed,
		SyncedFingerprint: synced,
	})
}

func TestDecideComparesTheDeclaredSurface(t *testing.T) {
	observed := Observed(PartitionFacts(fixtureFacts(), "calc")[0])

	declared := []spec.Interface{{Name: "Add"}, {Name: "Multiply"}}
	decision := decideWith(observed, "", declared)
	if !decision.SpecValid || !decision.CodeSynced || decision.Drifted {
		t.Errorf("decision = %+v, want valid, synced and not drifted", decision)
	}

	decision = decideWith(observed, "", []spec.Interface{{Name: "Add"}})
	if decision.CodeSynced {
		t.Error("a declared interface set missing Multiply is not synced")
	}
	if len(decision.Undeclared) != 1 || decision.Undeclared[0] != "Multiply" {
		t.Errorf("undeclared = %v, want [Multiply]", decision.Undeclared)
	}

	decision = decideWith(observed, "", []spec.Interface{{Name: "Add"}, {Name: "Multiply"}, {Name: "Subtract"}})
	if decision.CodeSynced || len(decision.Missing) != 1 || decision.Missing[0] != "Subtract" {
		t.Errorf("decision = %+v, want Subtract missing", decision)
	}
}

func TestDecideResolvesRequirementCodeRefs(t *testing.T) {
	observed := Observed(PartitionFacts(fixtureFacts(), "calc")[0])
	declared := []spec.Interface{{Name: "Add"}, {Name: "Multiply"}}

	resolved := decideWith(observed, "", declared,
		spec.Requirement{ID: "r.add", Level: spec.LevelMust, Text: "Add.", CodeRefs: []string{"function:Add", "function:add", "file:calc/calc.go"}})
	if !resolved.CodeSynced {
		t.Errorf("decision = %+v, want the code refs to resolve", resolved)
	}

	unresolved := decideWith(observed, "", declared,
		spec.Requirement{ID: "r.subtract", Level: spec.LevelMust, Text: "Subtract.", CodeRefs: []string{"function:Subtract"}})
	if unresolved.CodeSynced {
		t.Error("a code ref that names no observed interface is not synced")
	}
	if len(unresolved.Unresolved) != 1 || unresolved.Unresolved[0] != "r.subtract: function:Subtract" {
		t.Errorf("unresolved = %v", unresolved.Unresolved)
	}
}

func TestDecideDriftsAgainstTheSyncedFingerprint(t *testing.T) {
	observed := Observed(PartitionFacts(fixtureFacts(), "calc")[0])
	declared := []spec.Interface{{Name: "Add"}, {Name: "Multiply"}}

	if decideWith(observed, observed.Fingerprint, declared).Drifted {
		t.Error("the same fingerprint is not drift")
	}
	if !decideWith(observed, "some-old-fingerprint", declared).Drifted {
		t.Error("a different synced fingerprint is drift")
	}
	// Before the first ingest there is no baseline, so nothing has drifted.
	if decideWith(observed, "", declared).Drifted {
		t.Error("an unset synced fingerprint is not drift")
	}
}

func TestConditionsKeepTheTransitionTimeAndCarryTheGeneration(t *testing.T) {
	observed := Observed(PartitionFacts(fixtureFacts(), "calc")[0])
	in := Input{
		Name:              "calc",
		Generation:        3,
		Spec:              spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf, Interfaces: []spec.Interface{{Name: "Add"}, {Name: "Multiply"}}},
		Observed:          observed,
		SyncedFingerprint: observed.Fingerprint,
	}

	first, conditions := Conditions(in, nil)
	if !first.CodeSynced || first.Drifted {
		t.Fatalf("decision = %+v", first)
	}
	if len(conditions) != 3 {
		t.Fatalf("conditions = %+v, want three", conditions)
	}
	for _, declaredCondition := range conditions {
		if declaredCondition.ObservedGeneration != 3 {
			t.Errorf("condition %s observedGeneration = %d, want 3", declaredCondition.Type, declaredCondition.ObservedGeneration)
		}
	}
	for _, conditionType := range []string{specapi.ConditionSpecValid, specapi.ConditionCodeSynced} {
		if got := conditionOf(t, conditions, conditionType); got.Status != "True" {
			t.Errorf("condition %s = %s, want True", conditionType, got.Status)
		}
	}
	if got := conditionOf(t, conditions, specapi.ConditionDrifted); got.Status != "False" {
		t.Errorf("condition Drifted = %s, want False", got.Status)
	}

	in.Generation = 4
	in.Observed = spec.ObservedFacts{Files: observed.Files, Interfaces: observed.Interfaces, Fingerprint: "a-new-fingerprint"}
	second, drifted := Conditions(in, conditions)
	if !second.Drifted {
		t.Fatalf("decision = %+v, want drift", second)
	}
	validBefore := conditionOf(t, conditions, specapi.ConditionSpecValid)
	validAfter := conditionOf(t, drifted, specapi.ConditionSpecValid)
	if validAfter.ObservedGeneration != 4 {
		t.Errorf("observedGeneration = %d, want 4", validAfter.ObservedGeneration)
	}
	if !validAfter.LastTransitionTime.Equal(&validBefore.LastTransitionTime) {
		t.Error("an unchanged condition must keep its transition time")
	}

	driftedBefore := conditionOf(t, conditions, specapi.ConditionDrifted)
	driftedAfter := conditionOf(t, drifted, specapi.ConditionDrifted)
	if driftedAfter.LastTransitionTime.Equal(&driftedBefore.LastTransitionTime) {
		t.Error("a changed condition must move its transition time")
	}
}

func conditionOf(t *testing.T, conditions []metav1.Condition, conditionType string) metav1.Condition {
	t.Helper()
	for _, entry := range conditions {
		if entry.Type == conditionType {
			return entry
		}
	}
	t.Fatalf("%s is not in %+v", conditionType, conditions)
	return metav1.Condition{}
}

func TestSpecChangeDue(t *testing.T) {
	if !CodeToSpecDue(true, "aaa", "bbb") {
		t.Error("drift with a commit pair is due")
	}
	for _, due := range []bool{
		CodeToSpecDue(false, "aaa", "bbb"),
		CodeToSpecDue(true, "aaa", "aaa"),
		CodeToSpecDue(true, "", "bbb"),
		CodeToSpecDue(true, "aaa", ""),
	} {
		if due {
			t.Error("a change is due only while drifted, with a usable commit pair")
		}
	}

	if !SpecEditDue("hash-a", "hash-b") {
		t.Error("a spec that no longer hashes to the realized hash is a pending edit")
	}
	for _, due := range []bool{
		SpecEditDue("hash-a", "hash-a"),
		SpecEditDue("hash-a", ""),
		SpecEditDue("", "hash-b"),
	} {
		if due {
			t.Error("an edit is due only against a realized baseline")
		}
	}
}
