package searchindex

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/search"
)

// graphRows is the graph as the rebuild reads it: a select of one label
// answers the rows that label holds, the way a vertex select does.
type graphRows map[string][]map[string]any

func (g graphRows) Select(ctx context.Context, label string, properties []string, filter map[string]any) ([]map[string]any, error) {
	return g[label], nil
}

func openIndex(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	index, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { index.Close() })
	if Path(root) != filepath.Join(root, DatabaseName) {
		t.Fatalf("Path = %q", Path(root))
	}
	return index
}

func find(t *testing.T, index *Index, terms string) []search.Hit {
	t.Helper()
	hits, err := index.Search(context.Background(), terms, search.DefaultLimit)
	if err != nil {
		t.Fatalf("Search %q: %v", terms, err)
	}
	return hits
}

func TestARequirementTermFindsItsContext(t *testing.T) {
	index := openIndex(t)
	ctx := context.Background()
	documents := []search.Document{{
		ID:      1,
		Label:   "SpecRequirement:r.search-index",
		Context: "impl-searchindex",
		Text:    "SHOULD the search index answers a query over requirement text",
	}}
	if err := index.Rebuild(ctx, documents); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	hits := find(t, index, "requirement")
	if len(hits) == 0 {
		t.Fatalf("no hit for a term in a requirement text")
	}
	if hits[0].Context != "impl-searchindex" || hits[0].ID != 1 {
		t.Fatalf("hit = %+v", hits[0])
	}
}

func TestARebuildReplacesTheIndex(t *testing.T) {
	index := openIndex(t)
	ctx := context.Background()
	first := []search.Document{{ID: 1, Label: "SpecContext:one", Context: "one", Text: "alphabet"}}
	if err := index.Put(ctx, first); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(find(t, index, "alphabet")) != 1 {
		t.Fatalf("the put document is not indexed")
	}
	replacement := []search.Document{{ID: 2, Label: "SpecContext:two", Context: "two", Text: "bravado"}}
	if err := index.Rebuild(ctx, replacement); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if hits := find(t, index, "alphabet"); len(hits) != 0 {
		t.Fatalf("the rebuild appended: %+v", hits)
	}
	if len(find(t, index, "bravado")) != 1 {
		t.Fatalf("the rebuilt document is not indexed")
	}
}

func TestAnInterfaceSignatureTermFindsItsContext(t *testing.T) {
	index := openIndex(t)
	ctx := context.Background()
	documents := []search.Document{{
		ID:      3,
		Label:   "SpecInterface:Index",
		Context: "abc-search",
		Text:    "Index interface Rebuild(ctx context.Context, documents []Document) error",
	}}
	if err := index.Rebuild(ctx, documents); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	hits := find(t, index, "Rebuild")
	if len(hits) == 0 {
		t.Fatalf("no hit for a term in an interface signature")
	}
	if hits[0].Context != "abc-search" {
		t.Fatalf("hit = %+v", hits[0])
	}
}

func TestTheLimitIsHonoured(t *testing.T) {
	index := openIndex(t)
	ctx := context.Background()
	documents := []search.Document{}
	for id := int64(1); id <= 5; id++ {
		documents = append(documents, search.Document{ID: id, Label: "SpecContext:c", Context: "c", Text: "shared term"})
	}
	if err := index.Rebuild(ctx, documents); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	hits, err := index.Search(ctx, "shared", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
}

// A rebuild reads the graph and writes the index a query seeds from, so the
// documents a query finds name the contexts whose intent or requirements
// carry its terms, not just the symbols they mention.
func TestARebuildWritesTheIndexAQueryReads(t *testing.T) {
	index := openIndex(t)
	rows := graphRows{
		graph.LabelContext: []map[string]any{
			{"name": "calc", "intent": "Arithmetic on two integers."},
			{"name": "cmd-calc", "intent": "A command line front end."},
		},
		graph.LabelRequirement: []map[string]any{
			{"context": "calc", "reqId": "r.adds", "level": "MUST", "text": "The calculator adds two integers."},
		},
	}
	documents, err := Documents(context.Background(), rows, "")
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if err := index.Rebuild(context.Background(), documents); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	for _, query := range []string{"Arithmetic on two integers", "calculator adds"} {
		hits := find(t, index, query)
		if len(hits) == 0 {
			t.Fatalf("the rebuild wrote no document for %q", query)
		}
		if hits[0].Context != "calc" {
			t.Fatalf("the hit for %q names %q, want calc", query, hits[0].Context)
		}
	}
}
