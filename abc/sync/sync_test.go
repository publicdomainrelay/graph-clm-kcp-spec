package specsync

import (
	"reflect"
	"strings"
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

func sharedMethodFacts() Facts {
	return Facts{
		Files: []SourceFile{{Path: "store/store.go", Language: "go"}},
		Symbols: []Symbol{
			{ID: "method:setlist", Name: "List", Qualified: "Set.List", Kind: "method", File: "store/store.go", Line: 10, Exported: true},
			{ID: "method:indexerlist", Name: "List", Qualified: "Indexer.List", Kind: "method", File: "store/store.go", Line: 20, Exported: true},
			{ID: "function:newstore", Name: "NewStore", Kind: "function", File: "store/store.go", Line: 2, Exported: true},
		},
	}
}

func TestObservedKeepsTwoTypesThatShareAMethodName(t *testing.T) {
	partitions := PartitionFacts(sharedMethodFacts(), "store")
	observed := Observed(partitions[0])
	names := []string{}
	for _, entry := range observed.Interfaces {
		names = append(names, entry.Name)
	}
	want := map[string]bool{"Set.List": true, "Indexer.List": true, "NewStore": true}
	if len(names) != len(want) {
		t.Fatalf("interfaces = %v, want %v", names, want)
	}
	for _, name := range names {
		if !want[name] {
			t.Errorf("interface %q is not one of %v", name, want)
		}
	}
	for _, entry := range observed.Interfaces {
		if entry.Name == "NewStore" && entry.Kind != "function" {
			t.Errorf("NewStore kind = %q", entry.Kind)
		}
		if entry.Name == "List" {
			t.Errorf("a method was keyed by its bare name")
		}
	}
}

func TestMigrateDeclaredTakesTheReceiverWhenItIsUnambiguous(t *testing.T) {
	observed := Observed(PartitionFacts(sharedMethodFacts(), "store")[0])
	collided := MigrateDeclared(spec.SystemContextSpec{
		Interfaces:   []spec.Interface{{Name: "List"}},
		Requirements: []spec.Requirement{{ID: "r.list", CodeRefs: []string{"function:List"}}},
	}, observed)
	if collided.Interfaces[0].Name != "List" || collided.Requirements[0].CodeRefs[0] != "function:List" {
		t.Fatalf("an ambiguous name was rewritten: %+v", collided)
	}

	single := Observed(PartitionFacts(Facts{
		Files: []SourceFile{{Path: "store/store.go", Language: "go"}},
		Symbols: []Symbol{
			{ID: "method:indexerlist", Name: "List", Qualified: "Indexer.List", Kind: "method", File: "store/store.go", Line: 20, Exported: true},
		},
	}, "store")[0])
	migrated := MigrateDeclared(spec.SystemContextSpec{
		Interfaces:   []spec.Interface{{Name: "List"}, {Name: "Indexer"}},
		Requirements: []spec.Requirement{{ID: "r.list", CodeRefs: []string{"List", "function:List", "file:store/store.go"}}},
	}, single)
	if migrated.Interfaces[0].Name != "Indexer.List" {
		t.Errorf("declared interface = %q, want Indexer.List", migrated.Interfaces[0].Name)
	}
	if migrated.Interfaces[1].Name != "Indexer" {
		t.Errorf("an unqualified name must not move: %q", migrated.Interfaces[1].Name)
	}
	got := migrated.Requirements[0].CodeRefs
	if got[0] != "Indexer.List" || got[1] != "function:Indexer.List" || got[2] != "file:store/store.go" {
		t.Errorf("code refs = %v", got)
	}
}

func TestMigrateDeclaredLeavesASpecWithNothingToMigrateAlone(t *testing.T) {
	observed := Observed(PartitionFacts(sharedMethodFacts(), "store")[0])
	declared := spec.SystemContextSpec{
		Repository:   "store",
		Requirements: []spec.Requirement{{ID: "r.list", CodeRefs: []string{"file:store/store.go"}}},
		Interfaces:   nil,
		CodeRefs:     []string{"file:store/store.go"},
	}
	migrated := MigrateDeclared(declared, observed)
	if !reflect.DeepEqual(migrated, declared) {
		t.Errorf("a spec with nothing to migrate moved:\n got %+v\nwant %+v", migrated, declared)
	}
	empty := spec.SystemContextSpec{Repository: "store"}
	if got := MigrateDeclared(empty, observed); !reflect.DeepEqual(got, empty) {
		t.Errorf("an empty spec moved: %+v", got)
	}
}

func TestReanchorRefsFollowsASymbolThatMovedLines(t *testing.T) {
	previous := spec.ObservedFacts{Interfaces: []spec.ObservedInterface{
		{Name: "Add", Kind: "function", File: "calc/calc.go", Line: 4, CodegraphID: "function:aaa"},
		{Name: "Multiply", Kind: "function", File: "calc/calc.go", Line: 9, CodegraphID: "function:bbb"},
		{Name: "Gone", Kind: "function", File: "calc/calc.go", Line: 20, CodegraphID: "function:zzz"},
	}}
	observed := spec.ObservedFacts{Interfaces: []spec.ObservedInterface{
		{Name: "Add", Kind: "function", File: "calc/calc.go", Line: 6, CodegraphID: "function:ccc"},
		{Name: "Multiply", Kind: "function", File: "calc/calc.go", Line: 11, CodegraphID: "function:ddd"},
	}}
	declared := spec.SystemContextSpec{
		CodeRefs: []string{"function:aaa", "file:calc/calc.go"},
		Requirements: []spec.Requirement{
			{ID: "r.add", CodeRefs: []string{"function:aaa", "Add"}},
			{ID: "r.mul", CodeRefs: []string{"function:bbb"}},
			{ID: "r.gone", CodeRefs: []string{"function:zzz"}},
			{ID: "r.file", CodeRefs: []string{"file:calc/calc.go"}},
		},
	}
	got := ReanchorRefs(declared, previous, observed)
	if got.CodeRefs[0] != "function:ccc" || got.CodeRefs[1] != "file:calc/calc.go" {
		t.Errorf("context refs = %v", got.CodeRefs)
	}
	if refs := got.Requirements[0].CodeRefs; len(refs) != 2 || refs[0] != "function:ccc" || refs[1] != "Add" {
		t.Errorf("r.add refs = %v", refs)
	}
	if refs := got.Requirements[1].CodeRefs; len(refs) != 1 || refs[0] != "function:ddd" {
		t.Errorf("r.mul refs = %v", refs)
	}
	if refs := got.Requirements[2].CodeRefs; len(refs) != 1 || refs[0] != "function:zzz" {
		t.Errorf("a symbol that is gone must stay unresolved, not point elsewhere: %v", refs)
	}
	if refs := got.Requirements[3].CodeRefs; len(refs) != 1 || refs[0] != "file:calc/calc.go" {
		t.Errorf("r.file refs = %v", refs)
	}
	if declared.Requirements[0].CodeRefs[0] != "function:aaa" {
		t.Error("ReanchorRefs mutated the spec it was given")
	}
	if unresolved := UnresolvedCodeRefs(got.Requirements[:2], observed); len(unresolved) > 0 {
		t.Errorf("the re-anchored requirements do not resolve: %v", unresolved)
	}
}

func TestReanchorRefsLeavesAMovedNameAndAnUnmovedIDAlone(t *testing.T) {
	observed := spec.ObservedFacts{Interfaces: []spec.ObservedInterface{
		{Name: "Add", Kind: "function", File: "calc/calc.go", Line: 4, CodegraphID: "function:aaa"},
	}}
	declared := spec.SystemContextSpec{
		Requirements: []spec.Requirement{{ID: "r.add", CodeRefs: []string{"function:aaa"}}},
	}
	if got := ReanchorRefs(declared, spec.ObservedFacts{}, observed); !reflect.DeepEqual(got, declared) {
		t.Errorf("an unchanged id moved: %+v", got)
	}
	twice := spec.ObservedFacts{Interfaces: []spec.ObservedInterface{
		{Name: "Add", Kind: "function", File: "calc/calc.go", Line: 8, CodegraphID: "function:ccc"},
		{Name: "Add", Kind: "function", File: "calc/other.go", Line: 3, CodegraphID: "function:eee"},
	}}
	got := ReanchorRefs(declared, observed, twice)
	if got.Requirements[0].CodeRefs[0] != "function:aaa" {
		t.Errorf("an ambiguous name picked a symbol: %v", got.Requirements[0].CodeRefs)
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

func TestObservedKeysInterfacesByName(t *testing.T) {
	partition := Partition{
		Name:  "archyaml",
		Files: []string{"abc/archyaml/parse.go", "abc/archyaml/export.go"},
		Symbols: []Symbol{
			{ID: "type:1", Name: "Section", Kind: "type_alias", File: "abc/archyaml/parse.go", Line: 12, Exported: true},
			{ID: "type:2", Name: "Section", Kind: "type_alias", File: "abc/archyaml/export.go", Line: 4, Exported: true},
			{ID: "type:3", Name: "Node", Kind: "struct", File: "abc/archyaml/parse.go", Line: 20, Exported: true},
		},
	}
	observed := Observed(partition)
	if len(observed.Interfaces) != 2 {
		t.Fatalf("interfaces = %+v, want one entry per name", observed.Interfaces)
	}
	if observed.Interfaces[0].Name != "Section" || observed.Interfaces[0].File != "abc/archyaml/parse.go" {
		t.Errorf("the first entry kept is %+v, want the first in file order", observed.Interfaces[0])
	}
	if observed.Interfaces[1].Name != "Node" {
		t.Errorf("interfaces = %+v", observed.Interfaces)
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

	if !SpecEditDue("hash-a", "hash-b", "") {
		t.Error("a spec that no longer hashes to the realized hash is a pending edit")
	}
	for _, due := range []bool{
		SpecEditDue("hash-a", "hash-a", ""),
		SpecEditDue("hash-a", "", ""),
		SpecEditDue("", "hash-b", ""),
	} {
		if due {
			t.Error("an edit is due only against a realized baseline")
		}
	}
	if SpecEditDue("hash-a", "hash-b", "hash-a") {
		t.Error("a spec that is still the tool's own write is not a pending edit")
	}
	if !SpecEditDue("hash-c", "hash-b", "hash-a") {
		t.Error("a spec that is neither the realized hash nor the tool's write is a pending edit")
	}
}

func moduleFacts() Facts {
	return Facts{
		Commit: "abc123",
		Files: []SourceFile{
			{Path: "greet/mod.ts", Language: "typescript"},
			{Path: "greet/format/mod.ts", Language: "typescript"},
			{Path: "calc/calc/calc.go", Language: "go"},
		},
		Symbols: []Symbol{
			{ID: "function:greet", Name: "greet", Kind: "function", File: "greet/mod.ts", Line: 6, Exported: true},
			{ID: "function:format", Name: "format", Kind: "function", File: "greet/format/mod.ts", Line: 1, Exported: true},
			{ID: "function:add", Name: "Add", Kind: "function", File: "calc/calc/calc.go", Line: 4, Exported: true},
		},
	}
}

func moduleRoots() []string {
	return []string{".", "calc", "greet"}
}

func TestPartitionByPackageGroupsByManifestRoot(t *testing.T) {
	partitions := PartitionFactsWith(moduleFacts(), PartitionOptions{
		Mode:           spec.PartitionPackage,
		Roots:          moduleRoots(),
		RepositoryName: "unseen",
	})
	if len(partitions) != 2 {
		t.Fatalf("got %d partitions, want 2: %+v", len(partitions), partitions)
	}
	if partitions[0].Name != "calc" || partitions[0].Directory != "calc" {
		t.Errorf("first partition = %+v, want the calc module", partitions[0])
	}
	if got := partitions[0].Files; len(got) != 1 {
		t.Errorf("calc files = %v, want the module's one file", got)
	}
	if partitions[1].Name != "greet" || partitions[1].Directory != "greet" {
		t.Errorf("second partition = %+v, want the greet module", partitions[1])
	}
	if got := partitions[1].Files; len(got) != 2 {
		t.Errorf("greet files = %v, want the module's two files", got)
	}
}

func TestPartitionByDirectorySplitsTheSameTreeFurther(t *testing.T) {
	partitions := PartitionFactsWith(moduleFacts(), PartitionOptions{RepositoryName: "unseen"})
	names := []string{}
	for _, partition := range partitions {
		names = append(names, partition.Name)
	}
	want := []string{"calc-calc", "greet", "greet-format"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("directory partition = %v, want %v", names, want)
	}
}

func TestIncludeAndExcludeGlobsFilterTheTree(t *testing.T) {
	partitions := PartitionFactsWith(moduleFacts(), PartitionOptions{
		Mode:           spec.PartitionPackage,
		Roots:          moduleRoots(),
		Exclude:        []string{"calc/**"},
		RepositoryName: "unseen",
	})
	if len(partitions) != 1 || partitions[0].Name != "greet" {
		t.Fatalf("partitions = %+v, want only greet", partitions)
	}
	if partitions := PartitionFactsWith(moduleFacts(), PartitionOptions{
		Include:        []string{"*.ts"},
		RepositoryName: "unseen",
	}); len(partitions) != 2 {
		t.Errorf("include *.ts kept %d partitions, want the two TypeScript directories: %+v", len(partitions), partitions)
	}
	if partitions := PartitionFactsWith(moduleFacts(), PartitionOptions{
		Exclude:        []string{"*_test.go"},
		RepositoryName: "unseen",
	}); len(partitions) != 3 {
		t.Errorf("exclude *_test.go changed a tree with no test files: %+v", partitions)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"calc/**", "calc/calc.go", true},
		{"calc/**", "calc", false},
		{"calc/**", "calc2/calc.go", false},
		{"**/*.go", "cmd/calc/main.go", true},
		{"**/*.go", "main.go", true},
		{"a/**/b.go", "a/b.go", true},
		{"a/**/b.go", "a/x/y/b.go", true},
		{"*.go", "cmd/calc/main.go", true},
		{"*_test.go", "calc/calc_test.go", true},
		{"*_test.go", "calc/calc.go", false},
		{"cmd/?/main.go", "cmd/calc/main.go", false},
		{"cmd/*/main.go", "cmd/calc/main.go", true},
		{"vendor", "vendor/x.go", false},
		{"vendor", "a/vendor/x.go", false},
	}
	for _, testCase := range cases {
		if got := MatchGlob(testCase.pattern, testCase.name); got != testCase.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.name, got, testCase.want)
		}
	}
}

func TestLegacySpecMirrorPathsAreNotCode(t *testing.T) {
	facts := Facts{
		Files: []SourceFile{
			{Path: "calc/calc.go"},
			{Path: ".specs/calc.yaml"},
			{Path: ".specs/context/calc.md"},
		},
		Symbols: []Symbol{{File: "calc/calc.go", Name: "Add", Kind: "function", Exported: true}},
	}
	partitions := PartitionFacts(facts, "calc")
	names := []string{}
	for _, partition := range partitions {
		names = append(names, partition.Name)
	}
	if len(partitions) != 1 || names[0] != "calc" {
		t.Fatalf("partitions = %v, want only calc", names)
	}
	observed := Observed(partitions[0])
	for _, file := range observed.Files {
		if IsLegacySpecMirrorPath(file) {
			t.Errorf("observed files carry the spec artifact %s", file)
		}
	}
	for _, artifact := range []string{".specs", ".specs/calc.yaml", ".specs/context/calc.md"} {
		if !IsLegacySpecMirrorPath(artifact) {
			t.Errorf("%s is not read as a spec artifact", artifact)
		}
	}
	if IsLegacySpecMirrorPath("specs.go") || IsLegacySpecMirrorPath("calc/calc.go") {
		t.Error("a source file is read as a spec artifact")
	}
}
