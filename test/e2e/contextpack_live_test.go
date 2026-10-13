package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/searchindex"
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

const queryPackRepository = "query-pack-calc"

// A pack for a query seeds from the search index the graph rebuild wrote, and
// that index carries the intent of every context, so the terms of a context's
// own intent name that context. This is the live half of
// r.history-eval-reads-a-pack: the git-history benchmark reads the same pack
// for a commit subject, and a pack that names no context scores k 0.
func TestAQueryPackNamesTheContextItsIntentNames(t *testing.T) {
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

	repoPath := fixture.CopyAs(t, "calc", queryPackRepository)
	names := []string{"calc", "cmd-calc", queryPackRepository}
	repositories := []string{queryPackRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	indexRoot := t.TempDir()
	index, err := searchindex.Open(indexRoot)
	if err != nil {
		t.Fatalf("open the search index: %v", err)
	}
	defer index.Close()

	writer := connectBackend(t, ctx, backends(t)[0])
	if _, err := ingest.Run(ctx, client, ingest.Options{
		RepoPath:       repoPath,
		RepositoryName: queryPackRepository,
		Writer:         writer,
		GraphNamespace: graphNamespaceFor(t),
		Search:         index,
		SearchReader:   writer,
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// The ingest writes no intent, because the intent is the model's: set one
	// the way a summary would, then rebuild the graph, which is the pass that
	// writes the search index a query seeds from.
	const intent = "Arithmetic on two integers."
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatalf("read the calc context: %v", err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatalf("typed: %v", err)
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	edited := *systemContext
	edited.Spec.Intent = intent
	editedObject, err := kcpclient.Unstructured(&edited)
	if err != nil {
		t.Fatalf("unstructured: %v", err)
	}
	if _, err := client.Apply(ctx, editedObject); err != nil {
		t.Fatalf("apply the intent: %v", err)
	}

	specctl := buildSpecctl(t, root)
	runSpecctl(t, ctx, specctl, root, nil, "graph", "rebuild", "--search-root", indexRoot)

	pack := runSpecctl(t, ctx, specctl, root, nil, "context", "pack", "--query", intent, "--search-root", indexRoot)
	neighbors, found := sectionOf(pack, "ranked neighbor cards")
	if !found {
		t.Fatalf("the pack seeded by the intent has no neighbour section:\n%s", pack)
	}
	if !strings.Contains(neighbors, "calc") {
		t.Errorf("the pack seeded by the intent of calc names no context:\n%s", neighbors)
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
