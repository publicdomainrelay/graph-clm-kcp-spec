package codegraphfacts

import (
	"context"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/orgfixture"
	"slices"
	"strings"
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

func TestBuildOfAnOrgRootHoldsTheRootsOwnFilesOnly(t *testing.T) {
	orgfixture.FakeCodegraph(t)
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("facts", 0)
	// Both ways a checkout is indexed: a copy (the reader's tree is never
	// written) and in place.
	for name, inPlace := range map[string]bool{"copy": false, "in place": true} {
		graph, err := Build(context.Background(), clone, Options{Repository: "socialweb-computer", IndexInPlace: inPlace})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		paths := []string{}
		for _, file := range graph.Spec.Files {
			paths = append(paths, file.Path)
		}
		if !slices.Contains(paths, "scripts/find-all-package.ts") {
			t.Fatalf("%s: the root's own script is missing from %v", name, paths)
		}
		for _, path := range paths {
			for _, member := range []string{"market/", "hono-compute-provider/", "relay/"} {
				if strings.HasPrefix(path, member) {
					t.Fatalf("%s: %s is a member's file in the root's graph", name, path)
				}
			}
		}
		for _, node := range graph.Spec.Nodes {
			if strings.HasPrefix(node.File, "market/") {
				t.Fatalf("%s: node %s is a member's", name, node.File)
			}
		}
	}
}
