package boltgraph

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
)

func requireBolt(t *testing.T) Options {
	t.Helper()
	if testing.Short() && os.Getenv("SPECD_REQUIRE_LIVE") != "1" {
		t.Skip("live test skipped in short mode")
	}
	backend := os.Getenv("SPECD_TEST_BACKEND")
	if backend == "" {
		backend = "arcadedb"
	}
	options := Options{}
	switch backend {
	case "hydradb":
		url := os.Getenv("SPECD_TEST_HYDRA_URL")
		if url == "" {
			url = "bolt://127.0.0.1:7687"
		}
		password := os.Getenv("SPECD_TEST_HYDRA_PASSWORD")
		if password == "" {
			if path := envOr("SPECD_TEST_HYDRA_PASSWORD_FILE", "/tmp/hdb/token"); path != "" {
				if contents, err := os.ReadFile(path); err == nil {
					password = strings.TrimSpace(string(contents))
				}
			}
		}
		options = Options{
			URL:      url,
			User:     envOr("SPECD_TEST_HYDRA_USER", "neo4j"),
			Password: password,
			Database: os.Getenv("SPECD_TEST_HYDRA_DATABASE"),
		}
		if os.Getenv("SPECD_REQUIRE_LIVE") == "1" && password == "" {
			t.Fatalf("SPECD_REQUIRE_LIVE=1 but no bolt password for %s", url)
		}
	case "arcadedb":
		options = Options{
			URL:      envOr("SPECD_TEST_ARCADE_URL", "bolt://127.0.0.1:7688"),
			User:     envOr("SPECD_TEST_ARCADE_USER", "root"),
			Password: envOr("SPECD_TEST_ARCADE_PASSWORD", "clm-arcadedb-root"),
			Database: envOr("SPECD_TEST_ARCADE_DATABASE", "clm"),
		}
	default:
		t.Fatalf("SPECD_TEST_BACKEND = %q, want arcadedb or hydradb", backend)
	}
	return options
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func connect(t *testing.T, ctx context.Context) *Client {
	t.Helper()
	options := requireBolt(t)
	client, err := Connect(ctx, options, 1)
	if err != nil {
		if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
			t.Fatalf("bolt is not reachable at %s: %v", options.URL, err)
		}
		t.Skipf("bolt is not reachable at %s: %v", options.URL, err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })
	return client
}

func TestBoltWriteReadDeleteRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := connect(t, ctx)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	repoID := ids.Stable("boltgraph-test:" + suffix + ":repo")
	parentID := ids.Stable("boltgraph-test:" + suffix + ":parent")
	childID := ids.Stable("boltgraph-test:" + suffix + ":child")
	repoName := "boltgraph-test-" + suffix
	parentName := "boltgraph-parent-" + suffix
	childName := "boltgraph-child-" + suffix
	owned := []int64{repoID, parentID, childID}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.DeleteVertices(cleanupCtx, owned)
	})

	repo := graph.VertexSet{Label: graph.LabelRepo, Rows: []graph.Vertex{
		{ID: repoID, Props: map[string]any{"name": repoName, "path": "/tmp/" + repoName}},
	}}
	contexts := graph.VertexSet{Label: graph.LabelContext, Rows: []graph.Vertex{
		{ID: parentID, Props: map[string]any{"name": parentName, "repo": repoName, "intent": "the parent", "specHash": "aaa"}},
		{ID: childID, Props: map[string]any{"name": childName, "repo": repoName, "intent": "the child", "specHash": "bbb"}},
	}}
	for _, set := range []graph.VertexSet{repo, contexts} {
		if err := client.WriteVertices(ctx, set); err != nil {
			t.Fatalf("write %s: %v", set.Label, err)
		}
	}

	if err := client.WriteVertices(ctx, contexts); err != nil {
		t.Fatalf("second write: %v", err)
	}
	found, err := client.Select(ctx, graph.LabelContext, []string{"name", "repo"}, map[string]any{"id": childID})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0]["name"] != childName {
		t.Fatalf("context vertex = %+v", found)
	}
	all, err := client.SelectIDs(ctx, graph.LabelContext)
	if err != nil {
		t.Fatal(err)
	}
	if countOf(all, parentID) != 1 || countOf(all, childID) != 1 {
		t.Fatalf("the upsert duplicated a vertex: parent=%d child=%d", countOf(all, parentID), countOf(all, childID))
	}

	hasContext := graph.EdgeSet{
		Type: graph.EdgeHasContext, FromLabel: graph.LabelRepo, ToLabel: graph.LabelContext,
		Rows: []graph.Edge{{From: repoID, To: parentID}, {From: repoID, To: childID}},
	}
	upstream := graph.EdgeSet{
		Type: graph.EdgeUpstream, FromLabel: graph.LabelContext, ToLabel: graph.LabelContext,
		Rows: []graph.Edge{{From: childID, To: parentID}},
	}
	for _, set := range []graph.EdgeSet{hasContext, upstream} {
		if err := client.WriteEdges(ctx, set); err != nil {
			t.Fatalf("write %s edges: %v", set.Type, err)
		}
	}

	out, err := client.SelectOut(ctx, graph.EdgeUpstream, graph.LabelContext, graph.LabelContext, childID, []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0]["name"] != parentName {
		t.Fatalf("upstream out = %+v, want %s", out, parentName)
	}
	in, err := client.SelectIn(ctx, graph.EdgeUpstream, graph.LabelContext, graph.LabelContext, parentID, []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 1 || in[0]["name"] != childName {
		t.Fatalf("upstream in = %+v, want %s", in, childName)
	}
	children, err := client.SelectOut(ctx, graph.EdgeHasContext, graph.LabelRepo, graph.LabelContext, repoID, []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("HAS_CONTEXT out = %+v, want 2", children)
	}

	rows, err := client.Run(ctx, "MATCH (n:"+graph.LabelContext+" {id: $id}) RETURN n.name AS name", map[string]any{"id": childID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["name"] != childName {
		t.Fatalf("raw run = %+v", rows)
	}

	if err := client.DeleteVertices(ctx, owned); err != nil {
		t.Fatal(err)
	}
	gone, err := client.Select(ctx, graph.LabelContext, []string{"name"}, map[string]any{"id": childID})
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 0 {
		t.Fatalf("the deleted vertex is still there: %+v", gone)
	}
}

func countOf(values []int64, want int64) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
