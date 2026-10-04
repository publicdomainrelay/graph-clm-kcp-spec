package specsync

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/archyaml"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func parseArch(t *testing.T, body string) *archyaml.Document {
	t.Helper()
	document, err := archyaml.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestMatchArchMatchesByCodePath(t *testing.T) {
	document := parseArch(t, `
metadata: {name: calc, root: sc.calc}
system_contexts:
  - id: sc.calc
    code: {root: go.mod}
  - id: sc.runner
    source: cmd/calc
    upstream: up.go
  - id: sc.store
    code: {store: store/memory.go, tests: store/memory_test.go}
`)
	partitions := []Partition{
		{Name: "calc", Directory: ".", TreeFiles: []string{"go.mod"}},
		{Name: "cmd-calc", Directory: "cmd/calc", Files: []string{"cmd/calc/main.go"}},
		{Name: "store", Directory: "store", Files: []string{"store/memory.go", "store/memory_test.go"}},
	}
	matched := MatchArch(partitions, document)
	want := map[string]string{"cmd-calc": "sc.runner", "store": "sc.store"}
	if !reflect.DeepEqual(matched, want) {
		t.Fatalf("matched = %v, want %v", matched, want)
	}
}

func TestMatchArchPrefersTheNodeThatNamesMoreOfThePartition(t *testing.T) {
	document := parseArch(t, `
system_contexts:
  - id: sc.broad
    code: {one: store/memory.go}
  - id: sc.deep
    code: {one: store/memory.go, two: store/store.go}
`)
	partitions := []Partition{{Name: "store", Directory: "store", Files: []string{"store/memory.go", "store/store.go"}}}
	if got := MatchArch(partitions, document)["store"]; got != "sc.deep" {
		t.Fatalf("matched %q, want sc.deep: the node that names more files wins", got)
	}
}

func TestRootArchNodeIDReadsMetadataRootThenTheName(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "open-architecture", "arch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := archyaml.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := RootArchNodeID(document, "deno-kcp"); got != "sc.deno-kcp" {
		t.Fatalf("root = %q, want sc.deno-kcp", got)
	}
	named := parseArch(t, `
metadata: {name: calc}
system_contexts:
  - id: sc.calc
    code: {root: go.mod}
`)
	if got := RootArchNodeID(named, "calc"); got != "sc.calc" {
		t.Fatalf("root = %q, want sc.calc", got)
	}
	if got := RootArchNodeID(named, "other"); got != "" {
		t.Fatalf("root = %q, want none", got)
	}
}

func TestMatchArchOnTheRealDocument(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "open-architecture", "arch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := archyaml.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	partitions := []Partition{
		{Name: "deno-kcp", Directory: ".", TreeFiles: []string{"Makefile", "go.mod", "README.md", "docs/adrs/0001-cancel-policy-workflow-runs.md"}},
		{Name: "cmd-deno-kcp-provider", Directory: "cmd/deno-kcp-provider", Files: []string{"cmd/deno-kcp-provider/main.go"}},
		{Name: "internal-denopod", Directory: "internal/denopod", Files: []string{"internal/denopod/denopod.go"}},
		{Name: "test-integration", Directory: "test/integration", Files: []string{"test/integration/examples.go"}},
	}
	matched := MatchArch(partitions, document)
	if matched["cmd-deno-kcp-provider"] != "ov.provider-flags" {
		t.Errorf("cmd-deno-kcp-provider matched %q, want the flags overlay", matched["cmd-deno-kcp-provider"])
	}
	if matched["test-integration"] == "" {
		t.Errorf("test-integration matched nothing: %v", matched)
	}
	if matched["internal-denopod"] == "" {
		t.Errorf("internal-denopod matched nothing: %v", matched)
	}
}

func generatedFixtureFacts() Facts {
	return Facts{
		Files: []SourceFile{
			{Path: "api/v1alpha1/denospec.go", Language: "go"},
			{Path: "api/v1alpha1/zz_generated.deepcopy.go", Language: "go"},
		},
		Symbols: []Symbol{
			{ID: "type:1", Name: "DenoSpec", Kind: "struct", File: "api/v1alpha1/denospec.go", Line: 10, Exported: true},
			{ID: "method:1", Name: "DeepCopy", Qualified: "DenoSpec.DeepCopy", Kind: "method", File: "api/v1alpha1/zz_generated.deepcopy.go", Line: 20, Exported: true},
			{ID: "method:2", Name: "DeepCopyInto", Qualified: "DenoSpec.DeepCopyInto", Kind: "method", File: "api/v1alpha1/zz_generated.deepcopy.go", Line: 30, Exported: true},
			{ID: "method:3", Name: "DeepCopyObject", Qualified: "DenoSpec.DeepCopyObject", Kind: "method", File: "api/v1alpha1/denospec.go", Line: 40, Exported: true},
		},
	}
}

func TestGeneratedSymbolsAreNotObserved(t *testing.T) {
	observed := Observed(PartitionFacts(generatedFixtureFacts(), "repo")[0])
	names := []string{}
	for _, entry := range observed.Interfaces {
		names = append(names, entry.Name)
	}
	if !reflect.DeepEqual(names, []string{"DenoSpec"}) {
		t.Fatalf("interfaces = %v, want only the declared type", names)
	}
	if !IsGeneratedSymbol("api/v1alpha1/zz_generated.deepcopy.go", "Anything") {
		t.Error("a zz_generated file must be generated")
	}
	if IsGeneratedSymbol("api/v1alpha1/denospec.go", "DenoSpec") {
		t.Error("a hand-written type must not be generated")
	}
}

func TestTreeFilesGatherUnderTheirPartitionAndTheRoot(t *testing.T) {
	facts := Facts{Files: []SourceFile{{Path: "cmd/calc/main.go", Language: "go"}}}
	treeFiles := []string{
		"Makefile",
		"README.md",
		"docs/adrs/0001.md",
		"cmd/calc/apply.sh",
		"third_party/deno.ts",
	}
	partitions := PartitionFactsWith(facts, PartitionOptions{RepositoryName: "calc", TreeFiles: treeFiles})
	byName := map[string]Partition{}
	for _, partition := range partitions {
		byName[partition.Name] = partition
	}
	if len(partitions) != 1 {
		t.Fatalf("partitions = %+v, want the command and nothing else", partitions)
	}
	if got := byName["cmd-calc"].TreeFiles; !reflect.DeepEqual(got, []string{"cmd/calc/apply.sh"}) {
		t.Errorf("cmd-calc tree files = %v", got)
	}

	withRoot := PartitionFactsWith(facts, PartitionOptions{RepositoryName: "calc", TreeFiles: treeFiles, RootContext: true})
	byName = map[string]Partition{}
	for _, partition := range withRoot {
		byName[partition.Name] = partition
	}
	root, ok := byName["calc"]
	if !ok {
		t.Fatalf("no root partition: %+v", withRoot)
	}
	if !reflect.DeepEqual(root.TreeFiles, []string{"Makefile", "README.md", "docs/adrs/0001.md"}) {
		t.Errorf("root tree files = %v", root.TreeFiles)
	}
	if len(root.Files) != 0 {
		t.Errorf("root files = %v, want none: the index has no root source file", root.Files)
	}
}

func TestUnresolvedRefsResolveAgainstTheTree(t *testing.T) {
	observed := spec.ObservedFacts{
		Files:     []string{"deploy/examples/market/40-pds.yaml"},
		TreeFiles: []string{"deploy/examples/market/apply.sh", "deploy/examples/market/README.md"},
	}
	requirements := []spec.Requirement{
		{ID: "r.apply", Level: spec.LevelMust, Text: "t", CodeRefs: []string{"file:deploy/examples/market/apply.sh"}},
		{ID: "r.missing", Level: spec.LevelMust, Text: "t", CodeRefs: []string{"file:deploy/examples/market/gone.sh"}},
	}
	unresolved := UnresolvedCodeRefs(requirements, observed)
	if !reflect.DeepEqual(unresolved, []string{"r.missing: file:deploy/examples/market/gone.sh"}) {
		t.Fatalf("unresolved = %v", unresolved)
	}
}

func TestPartitionDependenciesFollowImportsAndTheRoot(t *testing.T) {
	partitions := []Partition{
		{Name: "calc", Directory: ".", TreeFiles: []string{"go.mod"}},
		{Name: "calc-calc", Directory: "calc", Files: []string{"calc/calc.go"}},
		{Name: "cmd-calc", Directory: "cmd/calc", Files: []string{"cmd/calc/main.go"}},
		{Name: "store", Directory: "store", Files: []string{"store/store.go"}},
	}
	imports := []Import{
		{From: "cmd/calc/main.go", Path: "example.com/calc/calc"},
		{From: "cmd/calc/main.go", Path: "fmt"},
		{From: "calc/calc.go", Path: "example.com/calc/calc"},
		{From: "store/store.go", Path: "k8s.io/apimachinery/pkg/api/meta"},
	}
	dependencies := PartitionDependencies(partitions, imports, "example.com/calc")
	want := map[string][]string{
		"cmd-calc":  {"calc", "calc-calc"},
		"calc-calc": {"calc"},
		"store":     {"calc"},
	}
	if !reflect.DeepEqual(dependencies, want) {
		t.Fatalf("dependencies = %v, want %v", dependencies, want)
	}
	if refs := DependencyRefs(dependencies["cmd-calc"]); !reflect.DeepEqual(refs, []string{"sc.calc", "sc.calc-calc"}) {
		t.Fatalf("refs = %v", refs)
	}
}
