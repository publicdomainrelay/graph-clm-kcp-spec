package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

type fakeCluster struct {
	objects  map[string]*unstructured.Unstructured
	statuses int
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
}

func objectKey(gvr schema.GroupVersionResource, namespace, name string) string {
	return gvr.Resource + "/" + namespace + "/" + name
}

func (c *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	if object, ok := c.objects[objectKey(gvr, namespace, name)]; ok {
		return object.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
}

func (c *fakeCluster) List(_ context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	for key, object := range c.objects {
		if len(key) < len(gvr.Resource) || key[:len(gvr.Resource)] != gvr.Resource {
			continue
		}
		list.Items = append(list.Items, *object.DeepCopy())
	}
	return list, nil
}

func (c *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	key := objectKey(gvrFor(object), object.GetNamespace(), object.GetName())
	stored, exists := c.objects[key]
	if !exists {
		created := object.DeepCopy()
		created.SetGeneration(1)
		created.SetResourceVersion("1")
		c.objects[key] = created
		return created.DeepCopy(), nil
	}
	updated := object.DeepCopy()
	updated.SetResourceVersion(stored.GetResourceVersion())
	if !specEqual(stored, updated) {
		updated.SetGeneration(stored.GetGeneration() + 1)
	} else {
		updated.SetGeneration(stored.GetGeneration())
	}
	c.objects[key] = updated
	return updated.DeepCopy(), nil
}

func (c *fakeCluster) PatchStatus(_ context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error) {
	key := objectKey(gvr, namespace, name)
	stored, ok := c.objects[key]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
	}
	updated := stored.DeepCopy()
	normalized, ok := normalize(status).(map[string]any)
	if !ok {
		return nil, apierrors.NewInternalError(errors.New("status is not an object"))
	}
	if err := unstructured.SetNestedMap(updated.Object, normalized, "status"); err != nil {
		return nil, err
	}
	version, _ := strconv.Atoi(stored.GetResourceVersion())
	updated.SetResourceVersion(strconv.Itoa(version + 1))
	c.objects[key] = updated
	c.statuses++
	return updated.DeepCopy(), nil
}

func normalize(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return value
	}
	return out
}

func specEqual(left, right *unstructured.Unstructured) bool {
	leftSpec, _, _ := unstructured.NestedMap(left.Object, "spec")
	rightSpec, _, _ := unstructured.NestedMap(right.Object, "spec")
	return reflect.DeepEqual(normalize(leftSpec), normalize(rightSpec))
}

func gvrFor(object *unstructured.Unstructured) schema.GroupVersionResource {
	gvr, err := specapi.GVRForKind(object.GetKind())
	if err != nil {
		return schema.GroupVersionResource{}
	}
	return gvr
}

func TestRunCreatesRepositoryAndContexts(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repoPath := fixture.Copy(t, "calc")
	cluster := newFakeCluster()

	result, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: "cafebabe"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Repository != "calc" || result.Commit != "cafebabe" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Contexts) != 2 {
		t.Fatalf("contexts = %+v, want calc and cmd-calc", result.Contexts)
	}

	library := result.Contexts[0]
	if library.Name != "calc" || !library.Created || !library.SpecChanged {
		t.Errorf("library result = %+v", library)
	}
	if len(library.Observed.Files) != 2 || len(library.Observed.Interfaces) != 2 {
		t.Errorf("library observed = %+v", library.Observed)
	}
	if library.Observed.Fingerprint == "" {
		t.Error("no fingerprint")
	}

	stored, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(stored)
	if err != nil {
		t.Fatal(err)
	}
	context, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("stored a %T", typed)
	}
	if context.Spec.Repository != "calc" {
		t.Errorf("repository = %q", context.Spec.Repository)
	}
	want := map[string]bool{"file:calc/calc.go": true, "file:calc/calc_test.go": true}
	for _, ref := range context.Spec.CodeRefs {
		delete(want, ref)
	}
	if len(want) != 0 {
		t.Errorf("code refs = %v, missing %v", context.Spec.CodeRefs, want)
	}
	if context.Status.ObservedCommit != "cafebabe" {
		t.Errorf("observedCommit = %q", context.Status.ObservedCommit)
	}
	if context.Status.RealizedSpecHash == "" {
		t.Error("ingest must set realizedSpecHash so the write does not look like a human edit")
	}
	valid := conditionOf(context.Status.Conditions, specapi.ConditionSpecValid)
	if valid == nil || valid.Status != metav1.ConditionTrue {
		t.Errorf("SpecValid = %+v", valid)
	}
	synced := conditionOf(context.Status.Conditions, specapi.ConditionCodeSynced)
	if synced == nil || synced.Status != metav1.ConditionFalse {
		t.Errorf("CodeSynced without a declared interface = %+v, want False", synced)
	}
	drifted := conditionOf(context.Status.Conditions, specapi.ConditionDrifted)
	if drifted == nil || drifted.Status != metav1.ConditionFalse {
		t.Errorf("Drifted on a first ingest = %+v, want False", drifted)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repoPath := fixture.Copy(t, "calc")
	cluster := newFakeCluster()

	first, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: "cafebabe"})
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotCluster(t, cluster)
	statuses := cluster.statuses

	second, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: "cafebabe"})
	if err != nil {
		t.Fatal(err)
	}
	if cluster.statuses != statuses {
		t.Errorf("the second ingest wrote status %d times, want no writes", cluster.statuses-statuses)
	}
	for index := range first.Contexts {
		if first.Contexts[index].Fingerprint != second.Contexts[index].Fingerprint {
			t.Errorf("fingerprint %d changed: %s -> %s", index, first.Contexts[index].Fingerprint, second.Contexts[index].Fingerprint)
		}
		if second.Contexts[index].SpecChanged || second.Contexts[index].Created {
			t.Errorf("the second ingest changed %s: %+v", second.Contexts[index].Name, second.Contexts[index])
		}
	}
	after := snapshotCluster(t, cluster)
	for key, want := range before {
		if after[key] != want {
			t.Errorf("%s changed:\n%s\n%s", key, want, after[key])
		}
	}
}

func TestRunPreservesHumanSpecFields(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repoPath := fixture.Copy(t, "calc")
	cluster := newFakeCluster()

	declared := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Intent:     "the human wrote this",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "add", CodeRefs: []string{"function:Add"}},
			},
			Interfaces: []spec.Interface{
				{Name: "Add", Kind: "function", Signature: "(a, b int) int"},
				{Name: "Multiply", Kind: "function", Signature: "(a, b int) int"},
			},
			CodeRefs: []string{"function:Add"},
		},
	}
	declared.SetDefaults()
	object, err := kcpclient.Unstructured(declared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Apply(context.Background(), object); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: "cafebabe"}); err != nil {
		t.Fatal(err)
	}
	stored, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(stored)
	if err != nil {
		t.Fatal(err)
	}
	context := typed.(*spec.SystemContext)
	if context.Spec.Intent != "the human wrote this" {
		t.Errorf("intent = %q", context.Spec.Intent)
	}
	if len(context.Spec.Requirements) != 1 || len(context.Spec.Interfaces) != 2 {
		t.Errorf("spec = %+v", context.Spec)
	}
	if context.Annotations[specapi.OriginAnnotation] != specapi.OriginIngest {
		t.Errorf("annotations = %v", context.Annotations)
	}
	found := false
	for _, ref := range context.Spec.CodeRefs {
		if ref == "function:Add" {
			found = true
		}
	}
	if !found {
		t.Errorf("the human symbol ref was dropped: %v", context.Spec.CodeRefs)
	}
	synced := conditionOf(context.Status.Conditions, specapi.ConditionCodeSynced)
	if synced == nil || synced.Status != metav1.ConditionTrue {
		t.Errorf("CodeSynced = %+v, want True", synced)
	}
}

// A context that has never been acknowledged gets its baseline from the first
// ingest, and the object says which spec that was, so a reconcile running
// before the status lands does not read the tool's own write as a human edit.
func TestRunStampsTheOriginHashWhenItAbsorbsItsOwnWrite(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repoPath := fixture.Copy(t, "calc")
	cluster := newFakeCluster()

	if _, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: "cafebabe"}); err != nil {
		t.Fatal(err)
	}
	context := storedContext(t, cluster, "calc")
	hash, err := spec.HashSystemContextSpec(context.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if context.Annotations[specapi.OriginHashAnnotation] != hash {
		t.Errorf("origin hash = %q, want the hash of the spec ingest wrote (%q)",
			context.Annotations[specapi.OriginHashAnnotation], hash)
	}
	if context.Status.RealizedSpecHash != hash {
		t.Errorf("realizedSpecHash = %q, want %q", context.Status.RealizedSpecHash, hash)
	}
}

// The other half of the rule: an ingest that runs while a human edit is
// pending absorbs nothing, so it must leave the origin hash off the object. A
// stale stamp there would make the spec hash to the annotation and the
// controller would never raise the SpecToCode change that realizes the edit.
func TestRunKeepsAHumanEditVisibleAcrossALaterIngest(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repoPath := fixture.Copy(t, "calc")
	cluster := newFakeCluster()

	if _, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: "cafebabe"}); err != nil {
		t.Fatal(err)
	}
	acknowledged := storedContext(t, cluster, "calc").Status.RealizedSpecHash

	// A human edits the spec the way kubectl apply does: the whole object, so
	// the annotations the tool left are carried along.
	edited := storedContext(t, cluster, "calc")
	edited.Spec.Intent = "the human wrote this"
	stamped, err := kcpclient.Unstructured(edited)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Apply(context.Background(), stamped); err != nil {
		t.Fatal(err)
	}

	// The tree gains a file, so the next ingest rewrites the spec's code refs
	// while the human edit is still pending.
	if err := os.WriteFile(filepath.Join(repoPath, "calc", "extra.go"), []byte("package calc\n\n// Double doubles an integer.\nfunc Double(a int) int { return a * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := fixture.Commit(t, repoPath, "add Double")
	if _, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: commit}); err != nil {
		t.Fatal(err)
	}

	context := storedContext(t, cluster, "calc")
	if context.Spec.Intent != "the human wrote this" {
		t.Errorf("intent = %q, want the human's", context.Spec.Intent)
	}
	if context.Status.RealizedSpecHash != acknowledged {
		t.Errorf("realizedSpecHash = %q, want the unchanged %q", context.Status.RealizedSpecHash, acknowledged)
	}
	hash, err := spec.HashSystemContextSpec(context.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if hash == acknowledged {
		t.Fatal("the ingest did not change the spec, so the test proves nothing")
	}
	if origin := context.Annotations[specapi.OriginHashAnnotation]; origin == hash {
		t.Errorf("origin hash = %q equals the spec, which hides the human edit", origin)
	}
}

func storedContext(t *testing.T, cluster *fakeCluster, name string) *spec.SystemContext {
	t.Helper()
	stored, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(stored)
	if err != nil {
		t.Fatal(err)
	}
	context, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	return context
}

func TestMergeCodeRefs(t *testing.T) {
	merged := mergeCodeRefs([]string{"function:Add", "file:stale.go"}, []string{"calc/calc.go", "calc/calc_test.go"})
	want := []string{"calc/calc_test.go", "calc/calc.go", "function:Add"}
	_ = want
	if len(merged) != 3 {
		t.Fatalf("merged = %v", merged)
	}
	if merged[0] != "file:calc/calc.go" || merged[1] != "file:calc/calc_test.go" || merged[2] != "function:Add" {
		t.Fatalf("merged = %v", merged)
	}
}

func conditionOf(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for index := range conditions {
		if conditions[index].Type == conditionType {
			return &conditions[index]
		}
	}
	return nil
}

func snapshotCluster(t *testing.T, cluster *fakeCluster) map[string]string {
	t.Helper()
	out := map[string]string{}
	for key, object := range cluster.objects {
		encoded, err := object.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		out[key] = string(encoded)
	}
	return out
}

func TestRunLeavesAContextOfAnotherRepositoryAlone(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repoPath := fixture.Copy(t, "calc")
	cluster := newFakeCluster()

	// The calc directory of this tree partitions to a context name that a
	// different Repository already owns.
	foreign := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "someone-else",
			Upstream:   spec.RefSelf,
			Intent:     "owned elsewhere",
			Interfaces: []spec.Interface{{Name: "Add", Kind: "function"}},
		},
	}
	foreign.SetDefaults()
	object, err := kcpclient.Unstructured(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Apply(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	stored, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	before := stored.GetResourceVersion()

	result, err := Run(context.Background(), cluster, Options{RepoPath: repoPath, Commit: "cafebabe"})
	if err != nil {
		t.Fatal(err)
	}
	skipped := map[string]bool{}
	for _, context := range result.Contexts {
		if context.Skipped {
			skipped[context.Name] = true
			if context.Reason == "" {
				t.Errorf("%s was skipped without a reason", context.Name)
			}
		}
	}
	if !skipped["calc"] {
		t.Fatalf("contexts = %+v, want calc skipped", result.Contexts)
	}
	if skipped["cmd-calc"] {
		t.Error("cmd-calc belongs to no one and must be ingested")
	}

	after, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if after.GetResourceVersion() != before {
		t.Errorf("the foreign context was written: %s -> %s", before, after.GetResourceVersion())
	}
	if intent, _, _ := unstructured.NestedString(after.Object, "spec", "intent"); intent != "owned elsewhere" {
		t.Errorf("spec.intent = %q, want the owner's value", intent)
	}
}
