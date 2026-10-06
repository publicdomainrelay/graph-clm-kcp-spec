package codegraphfacts

import (
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
)

func TestWithoutSubmodulesKeepsOnlyTheRootsOwnGraph(t *testing.T) {
	skip := map[string]bool{"market": true, "vendor/relay": true}
	files := []codegraphsqlite.File{{Path: "scripts/a.ts"}, {Path: "market/lib/b.ts"}, {Path: "vendor/relay/c.ts"}, {Path: "marketing/d.ts"}}
	nodes := []codegraphsqlite.Node{
		{ID: "own", FilePath: "scripts/a.ts"},
		{ID: "member", FilePath: "market/lib/b.ts"},
		{ID: "nested", FilePath: "vendor/relay/c.ts"},
		{ID: "lookalike", FilePath: "marketing/d.ts"},
	}
	edges := []codegraphsqlite.Edge{
		{Source: "own", Target: "lookalike"},
		{Source: "own", Target: "member"},
		{Source: "member", Target: "nested"},
		{Source: "own", Target: "external-unresolved"},
	}
	gotFiles, gotNodes, gotEdges := withoutSubmodules(skip, files, nodes, edges)
	if len(gotFiles) != 2 || gotFiles[0].Path != "scripts/a.ts" || gotFiles[1].Path != "marketing/d.ts" {
		t.Fatalf("files = %+v: a directory that only shares a prefix is not a submodule", gotFiles)
	}
	if len(gotNodes) != 2 {
		t.Fatalf("nodes = %+v", gotNodes)
	}
	if len(gotEdges) != 2 || gotEdges[0].Target != "lookalike" || gotEdges[1].Target != "external-unresolved" {
		t.Fatalf("edges = %+v: an edge into a dropped node must go", gotEdges)
	}
}
