package searchindex

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/search"
)

// graphRows is the graph as the rebuild reads it: a select of one label
// answers the rows that label holds, filtered on the properties the caller
// names, the way a vertex select does.
type graphRows map[string][]map[string]any

func (g graphRows) Select(ctx context.Context, label string, properties []string, filter map[string]any) ([]map[string]any, error) {
	rows := g[label]
	if len(filter) == 0 {
		return rows, nil
	}
	kept := []map[string]any{}
	for _, row := range rows {
		matches := true
		for key, want := range filter {
			if fmt.Sprint(row[key]) != fmt.Sprint(want) {
				matches = false
				break
			}
		}
		if matches {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

// findingCluster answers the finding select with the objects it was given.
type findingCluster struct {
	items []unstructured.Unstructured
}

func (c findingCluster) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{Items: c.items}, nil
}

func labelCounts(t *testing.T, index *Index) map[string]int {
	t.Helper()
	rows, err := index.db.Query(`SELECT label FROM documents`)
	if err != nil {
		t.Fatalf("read the documents table: %v", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		label := ""
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scan a document: %v", err)
		}
		name, _, _ := strings.Cut(label, ":")
		counts[name]++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the documents table: %v", err)
	}
	return counts
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
	documents, err := Documents(context.Background(), rows, "", "")
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

// The rebuild fills the index with one document per thing it claims to index:
// a context with its intent, a requirement with its text, an interface with its
// signature, a finding with its summary, a memory note with its title and body,
// and a code symbol the graph holds. It reads the instance's own graph
// namespace, so a context another instance wrote is not indexed, and one thing
// is one document, not two sharing an id.
func TestARebuildWritesOneDocumentPerIndexedThing(t *testing.T) {
	index := openIndex(t)
	ctx := context.Background()
	rows := graphRows{
		graph.LabelContext: {
			{"id": int64(1), "name": "calc", "intent": "Arithmetic on two integers.", "scope": "this-instance"},
			{"id": int64(2), "name": "cmd-calc", "intent": "A command line front end.", "scope": "this-instance"},
			{"id": int64(3), "name": "elsewhere", "intent": "A context another instance wrote.", "scope": "other-instance"},
		},
		graph.LabelRequirement: {
			{"id": int64(4), "context": "calc", "reqId": "r.adds", "level": "MUST", "text": "The calculator adds two integers.", "scope": "this-instance"},
		},
		graph.LabelInterface: {
			{"id": int64(5), "context": "calc", "name": "Add", "kind": "function", "signature": "func Add(a, b int) int", "scope": "this-instance"},
		},
		graph.LabelCodeRef: {
			{"id": int64(6), "codegraphId": "function:adds", "name": "Add", "kind": "function", "display": "calc.Add", "filePath": "calc/calc.go", "scope": "this-instance"},
		},
		graph.LabelPiMemory: {
			{"id": int64(7), "title": "the port is kernel assigned", "body": "a listener binds port 0 and reports its address"},
		},
	}
	cluster := findingCluster{items: []unstructured.Unstructured{{
		Object: map[string]any{
			"metadata": map[string]any{"name": "f.pack"},
			"spec":     map[string]any{"summary": "the pack named nothing", "context": "impl-eval"},
		},
	}}}
	documents, err := Documents(ctx, rows, "", "this-instance")
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	findings, err := Findings(ctx, cluster, "default")
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	documents = append(documents, findings...)
	if err := index.Rebuild(ctx, documents); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	want := map[string]int{
		graph.LabelContext:     2,
		graph.LabelRequirement: 1,
		graph.LabelInterface:   1,
		graph.LabelCodeRef:     1,
		graph.LabelPiMemory:    1,
		"SpecFinding":          1,
	}
	got := labelCounts(t, index)
	if len(got) != len(want) {
		t.Fatalf("the rebuild wrote %v, want %v", got, want)
	}
	for label, count := range want {
		if got[label] != count {
			t.Fatalf("the rebuild wrote %d %s document(s), want %d", got[label], label, count)
		}
	}
	if hits := find(t, index, "another instance wrote"); len(hits) != 0 {
		t.Fatalf("the rebuild indexed another instance's context: %+v", hits)
	}
	seen := map[int64]bool{}
	for _, document := range documents {
		seen[document.ID] = true
	}
	if len(seen) != len(documents) {
		t.Fatalf("two documents share an id: %d ids for %d documents", len(seen), len(documents))
	}
}
