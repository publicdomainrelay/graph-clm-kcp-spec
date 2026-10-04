package clm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
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

func key(gvr schema.GroupVersionResource, namespace, name string) string {
	return gvr.Resource + "/" + namespace + "/" + name
}

func (f *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	stored, ok := f.objects[key(gvr, namespace, name)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
	}
	return stored.DeepCopy(), nil
}

func (f *fakeCluster) List(_ context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	for objectKey, object := range f.objects {
		if strings.HasPrefix(objectKey, gvr.Resource+"/") && object.GetNamespace() == namespace {
			list.Items = append(list.Items, *object.DeepCopy())
		}
	}
	return list, nil
}

func (f *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	gvr, err := specapi.GVRForKind(object.GetKind())
	if err != nil {
		return nil, err
	}
	stored := object.DeepCopy()
	stored.SetResourceVersion("1")
	f.objects[key(gvr, object.GetNamespace(), object.GetName())] = stored
	return stored.DeepCopy(), nil
}

func (f *fakeCluster) PatchStatus(_ context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error) {
	stored, ok := f.objects[key(gvr, namespace, name)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
	}
	encoded, err := json.Marshal(map[string]any{"status": status})
	if err != nil {
		return nil, err
	}
	patch := map[string]any{}
	if err := json.Unmarshal(encoded, &patch); err != nil {
		return nil, err
	}
	updated := stored.DeepCopy()
	existing, _, err := unstructured.NestedMap(updated.Object, "status")
	if err != nil {
		return nil, err
	}
	for name, value := range patch["status"].(map[string]any) {
		existing[name] = value
	}
	if err := unstructured.SetNestedMap(updated.Object, existing, "status"); err != nil {
		return nil, err
	}
	f.objects[key(gvr, namespace, name)] = updated
	return updated.DeepCopy(), nil
}

func applyObject(t *testing.T, cluster Cluster, object any) {
	t.Helper()
	spec.SetDefaults(object)
	stamped, err := kcpclient.Unstructured(object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Apply(context.Background(), stamped); err != nil {
		t.Fatal(err)
	}
}

func contextOf(t *testing.T, cluster Cluster) *spec.SystemContext {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return typed.(*spec.SystemContext)
}

func fixture(t *testing.T) *fakeCluster {
	t.Helper()
	cluster := newFakeCluster()
	applyObject(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: "/tmp/calc"},
	})
	applyObject(t, cluster, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   spec.RefSelf,
			Intent:     "Add adds two integers.",
			Interfaces: []spec.Interface{{Name: "Add", Kind: "function"}},
			CodeRefs:   []string{"file:calc/calc.go"},
		},
	})
	return cluster
}

func TestRenderCarriesTheSpecAndTheResolvedRefs(t *testing.T) {
	cluster := fixture(t)
	document, err := Render(context.Background(), Options{Cluster: cluster, Context: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(document.Document, "Add adds two integers.") {
		t.Errorf("the document lost the intent:\n%s", document.Document)
	}
	if !strings.Contains(document.Document, "file:calc/calc.go") {
		t.Errorf("the document lost the code ref:\n%s", document.Document)
	}
	if _, err := clm.ParseModelZone(document.ModelZone); err != nil {
		t.Errorf("the rendered model zone does not parse: %v", err)
	}
}

func TestApplyIsANoOpWhenNothingChanged(t *testing.T) {
	cluster := fixture(t)
	rendered, err := Render(context.Background(), Options{Cluster: cluster, Context: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), Options{Cluster: cluster, Context: "calc"}, rendered.ModelZone)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied || !result.Delta.Empty() {
		t.Errorf("an unchanged model zone applied: %+v", result)
	}
}

func TestApplyWritesTheDeltaWithTheCLMOrigin(t *testing.T) {
	cluster := fixture(t)
	edited := addSubtract(t, cluster)
	result, err := Apply(context.Background(), Options{Cluster: cluster, Context: "calc"}, edited)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatal("the edit did not apply")
	}
	if len(result.Delta.Interfaces) != 1 || result.Delta.Interfaces[0].Name != "Subtract" {
		t.Errorf("delta = %+v", result.Delta)
	}
	current := contextOf(t, cluster)
	if current.Annotations[specapi.OriginAnnotation] != specapi.OriginCLM {
		t.Errorf("origin = %q", current.Annotations[specapi.OriginAnnotation])
	}
	if _, stamped := current.Annotations[specapi.OriginHashAnnotation]; stamped {
		t.Error("a clm write carries origin-hash, which would hide the edit from the controller")
	}
	if len(current.Spec.Interfaces) != 2 {
		t.Errorf("interfaces = %+v", current.Spec.Interfaces)
	}
	if len(current.Spec.CodeRefs) != 1 {
		t.Errorf("the tool-owned code refs were dropped: %v", current.Spec.CodeRefs)
	}
}

func TestApplyFoldsAnEditIntoTheRunningChange(t *testing.T) {
	cluster := fixture(t)
	applyObject(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-abc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc",
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    strings.Repeat("b", 64),
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseRunning},
	})
	edited := addSubtract(t, cluster)
	result, err := Apply(context.Background(), Options{Cluster: cluster, Context: "calc", Now: fixedNow}, edited)
	if err != nil {
		t.Fatal(err)
	}
	if result.Folded != "calc-s2c-abc" {
		t.Fatalf("folded = %q, want the running change", result.Folded)
	}
	if result.Applied {
		t.Error("the spec was written while the change that realizes it is Running")
	}
	merged := contextOf(t, cluster)
	if len(merged.Spec.Interfaces) != 1 {
		t.Errorf("interfaces = %+v, want the spec to hold still", merged.Spec.Interfaces)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-abc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change := typed.(*spec.SpecChange)
	if len(change.Status.Progress) != 1 {
		t.Fatalf("progress = %+v", change.Status.Progress)
	}
	if !strings.Contains(change.Status.Progress[0].Note, "folded into this change") {
		t.Errorf("note = %q", change.Status.Progress[0].Note)
	}
	if len(cluster.objects) != 3 {
		t.Errorf("the apply created an object; it must never spawn a change: %d", len(cluster.objects))
	}
}

func TestReportAppendsToTheBoundedListAndWritesTheGraph(t *testing.T) {
	cluster := fixture(t)
	applyObject(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-abc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc",
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    strings.Repeat("b", 64),
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseRunning},
	})
	writer := &fakeWriter{}
	options := Options{Cluster: cluster, Context: "calc", Writer: writer, Now: fixedNow}
	for turn := 1; turn <= 3; turn++ {
		if _, err := Report(context.Background(), options, ReportOptions{
			Change: "calc-s2c-abc",
			Event:  Event{Turn: turn, Tool: "Write", Files: []string{"calc/calc.go"}, Note: "wrote Subtract"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-abc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change := typed.(*spec.SpecChange)
	if len(change.Status.Progress) != 3 {
		t.Fatalf("progress = %+v", change.Status.Progress)
	}
	if change.Status.Progress[0].At != fixedNow().UTC().Format(time.RFC3339) {
		t.Errorf("at = %q", change.Status.Progress[0].At)
	}
	for index, vertex := range writer.labels[graph.LabelChange] {
		if vertex.ID != graph.ChangeID("calc-s2c-abc") || index > 0 && vertex.ID != writer.labels[graph.LabelChange][0].ID {
			t.Errorf("change vertex %d = %+v", index, vertex)
		}
	}
	if len(writer.edges[graph.EdgeTouched]) != 3 {
		t.Errorf("touched edges = %+v", writer.edges[graph.EdgeTouched])
	}
	if len(writer.edges[graph.EdgeOccurred]) != 3 {
		t.Errorf("occurred edges = %+v", writer.edges[graph.EdgeOccurred])
	}
	if writer.labels[graph.LabelCodeRef][0].ID != graph.FileCodeRefID("calc/calc.go") {
		t.Error("the touched file did not land on the shared coderef id")
	}
}

func TestARepeatedRecordIsNotAppendedTwice(t *testing.T) {
	cluster := fixture(t)
	applyObject(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-abc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc",
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    strings.Repeat("b", 64),
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseRunning},
	})
	options := Options{Cluster: cluster, Context: "calc", Now: fixedNow}
	event := Event{Turn: 1, Tool: "Read", Files: []string{"calc/calc.go"}}
	first, err := Report(context.Background(), options, ReportOptions{Change: "calc-s2c-abc", Event: event})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Report(context.Background(), options, ReportOptions{Change: "calc-s2c-abc", Event: event})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Recorded || second.Recorded {
		t.Errorf("first = %+v second = %+v", first, second)
	}
}

func TestProgressListIsBounded(t *testing.T) {
	status := spec.SpecChangeStatus{}
	for index := 0; index < spec.MaxProgressRecords+10; index++ {
		status.AppendProgress(spec.ProgressRecord{Turn: index, Tool: "Write", Note: "step"})
	}
	if len(status.Progress) != spec.MaxProgressRecords {
		t.Fatalf("progress = %d, want %d", len(status.Progress), spec.MaxProgressRecords)
	}
	if status.Progress[len(status.Progress)-1].Turn != spec.MaxProgressRecords+9 {
		t.Errorf("the newest record fell off: %+v", status.Progress[len(status.Progress)-1])
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
}

func addSubtract(t *testing.T, cluster *fakeCluster) string {
	t.Helper()
	rendered, err := Render(context.Background(), Options{Cluster: cluster, Context: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := clm.ParseModelZone(rendered.ModelZone)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Interfaces = append(parsed.Interfaces, spec.Interface{Name: "Subtract", Kind: "function"})
	edited, err := clm.RenderModelZone("calc", "calc", parsed)
	if err != nil {
		t.Fatal(err)
	}
	if edited == rendered.ModelZone {
		t.Fatal("the edit did not change the model zone")
	}
	return edited
}

type fakeWriter struct {
	labels map[string][]graph.Vertex
	edges  map[string][]graph.Edge
}

func (f *fakeWriter) record(label string, rows []graph.Vertex) {
	if f.labels == nil {
		f.labels = map[string][]graph.Vertex{}
	}
	f.labels[label] = append(f.labels[label], rows...)
}

func (f *fakeWriter) WriteVertices(_ context.Context, set graph.VertexSet) error {
	f.record(set.Label, set.Rows)
	return nil
}

func (f *fakeWriter) WriteEdges(_ context.Context, set graph.EdgeSet) error {
	if f.edges == nil {
		f.edges = map[string][]graph.Edge{}
	}
	f.edges[set.Type] = append(f.edges[set.Type], set.Rows...)
	return nil
}

func (f *fakeWriter) DeleteVertices(context.Context, []int64) error { return nil }

func (f *fakeWriter) SelectIDs(context.Context, string) ([]int64, error) { return nil, nil }

func (f *fakeWriter) SelectOut(context.Context, string, string, string, int64, []string) ([]map[string]any, error) {
	return nil, nil
}

func (f *fakeWriter) SelectIn(context.Context, string, string, string, int64, []string) ([]map[string]any, error) {
	return nil, nil
}
