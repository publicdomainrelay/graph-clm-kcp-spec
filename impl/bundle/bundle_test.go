package bundle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
}

func (f *fakeCluster) put(object any) {
	spec.SetDefaults(object)
	stamped, err := kcpclient.Unstructured(object)
	if err != nil {
		panic(err)
	}
	f.objects[stamped.GetKind()+"/"+stamped.GetName()] = stamped
}

func (f *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, _, name string) (*unstructured.Unstructured, error) {
	for _, object := range f.objects {
		if object.GetName() == name && specapi.ResourceForKind(object.GetKind()) == gvr.Resource {
			return object.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
}

type fakeGraph struct {
	out map[string][]map[string]any

	in map[string][]map[string]any
}

func (f *fakeGraph) WriteVertices(context.Context, graph.VertexSet) error { return nil }

func (f *fakeGraph) WriteEdges(context.Context, graph.EdgeSet) error { return nil }

func (f *fakeGraph) DeleteVertices(context.Context, []int64) error { return nil }

func (f *fakeGraph) SelectIDs(context.Context, string) ([]int64, error) { return nil, nil }

func (f *fakeGraph) SelectIDsWhere(context.Context, string, map[string]any) ([]int64, error) {
	return nil, nil
}

func (f *fakeGraph) SelectOut(_ context.Context, edgeType, _, _ string, _ int64, _ []string) ([]map[string]any, error) {
	return f.out[edgeType], nil
}

func (f *fakeGraph) SelectIn(_ context.Context, edgeType, _, _ string, _ int64, _ []string) ([]map[string]any, error) {
	return f.in[edgeType], nil
}

type fakeCodegraph struct {
	nodes map[string]string

	context string

	err error
}

func (f *fakeCodegraph) Context(context.Context, string, int) (string, error) {
	return f.context, f.err
}

func (f *fakeCodegraph) Node(_ context.Context, name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.nodes[name], nil
}

func repository(t *testing.T, path string) *spec.Repository {
	t.Helper()
	return &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: path},
	}
}

func systemContext() *spec.SystemContext {
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository:   "calc",
			Upstream:     spec.RefSelf,
			Intent:       "Arithmetic.",
			Requirements: []spec.Requirement{{ID: "r.add", Level: spec.LevelMust, Text: "Add adds."}},
		},
		Status: spec.SystemContextStatus{
			Observed: spec.ObservedFacts{
				Files: []string{"calc/calc.go"},
				Interfaces: []spec.ObservedInterface{
					{Name: "Add", Kind: "function", CodegraphID: "function:abc", File: "calc/calc.go", Line: 3},
					{Name: "Multiply", Kind: "function", CodegraphID: "function:def", File: "calc/calc.go", Line: 8},
				},
				Fingerprint: "f1",
			},
		},
	}
}

func TestBuildReadsTheContextTheFactsAndTheDocument(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteContextDoc(dir, "calc", "calc", "# Calc\n\nMy notes.", nil, 100); err != nil {
		t.Fatal(err)
	}
	cluster := newFakeCluster()
	cluster.put(systemContext())
	cluster.put(repository(t, dir))

	built, err := Build(context.Background(), Options{Cluster: cluster, Context: "calc", DocDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if built.Spec.Intent != "Arithmetic." || built.Repository != "calc" {
		t.Errorf("bundle = %+v", built)
	}
	if !strings.Contains(built.ContextDoc, "My notes.") {
		t.Errorf("context doc = %q", built.ContextDoc)
	}
	if built.Budget != DefaultBudget {
		t.Errorf("budget = %d, want the default", built.Budget)
	}
}

func TestBuildAddsOneHopOfTheGraphInBothDirections(t *testing.T) {
	cluster := newFakeCluster()
	cluster.put(systemContext())
	cluster.put(repository(t, t.TempDir()))

	writer := &fakeGraph{
		out: map[string][]map[string]any{
			graph.EdgeUpstream: {{"name": "math"}},
			graph.EdgeRequires: {{"reqId": "r.add", "level": "MUST"}},
		},
		in: map[string][]map[string]any{
			graph.EdgeOverlay: {{"name": "cli"}},
		},
	}
	built, err := Build(context.Background(), Options{Cluster: cluster, Context: "calc", Writer: writer})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, neighbor := range built.Neighbors {
		names[neighbor.Edge+"/"+neighbor.Direction] = neighbor.Name
	}
	if names[graph.EdgeUpstream+"/"+agent.DirectionOut] != "math" {
		t.Errorf("neighbors = %+v", built.Neighbors)
	}
	if names[graph.EdgeRequires+"/"+agent.DirectionOut] != "r.add" {
		t.Errorf("a requirement neighbor lost its name: %+v", built.Neighbors)
	}
	if names[graph.EdgeOverlay+"/"+agent.DirectionIn] != "cli" {
		t.Errorf("neighbors = %+v", built.Neighbors)
	}
}

func indexed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".codegraph", "codegraph.db"), []byte("index"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuildLeavesTheIndexAloneWhenThereIsNone(t *testing.T) {
	cluster := newFakeCluster()
	cluster.put(systemContext())
	cluster.put(repository(t, t.TempDir()))

	codegraph := &fakeCodegraph{context: "should never be asked"}
	built, err := Build(context.Background(), Options{Cluster: cluster, Context: "calc", Codegraph: codegraph})
	if err != nil {
		t.Fatal(err)
	}
	if len(built.CodeExcerpts) != 0 {
		t.Errorf("excerpts = %+v, want none without an index", built.CodeExcerpts)
	}
}

func TestBuildAddsCodegraphExcerptsAndSkipsWhatItCannotRead(t *testing.T) {
	cluster := newFakeCluster()
	cluster.put(systemContext())
	cluster.put(repository(t, indexed(t)))

	codegraph := &fakeCodegraph{
		nodes:   map[string]string{"Add": "func Add(a, b int) int { return a + b }"},
		context: "### relevant symbols",
	}
	built, err := Build(context.Background(), Options{Cluster: cluster, Context: "calc", Codegraph: codegraph})
	if err != nil {
		t.Fatal(err)
	}
	sources := []string{}
	for _, excerpt := range built.CodeExcerpts {
		sources = append(sources, excerpt.Source)
	}
	joined := strings.Join(sources, ",")
	if !strings.Contains(joined, "codegraph node Add") || strings.Contains(joined, "codegraph node Multiply") {
		t.Errorf("sources = %v, want the readable node only plus the context", sources)
	}
	if !strings.Contains(joined, "codegraph context") {
		t.Errorf("sources = %v, want the context excerpt", sources)
	}
}

func TestBuildSurvivesACodegraphThatFails(t *testing.T) {
	cluster := newFakeCluster()
	cluster.put(systemContext())
	cluster.put(repository(t, t.TempDir()))

	codegraph := &fakeCodegraph{err: errors.New("codegraph is having a bad day")}
	cluster.put(repository(t, indexed(t)))
	built, err := Build(context.Background(), Options{Cluster: cluster, Context: "calc", Codegraph: codegraph})
	if err != nil {
		t.Fatalf("a failing codegraph killed the bundle: %v", err)
	}
	if len(built.CodeExcerpts) != 0 {
		t.Errorf("excerpts = %+v, want none", built.CodeExcerpts)
	}
	if built.Spec.Intent == "" {
		t.Error("the bundle lost the spec")
	}
}

func TestBuildFailsWhenTheContextIsMissing(t *testing.T) {
	cluster := newFakeCluster()
	if _, err := Build(context.Background(), Options{Cluster: cluster, Context: "calc"}); err == nil {
		t.Fatal("a missing context built a bundle")
	}
}

func TestWriteContextDocKeepsTheModelZoneAndRegeneratesTheManagedOne(t *testing.T) {
	dir := t.TempDir()
	refs := ResolvedRefs(systemContext().Status.Observed)
	path, err := WriteContextDoc(dir, "calc", "calc", "# Calc\n\nSummary.", refs, 100)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "calc", "calc.md"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "Summary.") || !strings.Contains(string(contents), "function:abc") {
		t.Fatalf("document = %q", contents)
	}

	if _, err := WriteContextDoc(dir, "calc", "calc", "", nil, 100); err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "Summary.") {
		t.Error("the model zone was erased")
	}
	if strings.Contains(string(contents), "function:abc") {
		t.Error("the managed zone was not regenerated from the new facts")
	}
}

func TestReadContextDocTreatsAMissingFileAsAnEmptyModelZone(t *testing.T) {
	model, err := ReadContextDoc(t.TempDir(), "calc", "calc")
	if err != nil {
		t.Fatal(err)
	}
	if model != "" {
		t.Errorf("model = %q", model)
	}
}
