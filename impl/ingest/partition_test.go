package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
