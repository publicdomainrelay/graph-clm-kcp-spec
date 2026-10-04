package graph

import (
	"context"
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func snapshot() Snapshot {
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       spec.RepositorySpec{Path: "/repos/calc"},
	}
	library := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   spec.RefSelf,
			Intent:     "the library",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "add", CodeRefs: []string{"function:Add"}},
			},
			Interfaces: []spec.Interface{{Name: "Add", Kind: "function", Signature: "(a, b int) int"}},
			CodeRefs:   []string{"file:calc/calc.go", "function:Add", "function:gone"},
		},
	}
	command := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc"},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   "sc.calc",
			Overlay:    []string{"sc.calc"},
		},
	}
	return Snapshot{
		Repository: *repository,
		Contexts:   []spec.SystemContext{library, command},
		CodeRefs: map[string]CodeRef{
			"file:calc/calc.go": {CodegraphID: "file:calc/calc.go", Kind: "file", Name: "calc.go", FilePath: "calc/calc.go"},
			"function:Add":      {CodegraphID: "function:abc", Kind: "function", Name: "Add", FilePath: "calc/calc.go"},
		},
	}
}

func findSet(sets []VertexSet, label string) VertexSet {
	for _, set := range sets {
		if set.Label == label {
			return set
		}
	}
	return VertexSet{}
}

func findEdgeSet(sets []EdgeSet, edgeType, from string) EdgeSet {
	for _, set := range sets {
		if set.Type == edgeType && set.FromLabel == from {
			return set
		}
	}
	return EdgeSet{}
}

func TestBuildVertices(t *testing.T) {
	vertices, edges := Build(snapshot())
	if got := len(findSet(vertices, LabelRepo).Rows); got != 1 {
		t.Errorf("repos = %d, want 1", got)
	}
	if got := len(findSet(vertices, LabelContext).Rows); got != 2 {
		t.Errorf("contexts = %d, want 2", got)
	}
	if got := len(findSet(vertices, LabelRequirement).Rows); got != 1 {
		t.Errorf("requirements = %d, want 1", got)
	}
	if got := len(findSet(vertices, LabelInterface).Rows); got != 1 {
		t.Errorf("interfaces = %d, want 1", got)
	}
	coderefs := findSet(vertices, LabelCodeRef).Rows
	if len(coderefs) != 2 {
		t.Fatalf("code refs = %+v, want the two that resolve", coderefs)
	}
	for _, vertex := range coderefs {
		if vertex.ID != CodeRefID(vertex.Props["codegraphId"].(string)) {
			t.Errorf("code ref id = %d, want the stable id of %v", vertex.ID, vertex.Props["codegraphId"])
		}
	}

	if got := len(findEdgeSet(edges, EdgeHasContext, LabelRepo).Rows); got != 2 {
		t.Errorf("has context edges = %d, want 2", got)
	}
	if got := len(findEdgeSet(edges, EdgeUpstream, LabelContext).Rows); got != 1 {
		t.Errorf("upstream edges = %d, want only the sc. reference", got)
	}
	if got := len(findEdgeSet(edges, EdgeOverlay, LabelContext).Rows); got != 1 {
		t.Errorf("overlay edges = %d, want 1", got)
	}
	if got := len(findEdgeSet(edges, EdgeReferences, LabelContext).Rows); got != 2 {
		t.Errorf("context references = %d, want 2 resolved refs", got)
	}
	if got := len(findEdgeSet(edges, EdgeReferences, LabelRequirement).Rows); got != 1 {
		t.Errorf("requirement references = %d, want 1", got)
	}
	if got := len(findEdgeSet(edges, EdgeDeclares, LabelContext).Rows); got != 1 {
		t.Errorf("declares edges = %d, want 1", got)
	}
	if got := len(findEdgeSet(edges, EdgeRequires, LabelContext).Rows); got != 1 {
		t.Errorf("requires edges = %d, want 1", got)
	}
}

func TestBuildResolvesArchRefsAndExtraEdges(t *testing.T) {
	parent := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "sc-deno-kcp"},
		Spec: spec.SystemContextSpec{
			Repository: "deno-kcp",
			Upstream:   "up.kcp",
			Arch:       &spec.ArchSpec{ID: "sc.deno-kcp", Kind: spec.ArchKindNode},
		},
	}
	child := spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "sc-kind-denopod"},
		Spec: spec.SystemContextSpec{
			Repository:   "deno-kcp",
			Upstream:     "sc.deno-kcp",
			Overlay:      []string{"ov.kcp-local-config"},
			Orchestrator: "orch.demo-deno-runtime",
			DependsOn:    []string{"sc.kcp.workspaces"},
			Introduces:   []string{"sc.deno-kcp-provider"},
			Arch:         &spec.ArchSpec{ID: "sc.kind.denopod", Kind: spec.ArchKindNode},
		},
	}
	snapshot := Snapshot{
		Repository: spec.Repository{ObjectMeta: metav1.ObjectMeta{Name: "deno-kcp"}},
		Contexts:   []spec.SystemContext{parent, child},
	}
	_, edges := Build(snapshot)
	want := map[string]string{
		EdgeUpstream:     "sc-deno-kcp",
		EdgeOverlay:      "ov-kcp-local-config",
		EdgeOrchestrator: "orch-demo-deno-runtime",
		EdgeDependsOn:    "sc-kcp-workspaces",
		EdgeIntroduces:   "sc-deno-kcp-provider",
	}
	for edgeType, name := range want {
		found := false
		for _, row := range findEdgeSet(edges, edgeType, LabelContext).Rows {
			if row.To == ContextID(name) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has no edge to the context named %s", edgeType, name)
		}
	}
	if got := len(findEdgeSet(edges, EdgeUpstream, LabelContext).Rows); got != 2 {
		t.Fatalf("upstream edges = %d, want 2", got)
	}
}

func TestRefTargetKnowsBothVocabularies(t *testing.T) {
	if name, ok := RefTarget("sc.calc", false); !ok || name != "calc" {
		t.Fatalf("plain ref = %q %v", name, ok)
	}
	if name, ok := RefTarget("sc.kind.denopod", true); !ok || name != "sc-kind-denopod" {
		t.Fatalf("arch ref = %q %v", name, ok)
	}
	if _, ok := RefTarget(spec.RefSelf, true); ok {
		t.Fatal("self points at nothing")
	}
	if _, ok := RefTarget("sc.calc", true); !ok {
		t.Fatal("sc.calc is also a valid arch id")
	}
}

func TestStableIDsAreContentKeyed(t *testing.T) {
	first := RepoID("calc")
	if first != RepoID("calc") {
		t.Error("repository ids must be stable")
	}
	if RepoID("calc") == RepoID("other") {
		t.Error("different repositories must not share an id")
	}
	if RequirementID("calc", "r.add") == RequirementID("calc", "r.sub") {
		t.Error("different requirements must not share an id")
	}
	if RequirementID("calc", "r.add") == RequirementID("cmd-calc", "r.add") {
		t.Error("the same requirement id in two contexts must not share a vertex")
	}
}

func TestBuildAllMergesRepositories(t *testing.T) {
	other := snapshot()
	other.Repository.Name = "other"
	vertices, _ := BuildAll([]Snapshot{snapshot(), other})
	if got := len(findSet(vertices, LabelRepo).Rows); got != 2 {
		t.Errorf("repos = %d, want 2", got)
	}
	if got := len(findSet(vertices, LabelContext).Rows); got != 4 {
		t.Errorf("contexts = %d, want 4", got)
	}
}

func TestCypherBuilders(t *testing.T) {
	if got := VertexUpsert(LabelContext, []string{"name", "intent"}); got != "UNWIND $rows AS row MERGE (n {id: row.id}) SET n:SpecContext, n.name = row.name, n.intent = row.intent" {
		t.Errorf("VertexUpsert = %s", got)
	}
	if got := EdgeCreate(EdgeUpstream, LabelContext, LabelContext); got != "UNWIND $rows AS row MATCH (a:SpecContext {id: row.src}), (b:SpecContext {id: row.dst}) CREATE (a)-[:UPSTREAM]->(b)" {
		t.Errorf("EdgeCreate = %s", got)
	}
	if got := VertexDelete(); got != "UNWIND $rows AS row MATCH (n {id: row.id}) DETACH DELETE n" {
		t.Errorf("VertexDelete = %s", got)
	}
	if got := VertexIDs(LabelCodeRef); got != "MATCH (n:CodeRef) RETURN n.id AS id" {
		t.Errorf("VertexIDs = %s", got)
	}
	if got := VertexSelect(LabelContext, []string{"name"}, map[string]any{"id": int64(7)}); got != "MATCH (n:SpecContext {id: 7}) RETURN n.name AS name" {
		t.Errorf("VertexSelect = %s", got)
	}
	if got := VertexSelect(LabelContext, []string{"name"}, nil); got != "MATCH (n:SpecContext) RETURN n.name AS name" {
		t.Errorf("VertexSelect without a filter = %s", got)
	}
	if got := EdgeSelectOut(EdgeRequires, LabelContext, LabelRequirement, 7, []string{"reqId"}); got != "MATCH (a:SpecContext {id: 7})-[:REQUIRES]->(b:SpecRequirement) RETURN b.reqId AS reqId" {
		t.Errorf("EdgeSelectOut = %s", got)
	}
	if got := EdgeSelectIn(EdgeUpstream, LabelContext, LabelContext, 7, []string{"name"}); got != "MATCH (a:SpecContext)-[:UPSTREAM]->(b:SpecContext {id: 7}) RETURN a.name AS name" {
		t.Errorf("EdgeSelectIn = %s", got)
	}
}

type fakeWriter struct {
	deleted []string
	written []string
	queries []string
	scopes  []string
}

func (w *fakeWriter) WriteVertices(_ context.Context, set VertexSet) error {
	w.written = append(w.written, set.Label)
	w.queries = append(w.queries, VertexUpsert(set.Label, LabelProperties[set.Label]))
	return nil
}

func (w *fakeWriter) WriteEdges(_ context.Context, set EdgeSet) error {
	w.written = append(w.written, set.Type)
	w.queries = append(w.queries, EdgeCreate(set.Type, set.FromLabel, set.ToLabel))
	return nil
}

func (w *fakeWriter) DeleteVertices(_ context.Context, ids []int64) error {
	if len(ids) > 0 {
		w.deleted = append(w.deleted, "batch")
	}
	return nil
}

func (w *fakeWriter) SelectIDs(_ context.Context, label string) ([]int64, error) {
	return []int64{1, 2}, nil
}

func (w *fakeWriter) SelectIDsWhere(_ context.Context, label string, filter map[string]any) ([]int64, error) {
	w.scopes = append(w.scopes, fmt.Sprint(filter[ScopeProperty]))
	return []int64{1, 2}, nil
}

func (w *fakeWriter) SelectOut(context.Context, string, string, string, int64, []string) ([]map[string]any, error) {
	return nil, nil
}

func (w *fakeWriter) SelectIn(context.Context, string, string, string, int64, []string) ([]map[string]any, error) {
	return nil, nil
}

func TestRebuildDeletesEveryManagedLabelThenWrites(t *testing.T) {
	writer := &fakeWriter{}
	if err := Rebuild(context.Background(), writer, "", []Snapshot{snapshot()}); err != nil {
		t.Fatal(err)
	}
	if len(writer.deleted) != len(ManagedLabels) {
		t.Errorf("deleted %d labels, want %d", len(writer.deleted), len(ManagedLabels))
	}
	for _, label := range ManagedLabels {
		if !contains(writer.written, label) {
			t.Errorf("%s was not written", label)
		}
	}
	for _, edgeSpec := range EdgeSpecs {
		if !contains(writer.written, edgeSpec.Type) {
			t.Errorf("%s edges were not written", edgeSpec.Type)
		}
	}
	for _, query := range writer.queries {
		if strings.Contains(query, "MERGE (n:") || strings.Contains(query, "SET n.") && !strings.HasPrefix(query, "UNWIND $rows AS row MERGE (n {id: row.id}) SET ") {
			t.Errorf("query leaves the core subset: %s", query)
		}
	}
}

type scopedWriter struct {
	fakeWriter

	deletedScopes []string

	ids map[string]map[string][]int64
}

func (w *scopedWriter) SelectIDsWhere(_ context.Context, label string, filter map[string]any) ([]int64, error) {
	scope := fmt.Sprint(filter[ScopeProperty])
	w.deletedScopes = append(w.deletedScopes, scope)
	return w.ids[scope][label], nil
}

func TestRebuildScopedDeletesOnlyItsOwnNamespace(t *testing.T) {
	writer := &scopedWriter{
		ids: map[string]map[string][]int64{
			"run-a": {LabelContext: {11}},
			"run-b": {LabelContext: {22}},
		},
	}
	if err := Rebuild(context.Background(), writer, "run-a", []Snapshot{snapshot()}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range writer.deletedScopes {
		if scope != "run-a" {
			t.Fatalf("a scoped rebuild deleted scope %q", scope)
		}
	}
	scoped, _ := BuildAll([]Snapshot{{Namespace: "run-a", Repository: snapshot().Repository}})
	plain, _ := BuildAll([]Snapshot{{Repository: snapshot().Repository}})
	if findSet(scoped, LabelRepo).Rows[0].ID == findSet(plain, LabelRepo).Rows[0].ID {
		t.Error("a namespaced snapshot reused the unscoped repo id")
	}
	if got := findSet(scoped, LabelRepo).Rows[0].Props[ScopeProperty]; got != "run-a" {
		t.Errorf("scope property = %v", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
