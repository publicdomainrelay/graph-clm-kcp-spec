package ingest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
)

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSourceExtensionsAreTheSourceCodeExtensions(t *testing.T) {
	want := []string{
		".go", ".ts", ".tsx", ".js", ".mjs", ".jsx", ".rs", ".py",
		".rb", ".sh", ".c", ".cc", ".h", ".cpp",
	}
	if !reflect.DeepEqual(SourceExtensions, want) {
		t.Fatalf("SourceExtensions = %v, want %v", SourceExtensions, want)
	}
	for _, file := range []string{"impl/ingest/ingest.go", "web/app.tsx", "web/app.min.js", "tool/run.sh", "lib/x.mjs"} {
		if !IsSourceFile(file) {
			t.Errorf("%s holds source code", file)
		}
	}
	for _, file := range []string{"policies/allow.yaml", "deno.lock", "docs/adr.md", "policies/policy.rego", "deploy/.env", "assets/logo.png", "Makefile"} {
		if IsSourceFile(file) {
			t.Errorf("%s is a tree file, not source", file)
		}
	}
}

func TestATreeOfDataFilesMakesNoPartition(t *testing.T) {
	facts, data := SourceOnlyFacts(specsync.Facts{
		Commit: "abc123",
		Files: []specsync.SourceFile{
			{Path: "policies/allow.yaml", Language: "yaml"},
			{Path: "policies/deny.yaml", Language: "yaml"},
			{Path: "deploy/guest.yaml", Language: "yaml"},
		},
	})
	if len(facts.Files) != 0 {
		t.Fatalf("source files = %v, want none", facts.Files)
	}
	if !reflect.DeepEqual(data, []string{"policies/allow.yaml", "policies/deny.yaml", "deploy/guest.yaml"}) {
		t.Errorf("data files = %v", data)
	}
	partitions := SourceOnlyPartitions(specsync.PartitionFactsWith(facts, specsync.PartitionOptions{
		RepositoryName: "demo",
		TreeFiles:      data,
	}), false)
	if len(partitions) != 0 {
		t.Fatalf("partitions = %+v, want none for a data-only tree", partitions)
	}
}

func TestASourceFileMakesThePartitionAndItsDataFilesAreTreeFiles(t *testing.T) {
	facts, data := SourceOnlyFacts(specsync.Facts{
		Commit: "abc123",
		Files: []specsync.SourceFile{
			{Path: "policies/policy.go", Language: "go"},
			{Path: "policies/allow.yaml", Language: "yaml"},
		},
		Symbols: []specsync.Symbol{
			{ID: "function:Allow", Name: "Allow", Kind: "function", File: "policies/policy.go", Exported: true},
		},
	})
	partitions := SourceOnlyPartitions(specsync.PartitionFactsWith(facts, specsync.PartitionOptions{
		RepositoryName: "demo",
		TreeFiles:      data,
	}), false)
	if len(partitions) != 1 || partitions[0].Name != "policies" {
		t.Fatalf("partitions = %+v, want the policies context", partitions)
	}
	if !reflect.DeepEqual(partitions[0].Files, []string{"policies/policy.go"}) {
		t.Errorf("files = %v, want the Go source alone", partitions[0].Files)
	}
	if !reflect.DeepEqual(partitions[0].TreeFiles, []string{"policies/allow.yaml"}) {
		t.Errorf("tree files = %v, want the YAML next to the source", partitions[0].TreeFiles)
	}
	if len(partitions[0].Symbols) != 1 || partitions[0].Symbols[0].Name != "Allow" {
		t.Errorf("symbols = %+v, want the source function", partitions[0].Symbols)
	}
}

func TestPackageRootsFindsManifestsAndSkipsDependencies(t *testing.T) {
	tree := t.TempDir()
	write(t, filepath.Join(tree, "go.mod"), "module unseen\n")
	write(t, filepath.Join(tree, "greet", "deno.json"), "{}\n")
	write(t, filepath.Join(tree, "greet", "mod.ts"), "export const a = 1;\n")
	write(t, filepath.Join(tree, "node_modules", "dep", "package.json"), "{}\n")
	write(t, filepath.Join(tree, ".git", "config"), "[core]\n")

	roots, err := PackageRoots(tree)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(roots, ",") != ".,greet" {
		t.Errorf("roots = %v, want the tree root and greet", roots)
	}
}

func TestPackageRootsAlwaysIncludesTheTreeRoot(t *testing.T) {
	tree := t.TempDir()
	write(t, filepath.Join(tree, "main.go"), "package main\n")
	roots, err := PackageRoots(tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] != "." {
		t.Errorf("roots = %v, want only the tree root", roots)
	}
}

func TestDenoImportMapHoldsTheMapAndTheMemberNames(t *testing.T) {
	tree := t.TempDir()
	write(t, filepath.Join(tree, "deno.json"), `{
  "workspace": ["lib/a", "lib/b"],
  "imports": {
    "@scope/local": "./lib/a/mod.ts",
    "@scope/npm": "npm:@scope/npm@^1"
  }
}
`)
	write(t, filepath.Join(tree, "lib", "a", "deno.json"), `{"name": "@scope/a", "version": "0.0.0"}`+"\n")
	write(t, filepath.Join(tree, "lib", "b", "deno.json"), `{"version": "0.0.0"}`+"\n")

	imports := DenoImportMap(tree)
	if imports["@scope/local"] != "./lib/a/mod.ts" {
		t.Errorf("the map entry = %q", imports["@scope/local"])
	}
	if imports["@scope/a"] != "./lib/a" {
		t.Errorf("the member name = %q", imports["@scope/a"])
	}
	if _, found := imports["@scope/npm"]; !found {
		t.Errorf("an npm entry was dropped: %v", imports)
	}
	if _, found := imports["@scope/b"]; found {
		t.Errorf("a member without a name got one: %v", imports)
	}
}

func TestDetectPartitionPicksPackageForADenoWorkspace(t *testing.T) {
	workspace := t.TempDir()
	write(t, filepath.Join(workspace, "deno.json"), `{"workspace": ["lib/a"]}`+"\n")
	if got := DetectPartition(workspace); got != "package" {
		t.Errorf("partition = %q, want package", got)
	}
	single := t.TempDir()
	write(t, filepath.Join(single, "deno.json"), `{"name": "@scope/one"}`+"\n")
	if got := DetectPartition(single); got != "directory" {
		t.Errorf("partition = %q, want directory", got)
	}
	goOnly := t.TempDir()
	write(t, filepath.Join(goOnly, "go.mod"), "module x\n")
	if got := DetectPartition(goOnly); got != "directory" {
		t.Errorf("partition = %q, want directory", got)
	}
}

func TestGeneratedDirectoriesFoldsOnlyAllGeneratedTrees(t *testing.T) {
	tree := t.TempDir()
	write(t, filepath.Join(tree, "lib", "lex", "mod.ts"), "export const a = 1;\n")
	write(t, filepath.Join(tree, "lib", "lex", "com", "a.ts"), "/*\n * THIS FILE WAS GENERATED BY \"@atproto/lex\". DO NOT EDIT.\n */\nexport const b = 2;\n")
	write(t, filepath.Join(tree, "lib", "lex", "com", "b.ts"), "/*\n * THIS FILE WAS GENERATED BY \"@atproto/lex\". DO NOT EDIT.\n */\nexport const c = 3;\n")
	write(t, filepath.Join(tree, "lib", "mixed", "gen.ts"), "// DO NOT EDIT\nexport const d = 4;\n")
	write(t, filepath.Join(tree, "lib", "mixed", "hand.ts"), "export const e = 5;\n")

	files := []specsync.SourceFile{
		{Path: "lib/lex/mod.ts"},
		{Path: "lib/lex/com/a.ts"},
		{Path: "lib/lex/com/b.ts"},
		{Path: "lib/mixed/gen.ts"},
		{Path: "lib/mixed/hand.ts"},
	}
	generated := GeneratedDirectories(tree, files)
	if !generated["lib/lex/com"] {
		t.Errorf("an all-generated directory was not folded: %v", generated)
	}
	if generated["lib/lex"] || generated["lib/mixed"] {
		t.Errorf("a directory with hand-written code was folded: %v", generated)
	}
	roots := withoutGenerated([]string{".", "lib/lex", "lib/lex/com"}, generated)
	if strings.Join(roots, ",") != ".,lib/lex" {
		t.Errorf("roots = %v", roots)
	}
}
