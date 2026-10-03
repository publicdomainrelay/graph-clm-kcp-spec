package specsync

import (
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
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

func TestDecide(t *testing.T) {
	observed := Observed(PartitionFacts(fixtureFacts(), "calc")[0])

	declared := []spec.Interface{{Name: "Add"}, {Name: "Multiply"}}
	decision := Decide(declared, observed, "")
	if !decision.CodeSynced || decision.Drifted {
		t.Errorf("decision = %+v, want synced and not drifted", decision)
	}

	decision = Decide([]spec.Interface{{Name: "Add"}}, observed, "")
	if decision.CodeSynced {
		t.Error("a declared interface set missing Multiply is not synced")
	}
	if len(decision.Undeclared) != 1 || decision.Undeclared[0] != "Multiply" {
		t.Errorf("undeclared = %v, want [Multiply]", decision.Undeclared)
	}

	decision = Decide([]spec.Interface{{Name: "Add"}, {Name: "Multiply"}, {Name: "Subtract"}}, observed, "")
	if decision.CodeSynced || len(decision.Missing) != 1 || decision.Missing[0] != "Subtract" {
		t.Errorf("decision = %+v, want Subtract missing", decision)
	}

	decision = Decide(declared, observed, "some-old-fingerprint")
	if !decision.Drifted {
		t.Error("a different previous fingerprint is drift")
	}
	decision = Decide(declared, observed, observed.Fingerprint)
	if decision.Drifted {
		t.Error("the same fingerprint is not drift")
	}
}
