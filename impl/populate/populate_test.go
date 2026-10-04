package populate

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured

	writes int
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
}

func keyOf(object *unstructured.Unstructured) string {
	return object.GetKind() + "/" + object.GetName()
}

func (f *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, _, name string) (*unstructured.Unstructured, error) {
	for _, object := range f.objects {
		if object.GetName() == name && specapi.ResourceForKind(object.GetKind()) == gvr.Resource {
			return object.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
}

func (f *fakeCluster) List(_ context.Context, gvr schema.GroupVersionResource, _ string) (*unstructured.UnstructuredList, error) {
	listed := &unstructured.UnstructuredList{}
	for _, object := range f.objects {
		if specapi.ResourceForKind(object.GetKind()) == gvr.Resource {
			listed.Items = append(listed.Items, *object.DeepCopy())
		}
	}
	return listed, nil
}

func (f *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	stored, ok := f.objects[keyOf(object)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: object.GroupVersionKind().Group}, object.GetName())
	}
	updated := object.DeepCopy()
	updated.SetResourceVersion(stored.GetResourceVersion())
	f.objects[keyOf(object)] = updated
	f.writes++
	return updated.DeepCopy(), nil
}

func (f *fakeCluster) Create(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if _, ok := f.objects[keyOf(object)]; ok {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: object.GroupVersionKind().Group}, object.GetName())
	}
	created := object.DeepCopy()
	created.SetResourceVersion("1")
	f.objects[keyOf(object)] = created
	f.writes++
	return created.DeepCopy(), nil
}

func (f *fakeCluster) PatchStatus(_ context.Context, gvr schema.GroupVersionResource, _, name string, status map[string]any) (*unstructured.Unstructured, error) {
	for key, object := range f.objects {
		if object.GetName() != name || specapi.ResourceForKind(object.GetKind()) != gvr.Resource {
			continue
		}
		updated := object.DeepCopy()
		current, _, err := unstructured.NestedMap(updated.Object, "status")
		if err != nil {
			return nil, err
		}
		normalized, ok := normalize(status).(map[string]any)
		if !ok {
			return nil, apierrors.NewInternalError(errors.New("status is not an object"))
		}
		maps.Copy(current, normalized)
		if err := unstructured.SetNestedMap(updated.Object, current, "status"); err != nil {
			return nil, err
		}
		f.objects[key] = updated
		f.writes++
		return updated.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
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

func setup(t *testing.T, summarize bool, maxConcurrent int) (*fakeCluster, *spec.Repository) {
	t.Helper()
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "unseen", Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Source:   &spec.RepositorySource{Path: "/src/unseen"},
			Populate: &spec.RepositoryPopulate{Partition: spec.PartitionDirectory, Summarize: summarize},
		},
		Status: spec.RepositoryStatus{
			Phase:           specapi.PhasePopulating,
			ResolvedPath:    "/src/unseen",
			HeadCommit:      "c1",
			IndexedCommit:   "c1",
			PopulateRequest: "req-1",
		},
	}
	cluster := newFakeCluster()
	store(t, cluster, repository)
	for _, name := range []string{"calc", "greet"} {
		store(t, cluster, &spec.SystemContext{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
			Spec:       spec.SystemContextSpec{Repository: "unseen", Upstream: spec.RefSelf},
			Status: spec.SystemContextStatus{
				ObservedCommit: "c1", SyncedCommit: "c1",
				Observed: spec.ObservedFacts{Files: []string{name + "/mod.ts"}, Fingerprint: "f1"},
			},
		})
	}
	store(t, cluster, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SystemContextSpec{Repository: "unseen", Upstream: spec.RefSelf, Intent: "already summarized"},
		Status:     spec.SystemContextStatus{ObservedCommit: "c1", SyncedCommit: "c1"},
	})
	return cluster, repository
}

func store(t *testing.T, cluster *fakeCluster, object any) {
	t.Helper()
	spec.SetDefaults(object)
	stamped, err := kcpclient.Unstructured(object)
	if err != nil {
		t.Fatal(err)
	}
	cluster.objects[keyOf(stamped)] = stamped
}

func run(t *testing.T, cluster *fakeCluster, repository *spec.Repository, maxConcurrent, maxAttempts int) (Result, *spec.Repository) {
	t.Helper()
	result, err := Run(context.Background(), Options{
		Cluster: cluster, Namespace: specapi.DefaultNamespace, Repository: repository,
		Path: "/src/unseen", Commit: "c1", MaxConcurrent: maxConcurrent, MaxAttempts: maxAttempts,
	})
	if err != nil {
		t.Fatal(err)
	}
	object, err := cluster.Get(context.Background(), specapi.RepositoryGVR, specapi.DefaultNamespace, "unseen")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return result, typed.(*spec.Repository)
}

func TestRunRaisesOneChangePerOpenContext(t *testing.T) {
	cluster, repository := setup(t, true, 4)
	result, after := run(t, cluster, repository, 4, 3)
	if result.Raised != 2 {
		t.Errorf("raised = %d, want one per context with no spec", result.Raised)
	}
	if result.Total != 3 || result.Summarized != 1 {
		t.Errorf("result = %+v", result)
	}
	if result.Phase != specapi.PhasePopulating {
		t.Errorf("phase = %q, want Populating", result.Phase)
	}
	if after.Status.Contexts == nil || after.Status.Contexts.Total != 3 || after.Status.Contexts.Summarized != 1 {
		t.Errorf("status.contexts = %+v", after.Status.Contexts)
	}

	listed, err := cluster.List(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("changes = %d, want two", len(listed.Items))
	}
	for index := range listed.Items {
		phase, _, _ := unstructured.NestedString(listed.Items[index].Object, "status", "phase")
		if phase != specapi.PhasePending {
			t.Errorf("change %s phase = %q, want Pending", listed.Items[index].GetName(), phase)
		}
		direction, _, _ := unstructured.NestedString(listed.Items[index].Object, "spec", "direction")
		if direction != specapi.DirectionCodeToSpec {
			t.Errorf("change direction = %q", direction)
		}
	}
}

func TestRunAdmitsAtMostMaxConcurrentSummaries(t *testing.T) {
	cluster, repository := setup(t, true, 1)
	result, _ := run(t, cluster, repository, 1, 3)
	if result.Raised != 1 {
		t.Errorf("raised = %d, want the admission limit", result.Raised)
	}
	if result.Phase != specapi.PhasePopulating {
		t.Errorf("phase = %q, want Populating while work is left", result.Phase)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	cluster, repository := setup(t, true, 4)
	run(t, cluster, repository, 4, 3)
	writes := cluster.writes
	result, _ := run(t, cluster, repository, 4, 3)
	if result.Raised != 0 {
		t.Errorf("raised = %d, want nothing: the changes are still unfinished", result.Raised)
	}
	if cluster.writes != writes {
		t.Errorf("the second run wrote status %d time(s)", cluster.writes-writes)
	}
}

func TestRunPopulatedWithoutSummarize(t *testing.T) {
	cluster, repository := setup(t, false, 4)
	result, after := run(t, cluster, repository, 4, 3)
	if result.Phase != specapi.PhasePopulated || result.Raised != 0 {
		t.Errorf("result = %+v", result)
	}
	if after.Status.Phase != specapi.PhasePopulated {
		t.Errorf("phase = %q", after.Status.Phase)
	}
	if got := conditionOf(after.Status.Conditions, specapi.ConditionPopulated); got == nil || got.Status != metav1.ConditionTrue {
		t.Errorf("Populated condition = %+v", got)
	}
}

func TestRunPopulatedWhenEveryContextHasASpec(t *testing.T) {
	cluster, repository := setup(t, true, 4)
	for _, name := range []string{"calc", "greet"} {
		store(t, cluster, &spec.SystemContext{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
			Spec:       spec.SystemContextSpec{Repository: "unseen", Upstream: spec.RefSelf, Intent: "written"},
			Status:     spec.SystemContextStatus{ObservedCommit: "c1", SyncedCommit: "c1"},
		})
	}
	result, _ := run(t, cluster, repository, 4, 3)
	if result.Phase != specapi.PhasePopulated || result.Summarized != 3 {
		t.Errorf("result = %+v", result)
	}
}

func TestRunFailedWhenTheAttemptCapIsReached(t *testing.T) {
	cluster, repository := setup(t, true, 4)
	store(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-c1-c1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c1"},
		Status:     spec.SpecChangeStatus{Phase: specapi.PhaseFailed},
	})
	store(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "greet-c2s-c1-c1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SpecChangeSpec{SystemContext: "greet", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c1"},
		Status:     spec.SpecChangeStatus{Phase: specapi.PhaseFailed},
	})
	result, after := run(t, cluster, repository, 4, 1)
	if result.Failed != 2 || result.Phase != specapi.PhaseFailed {
		t.Errorf("result = %+v", result)
	}
	if got := conditionOf(after.Status.Conditions, specapi.ConditionPopulated); got == nil || got.Reason != specapi.ReasonPopulateFailed {
		t.Errorf("Populated condition = %+v", got)
	}
}

func TestRunTreatsAPhaseLessChangeAsUnfinished(t *testing.T) {
	cluster, repository := setup(t, true, 4)
	store(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-c1-c1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c1"},
	})
	result, _ := run(t, cluster, repository, 4, 3)
	if result.Raised != 1 {
		t.Errorf("raised = %d, want only the context with no change at all", result.Raised)
	}
}

func TestRunRestoresTheIndexedCondition(t *testing.T) {
	cluster, repository := setup(t, true, 4)
	repository.Status.Conditions = []metav1.Condition{{
		Type: specapi.ConditionIndexed, Status: metav1.ConditionFalse,
		Reason: specapi.ReasonIndexFailed, Message: "a transient failure",
	}}
	store(t, cluster, repository)
	_, after := run(t, cluster, repository, 4, 3)
	condition := conditionOf(after.Status.Conditions, specapi.ConditionIndexed)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		t.Errorf("Indexed = %+v, want True once the index is current", condition)
	}
}

func TestRunIsAlreadyExistsTolerant(t *testing.T) {
	cluster, repository := setup(t, true, 4)
	store(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-c1-c1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionSpecToCode, ToSpecHash: strings.Repeat("a", 64)},
		Status:     spec.SpecChangeStatus{Phase: specapi.PhasePending},
	})
	result, _ := run(t, cluster, repository, 4, 3)
	if result.Raised != 2 {
		t.Errorf("raised = %d, want both contexts: an existing name of another direction is not this episode", result.Raised)
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
