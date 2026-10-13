package ingest

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// applicationTree writes a single module Go tree: one command, one library
// package and a TypeScript entry named by deploy/ts-entries.yaml.
func applicationTree(t *testing.T) string {
	t.Helper()
	repoPath := t.TempDir()
	write(t, filepath.Join(repoPath, "go.mod"), "module example.com/demo\n")
	write(t, filepath.Join(repoPath, "cmd", "tool", "main.go"),
		"package main\n\nimport \"example.com/demo/store\"\n\nfunc main() { store.Save() }\n")
	write(t, filepath.Join(repoPath, "cmd", "tool", "main_test.go"),
		"package main\n\nimport \"example.com/demo/store\"\n\nfunc TestMain(t *testing.T) { store.Save() }\n")
	write(t, filepath.Join(repoPath, "store", "store.go"),
		"package store\n\nimport \"example.com/demo/cmd/tool\"\n\nfunc Save() { _ = tool.Name }\n")
	write(t, filepath.Join(repoPath, "store", "store_test.go"),
		"package store\n\nimport \"example.com/demo/extra\"\n\nfunc TestSave(t *testing.T) { extra.Helper() }\n")
	write(t, filepath.Join(repoPath, "extra", "extra.go"), "package extra\n\nfunc Helper() {}\n")
	write(t, filepath.Join(repoPath, "web", "mod.ts"), "export const web = 1;\n")
	write(t, filepath.Join(repoPath, "deploy", "ts-entries.yaml"), "entries:\n  - name: spa\n    path: web/mod.ts\n")
	return repoPath
}

func applicationFacts() specsync.Facts {
	return specsync.Facts{
		Commit: "abc123",
		Files: []specsync.SourceFile{
			{Path: "cmd/tool/main.go", Language: "go"},
			{Path: "cmd/tool/main_test.go", Language: "go"},
			{Path: "store/store.go", Language: "go"},
			{Path: "store/store_test.go", Language: "go"},
			{Path: "extra/extra.go", Language: "go"},
			{Path: "web/mod.ts", Language: "typescript"},
		},
		Symbols: []specsync.Symbol{
			{ID: "function:main", Name: "main", Kind: "function", File: "cmd/tool/main.go", Line: 5},
			{ID: "function:Save", Name: "Save", Kind: "function", File: "store/store.go", Line: 5, Exported: true},
			{ID: "function:Helper", Name: "Helper", Kind: "function", File: "extra/extra.go", Line: 3, Exported: true},
			{ID: "function:web", Name: "web", Kind: "function", File: "web/mod.ts", Line: 1, Exported: true},
		},
		Imports: []specsync.Import{
			{From: "cmd/tool/main.go", Path: "example.com/demo/store"},
			{From: "cmd/tool/main_test.go", Path: "example.com/demo/store"},
			{From: "store/store.go", Path: "example.com/demo/cmd/tool"},
			{From: "store/store_test.go", Path: "example.com/demo/extra"},
			{From: "web/mod.ts", Path: "jsr:@std/assert"},
		},
	}
}

// applicationPartitions runs the partitioner over the application tree the way
// an ingest does: the module roots the manifests name, the application split,
// the dependency facts and the entry point edges.
func applicationPartitions(t *testing.T, repoPath string, facts specsync.Facts) ([]specsync.Partition, map[string]string) {
	t.Helper()
	partitions := specsync.PartitionFactsWith(facts, specsync.PartitionOptions{
		Mode:           spec.PartitionPackage,
		Roots:          []string{".", "extra", "store"},
		RepositoryName: "demo",
	})
	partitions, entries := ApplicationPartitions(repoPath, facts.Files, facts.Symbols, partitions, "demo")
	specsync.AttachDependencies(partitions, facts.Imports, specsync.ModuleResolver{ModulePath: "example.com/demo"})
	partitions, orchestrators := applicationEdges(partitions, entries)
	return partitions, orchestrators
}

// TestACommandPartitionIsAnOrchestrator pins: one partition per Go
// command and per TypeScript entry, each dependsOn the partitions its
// production imports resolve to, and the partitions a command imports carry it
// as their orchestrator.
func TestACommandPartitionIsAnOrchestrator(t *testing.T) {
	repoPath := applicationTree(t)
	facts := applicationFacts()
	partitions, orchestrators := applicationPartitions(t, repoPath, facts)

	byName := map[string]specsync.Partition{}
	for _, partition := range partitions {
		byName[partition.Name] = partition
	}
	command, ok := byName["cmd-tool"]
	if !ok {
		t.Fatalf("partitions = %+v, want one named after the command", partitions)
	}
	if command.Directory != "cmd/tool" || !reflect.DeepEqual(command.Files, []string{"cmd/tool/main.go", "cmd/tool/main_test.go"}) {
		t.Errorf("command partition = %+v", command)
	}
	if !reflect.DeepEqual(command.DependsOn, []string{"store"}) {
		t.Errorf("command dependsOn = %v, want the partition its production import resolves to", command.DependsOn)
	}
	if got := byName["store"].Files; !reflect.DeepEqual(got, []string{"store/store.go", "store/store_test.go"}) {
		t.Errorf("store files = %v, want the module's own files", got)
	}
	entry, ok := byName["spa"]
	if !ok || entry.Directory != "web" || !reflect.DeepEqual(entry.Files, []string{"web/mod.ts"}) {
		t.Fatalf("TypeScript entry partition = %+v, want one named after the entry", entry)
	}
	if !reflect.DeepEqual(orchestrators, map[string]string{"store": "sc.cmd-tool"}) {
		t.Errorf("orchestrators = %v, want every partition a command imports placed under it", orchestrators)
	}

	cluster := newFakeCluster()
	for _, partition := range partitions {
		if _, err := ingestPartition(context.Background(), cluster, "demo", specapi.DefaultNamespace, "abc123", partition, orchestrators, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "store")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(stored)
	if err != nil {
		t.Fatal(err)
	}
	context, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("stored a %T", typed)
	}
	if context.Spec.Orchestrator != "sc.cmd-tool" {
		t.Errorf("orchestrator = %q, want sc.cmd-tool written into the spec", context.Spec.Orchestrator)
	}
	if !reflect.DeepEqual(context.Spec.TestDependsOn, []string{"sc.extra"}) {
		t.Errorf("testDependsOn = %v, want the test-only import", context.Spec.TestDependsOn)
	}
}

// TestACommandIsNeverTheDependencyOfAPackage pins the other half of the rule: a
// package import that resolves into a command stays out of spec.dependsOn.
func TestACommandIsNeverTheDependencyOfAPackage(t *testing.T) {
	repoPath := applicationTree(t)
	partitions, _ := applicationPartitions(t, repoPath, applicationFacts())
	byName := map[string]specsync.Partition{}
	for _, partition := range partitions {
		byName[partition.Name] = partition
	}
	if got := byName["store"].DependsOn; len(got) != 0 {
		t.Errorf("store dependsOn = %v, want none: its import names the command", got)
	}
	for _, partition := range partitions {
		for _, target := range partition.DependsOn {
			if target == "cmd-tool" {
				t.Errorf("%s dependsOn the command: %v", partition.Name, partition.DependsOn)
			}
		}
	}
}

func TestApplicationPartitionsAddNothingToATreeWithoutEntryPoints(t *testing.T) {
	repoPath := t.TempDir()
	write(t, filepath.Join(repoPath, "lib", "lib.go"), "package lib\n")
	facts := specsync.Facts{Files: []specsync.SourceFile{{Path: "lib/lib.go", Language: "go"}}}
	partitions := specsync.PartitionFactsWith(facts, specsync.PartitionOptions{RepositoryName: "demo"})
	got, entries := ApplicationPartitions(repoPath, facts.Files, facts.Symbols, partitions, "demo")
	if len(entries) != 0 {
		t.Errorf("entry points = %v, want none", entries)
	}
	if !reflect.DeepEqual(got, partitions) {
		t.Errorf("partitions = %+v, want the ones the tree made", got)
	}
}
