package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const contextPackRepository = "pack-calc"

// The pack the command prints comes from the live graph, not from a fake
// reader: the neighbour section names the context the seed depends on, and
// the owner card names the context the file belongs to. The calc fixture
// holds two contexts joined by a DEPENDS_ON edge (cmd-calc imports calc).
func TestContextPackReadsTheLiveGraph(t *testing.T) {
	t.Parallel()
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", contextPackRepository)
	names := []string{"calc", "cmd-calc", contextPackRepository}
	repositories := []string{contextPackRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	writer := connectBackend(t, ctx, backends(t)[0])
	result, err := ingest.Run(ctx, client, ingest.Options{
		RepoPath:       repoPath,
		RepositoryName: contextPackRepository,
		Writer:         writer,
		GraphNamespace: graphNamespaceFor(t),
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(result.Contexts) != 2 {
		t.Fatalf("contexts = %+v", result.Contexts)
	}

	specctl := buildSpecctl(t, root)

	pack := runSpecctl(t, ctx, specctl, root, nil, "context", "pack", "--context", "cmd-calc")
	neighbors, found := sectionOf(pack, "ranked neighbor cards")
	if !found {
		t.Fatalf("the pack has no neighbour section:\n%s", pack)
	}
	if !strings.Contains(neighbors, "calc") {
		t.Errorf("the neighbour section names no neighbour:\n%s", neighbors)
	}
	if !strings.Contains(neighbors, "DEPENDS_ON") {
		t.Errorf("the neighbour section names no edge:\n%s", neighbors)
	}

	byFile := runSpecctl(t, ctx, specctl, root, nil, "context", "pack", "--file", "calc/calc.go")
	owner, found := sectionOf(byFile, "owner context card")
	if !found {
		t.Fatalf("the pack seeded by file has no owner card:\n%s", byFile)
	}
	if !strings.Contains(owner, "calc") {
		t.Errorf("the owner card names another context:\n%s", owner)
	}
}

// sectionOf returns the body of one "## title" section of a rendered pack.
func sectionOf(pack, title string) (string, bool) {
	heading := "## " + title + "\n"
	start := strings.Index(pack, heading)
	if start == -1 {
		return "", false
	}
	body := pack[start+len(heading):]
	if end := strings.Index(body, "\n## "); end != -1 {
		body = body[:end]
	}
	return strings.TrimSpace(body), true
}
