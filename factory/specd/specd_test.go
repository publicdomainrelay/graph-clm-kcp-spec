package specd

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
}

func objectKey(gvr schema.GroupVersionResource, namespace, name string) string {
	return gvr.Resource + "/" + namespace + "/" + name
}

func (f *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	stored, ok := f.objects[objectKey(gvr, namespace, name)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
	}
	return stored.DeepCopy(), nil
}

func (f *fakeCluster) List(_ context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	for key, object := range f.objects {
		if len(key) > len(gvr.Resource)+1 && key[:len(gvr.Resource)+1] == gvr.Resource+"/" &&
			object.GetNamespace() == namespace {
			list.Items = append(list.Items, *object.DeepCopy())
		}
	}
	sort.Slice(list.Items, func(left, right int) bool { return list.Items[left].GetName() < list.Items[right].GetName() })
	return list, nil
}

func (f *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	key := objectKey(gvrOf(object), object.GetNamespace(), object.GetName())
	stored, ok := f.objects[key]
	if !ok {
		return f.Create(context.Background(), object)
	}
	updated := object.DeepCopy()
	updated.SetResourceVersion(stored.GetResourceVersion())
	if !specEqual(stored, updated) {
		updated.SetGeneration(stored.GetGeneration() + 1)
	} else {
		updated.SetGeneration(stored.GetGeneration())
	}
	f.objects[key] = updated
	return updated.DeepCopy(), nil
}

func (f *fakeCluster) Create(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	key := objectKey(gvrOf(object), object.GetNamespace(), object.GetName())
	if _, ok := f.objects[key]; ok {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: gvrOf(object).Group}, object.GetName())
	}
	created := object.DeepCopy()
	created.SetResourceVersion("1")
	created.SetGeneration(1)
	createdAt := created.GetCreationTimestamp()
	if createdAt.IsZero() {
		created.SetCreationTimestamp(metav1.Now())
	}
	f.objects[key] = created
	return created.DeepCopy(), nil
}

func (f *fakeCluster) PatchStatus(_ context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error) {
	key := objectKey(gvr, namespace, name)
	stored, ok := f.objects[key]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
	}
	updated := stored.DeepCopy()
	normalized, ok := normalize(status).(map[string]any)
	if !ok {
		return nil, apierrors.NewInternalError(errors.New("status is not an object"))
	}
	// The real client sends a merge patch, so a key the caller did not name
	// keeps its value. The fake has to merge too, or a reconcile would look
	// like it dropped the observed facts the previous ingest wrote.
	current, _, err := unstructured.NestedMap(updated.Object, "status")
	if err != nil {
		return nil, err
	}
	for key, value := range normalized {
		current[key] = value
	}
	if err := unstructured.SetNestedMap(updated.Object, current, "status"); err != nil {
		return nil, err
	}
	version, _ := strconv.Atoi(stored.GetResourceVersion())
	updated.SetResourceVersion(strconv.Itoa(version + 1))
	f.objects[key] = updated
	return updated.DeepCopy(), nil
}

func (f *fakeCluster) names(gvr schema.GroupVersionResource) []string {
	out := []string{}
	for key := range f.objects {
		if len(key) > len(gvr.Resource) && key[:len(gvr.Resource)] == gvr.Resource {
			stored := f.objects[key]
			out = append(out, stored.GetName())
		}
	}
	sort.Strings(out)
	return out
}

func gvrOf(object *unstructured.Unstructured) schema.GroupVersionResource {
	gvr, err := specapi.GVRForKind(object.GetKind())
	if err != nil {
		return schema.GroupVersionResource{}
	}
	return gvr
}

func specEqual(left, right *unstructured.Unstructured) bool {
	leftSpec, _, _ := unstructured.NestedMap(left.Object, "spec")
	rightSpec, _, _ := unstructured.NestedMap(right.Object, "spec")
	return reflect.DeepEqual(normalize(leftSpec), normalize(rightSpec))
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

func testController(cluster Cluster) *Controller {
	return &Controller{
		opts:   Options{Namespace: specapi.DefaultNamespace, Resync: time.Second},
		client: cluster,
		queue:  workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[key]()),
		log:    logging.Discard(),
	}
}

func apply(t *testing.T, cluster Cluster, object any) *unstructured.Unstructured {
	t.Helper()
	spec.SetDefaults(object)
	stamped, err := kcpclient.Unstructured(object)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := cluster.Apply(context.Background(), stamped)
	if err != nil {
		t.Fatal(err)
	}
	return applied
}

func systemContext(name string, mutate func(*spec.SystemContext)) *spec.SystemContext {
	systemContext := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   spec.RefSelf,
			Interfaces: []spec.Interface{{Name: "Add", Kind: "function"}},
		},
	}
	mutate(systemContext)
	return systemContext
}

func observedFacts(fingerprint string) spec.ObservedFacts {
	return spec.ObservedFacts{
		Files:       []string{"calc/calc.go"},
		Interfaces:  []spec.ObservedInterface{{Name: "Add", Kind: "function", CodegraphID: "function:add", File: "calc/calc.go"}},
		Fingerprint: fingerprint,
	}
}

func conditionStatus(t *testing.T, object *unstructured.Unstructured, conditionType string) string {
	t.Helper()
	conditions, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil || !found {
		t.Fatalf("no conditions on %s", object.GetName())
	}
	for _, entry := range conditions {
		condition, ok := entry.(map[string]any)
		if ok && condition["type"] == conditionType {
			status, _ := condition["status"].(string)
			return status
		}
	}
	t.Fatalf("%s has no %s condition: %+v", object.GetName(), conditionType, conditions)
	return ""
}

func TestSystemContextReconcileWritesConditionsAndTheGeneration(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Status.Observed = observedFacts("f1")
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.ObservedCommit = "c1"
		systemContext.Status.SyncedCommit = "c1"
	}))
	controller := testController(cluster)

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}

	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if got := conditionStatus(t, object, specapi.ConditionSpecValid); got != "True" {
		t.Errorf("SpecValid = %s, want True", got)
	}
	if got := conditionStatus(t, object, specapi.ConditionCodeSynced); got != "True" {
		t.Errorf("CodeSynced = %s, want True", got)
	}
	if got := conditionStatus(t, object, specapi.ConditionDrifted); got != "False" {
		t.Errorf("Drifted = %s, want False", got)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	readBack, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	if readBack.Status.ObservedGeneration != object.GetGeneration() {
		t.Errorf("observedGeneration = %d, want %d", readBack.Status.ObservedGeneration, object.GetGeneration())
	}
}

func TestSystemContextReconcileRaisesOneCodeToSpecOnDrift(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Status.Observed = observedFacts("f2")
		systemContext.Status.ObservedCommit = "c2"
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.SyncedCommit = "c1"
		systemContext.Status.Conditions = []metav1.Condition{{
			Type: specapi.ConditionDrifted, Status: metav1.ConditionFalse, Reason: specapi.ReasonFingerprintEqual,
		}}
	}))
	controller := testController(cluster)

	for round := 0; round < 2; round++ {
		if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
			t.Fatal(err)
		}
	}

	names := cluster.names(specapi.SpecChangeGVR)
	if len(names) != 1 || names[0] != "calc-c2s-c1-c2" {
		t.Fatalf("spec changes = %v, want one calc-c2s-c1-c2", names)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-c2s-c1-c2")
	if err != nil {
		t.Fatal(err)
	}
	change, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	typed, ok := change.(*spec.SpecChange)
	if !ok {
		t.Fatalf("read back a %T", change)
	}
	if typed.Spec.Direction != specapi.DirectionCodeToSpec || typed.Spec.FromCommit != "c1" || typed.Spec.ToCommit != "c2" {
		t.Errorf("change = %+v", typed.Spec)
	}
	if typed.Status.Phase != specapi.PhasePending {
		t.Errorf("phase = %q, want Pending", typed.Status.Phase)
	}
}

func TestSystemContextReconcileRaisesOneSpecToCodeOnAHumanEdit(t *testing.T) {
	cluster := newFakeCluster()
	realized, err := spec.HashSystemContextSpec(spec.SystemContextSpec{
		Repository: "calc", Upstream: spec.RefSelf, Intent: "as it was",
		Interfaces: []spec.Interface{{Name: "Add", Kind: "function"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Intent = "as a human left it"
		systemContext.Status.Observed = observedFacts("f1")
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.RealizedSpecHash = realized
	}))
	controller := testController(cluster)

	for round := 0; round < 2; round++ {
		if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
			t.Fatal(err)
		}
	}

	names := cluster.names(specapi.SpecChangeGVR)
	if len(names) != 1 {
		t.Fatalf("spec changes = %v, want exactly one", names)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, names[0])
	if err != nil {
		t.Fatal(err)
	}
	toSpecHash, _, _ := unstructured.NestedString(object.Object, "spec", "toSpecHash")
	fromSpecHash, _, _ := unstructured.NestedString(object.Object, "spec", "fromSpecHash")
	if toSpecHash == "" || fromSpecHash != realized {
		t.Errorf("hashes = %q -> %q, want %q -> a new hash", fromSpecHash, toSpecHash, realized)
	}
	if spec.ChangeNameSpecToCode("calc", toSpecHash) != names[0] {
		t.Errorf("name %q is not the deterministic name of the hash", names[0])
	}
	if direction, _, _ := unstructured.NestedString(object.Object, "spec", "direction"); direction != specapi.DirectionSpecToCode {
		t.Errorf("direction = %q", direction)
	}
}

func TestSystemContextReconcileIsQuietWhenSyncedAndRealized(t *testing.T) {
	cluster := newFakeCluster()
	systemContextObject := systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Status.Observed = observedFacts("f1")
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.ObservedCommit = "c1"
		systemContext.Status.SyncedCommit = "c1"
	})
	realized, err := spec.HashSystemContextSpec(systemContextObject.Spec)
	if err != nil {
		t.Fatal(err)
	}
	systemContextObject.Status.RealizedSpecHash = realized
	apply(t, cluster, systemContextObject)

	controller := testController(cluster)
	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	settled, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	before := settled.GetResourceVersion()

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}

	if names := cluster.names(specapi.SpecChangeGVR); len(names) != 0 {
		t.Errorf("spec changes = %v, want none", names)
	}
	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if object.GetResourceVersion() != before {
		t.Errorf("a quiet reconcile wrote status: %s -> %s", before, object.GetResourceVersion())
	}
}

func TestSystemContextReconcileWaitsForAnUnfinishedChange(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Status.Observed = observedFacts("f2")
		systemContext.Status.ObservedCommit = "c2"
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.SyncedCommit = "c1"
	}))
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-c0-c1", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c0", ToCommit: "c1",
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	})
	controller := testController(cluster)

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	names := cluster.names(specapi.SpecChangeGVR)
	if len(names) != 1 || names[0] != "calc-c2s-c0-c1" {
		t.Errorf("spec changes = %v, want the pending one only", names)
	}
}

func TestSpecChangeReconcileMovesAnEmptyPhaseToPending(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-abc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionSpecToCode, ToSpecHash: hashOf(t, "calc"),
		},
	})
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-abc"); err != nil {
		t.Fatal(err)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-abc")
	if err != nil {
		t.Fatal(err)
	}
	if phase, _, _ := unstructured.NestedString(object.Object, "status", "phase"); phase != specapi.PhasePending {
		t.Errorf("phase = %q, want Pending", phase)
	}
}

func TestSpecChangeReconcileAdmitsOneRunningChangePerContext(t *testing.T) {
	cluster := newFakeCluster()
	for _, name := range []string{"calc-c2s-a", "calc-c2s-b"} {
		apply(t, cluster, &spec.SpecChange{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
			Spec: spec.SpecChangeSpec{
				SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c0", ToCommit: "c1",
			},
			Status: spec.SpecChangeStatus{Phase: specapi.PhaseRunning},
		})
	}
	controller := testController(cluster)

	for _, name := range []string{"calc-c2s-a", "calc-c2s-b"} {
		if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, name); err != nil {
			t.Fatal(err)
		}
	}

	phases := map[string]string{}
	for _, name := range []string{"calc-c2s-a", "calc-c2s-b"} {
		object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, name)
		if err != nil {
			t.Fatal(err)
		}
		phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
		phases[name] = phase
	}
	if phases["calc-c2s-a"] != specapi.PhaseRunning || phases["calc-c2s-b"] != specapi.PhaseFailed {
		t.Errorf("phases = %v, want a running and b failed", phases)
	}
}

func hashOf(t *testing.T, intent string) string {
	t.Helper()
	hash, err := spec.HashSystemContextSpec(spec.SystemContextSpec{Repository: "calc", Intent: intent})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestSystemContextReconcileRetriesAFailedChange(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Status.Observed = observedFacts("f2")
		systemContext.Status.ObservedCommit = "c2"
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.SyncedCommit = "c1"
	}))
	// The first attempt exists and failed, so the drift is still work.
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-c1-c2", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "c1", ToCommit: "c2",
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseFailed},
	})
	controller := testController(cluster)

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}

	names := cluster.names(specapi.SpecChangeGVR)
	if strings.Join(names, ",") != "calc-c2s-c1-c2,calc-c2s-c1-c2-a2" {
		t.Fatalf("spec changes = %v, want the failed attempt and a second one", names)
	}
	attempt, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-c2s-c1-c2-a2")
	if err != nil {
		t.Fatal(err)
	}
	if phase, _, _ := unstructured.NestedString(attempt.Object, "status", "phase"); phase != specapi.PhasePending {
		t.Errorf("the retry is %q, want Pending", phase)
	}
	// The failed attempt keeps its record.
	failed, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-c2s-c1-c2")
	if err != nil {
		t.Fatal(err)
	}
	if phase, _, _ := unstructured.NestedString(failed.Object, "status", "phase"); phase != specapi.PhaseFailed {
		t.Errorf("the first attempt is %q, want Failed", phase)
	}
}

func TestSystemContextReconcileLeavesACleanIndexedRepositoryAlone(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: "/does/not/matter"},
		Status: spec.RepositoryStatus{
			HeadCommit:    "abc",
			IndexedCommit: "abc",
			Conditions: []metav1.Condition{{
				Type: specapi.ConditionIndexed, Status: metav1.ConditionTrue,
				Reason: specapi.ReasonIndexed, Message: "the codegraph index is current",
				ObservedGeneration: 1,
			}},
		},
	})
	controller := testController(cluster)
	object, err := cluster.Get(context.Background(), specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	before := object.GetResourceVersion()
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	if err := controller.setRepositoryCondition(context.Background(), repository, specapi.DefaultNamespace,
		metav1.ConditionTrue, specapi.ReasonIndexed, "the codegraph index is current"); err != nil {
		t.Fatal(err)
	}
	after, err := cluster.Get(context.Background(), specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if after.GetResourceVersion() != before {
		t.Errorf("an already true condition was rewritten: %s -> %s", before, after.GetResourceVersion())
	}

	// The same call has to bring a condition left False back to True.
	stale := *repository
	stale.ObjectMeta = *repository.ObjectMeta.DeepCopy()
	stale.Status.Conditions = []metav1.Condition{{
		Type: specapi.ConditionIndexed, Status: metav1.ConditionFalse,
		Reason: specapi.ReasonHeadUnavailable, Message: "the path did not exist yet",
		ObservedGeneration: 1,
	}}
	if err := controller.setRepositoryCondition(context.Background(), &stale, specapi.DefaultNamespace,
		metav1.ConditionTrue, specapi.ReasonIndexed, "the codegraph index is current"); err != nil {
		t.Fatal(err)
	}
	recovered, err := cluster.Get(context.Background(), specapi.RepositoryGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	if got := repositoryConditionStatus(t, recovered, specapi.ConditionIndexed); got != "True" {
		t.Errorf("Indexed = %s, want True", got)
	}
}

func repositoryConditionStatus(t *testing.T, object *unstructured.Unstructured, conditionType string) string {
	t.Helper()
	conditions, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil || !found {
		t.Fatalf("no conditions on %s", object.GetName())
	}
	for _, entry := range conditions {
		condition, ok := entry.(map[string]any)
		if ok && condition["type"] == conditionType {
			status, _ := condition["status"].(string)
			return status
		}
	}
	t.Fatalf("%s has no %s condition", object.GetName(), conditionType)
	return ""
}

// TestSpecToCodeChangeCarriesTheDelta is the phase 6 contract for the spec ->
// code direction: the change carries what changed, computed against the spec
// that was last realized, never the whole spec.
func TestSpecToCodeChangeCarriesTheDelta(t *testing.T) {
	cluster := newFakeCluster()
	realized := spec.SystemContextSpec{
		Repository:   "calc",
		Upstream:     spec.RefSelf,
		Intent:       "Arithmetic on two integers.",
		Requirements: []spec.Requirement{{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum."}},
		Interfaces:   []spec.Interface{{Name: "Add", Kind: "function"}},
		CodeRefs:     []string{"file:calc/calc.go"},
	}
	applied := apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec = realized
		systemContext.Spec.Interfaces = append(systemContext.Spec.Interfaces,
			spec.Interface{Name: "Subtract", Kind: "function", Signature: "func Subtract(a, b int) int"})
		systemContext.Spec.Requirements = append(systemContext.Spec.Requirements,
			spec.Requirement{ID: "r.subtract", Level: spec.LevelShould, Text: "Subtract returns the difference."})
		hash, err := spec.HashSystemContextSpec(realized)
		if err != nil {
			t.Fatal(err)
		}
		systemContext.Status.Observed = observedFacts("f1")
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.RealizedSpecHash = hash
		systemContext.Status.RealizedSpec = &realized
	}))
	controller := testController(cluster)

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	names := cluster.names(specapi.SpecChangeGVR)
	if len(names) != 1 {
		t.Fatalf("spec changes = %v, want one", names)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, names[0])
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change := typed.(*spec.SpecChange)
	if change.Spec.Direction != specapi.DirectionSpecToCode {
		t.Fatalf("direction = %q", change.Spec.Direction)
	}
	if change.Spec.Delta == nil {
		t.Fatal("the change carries no delta")
	}
	counts := change.Spec.Delta.Count()
	if counts.Added != 2 || counts.Removed != 0 || counts.Changed != 0 {
		t.Errorf("delta = %+v, want exactly the two added entries", counts)
	}
	if len(change.Spec.Delta.Interfaces) != 1 || change.Spec.Delta.Interfaces[0].Name != "Subtract" {
		t.Errorf("interfaces = %+v", change.Spec.Delta.Interfaces)
	}
	if len(change.Spec.Delta.Requirements) != 1 || change.Spec.Delta.Requirements[0].ID != "r.subtract" {
		t.Errorf("requirements = %+v", change.Spec.Delta.Requirements)
	}
	if change.Spec.FromSpecHash != applied.GetAnnotations()[specapi.OriginHashAnnotation] &&
		change.Spec.FromSpecHash != realizedHashOf(t, realized) {
		t.Errorf("fromSpecHash = %q", change.Spec.FromSpecHash)
	}
}

// TestCodeToSpecChangeCarriesTheObservedDelta is the other direction: the
// change says which facts the code gained since the synced baseline.
func TestCodeToSpecChangeCarriesTheObservedDelta(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Status.Observed = observedFacts("f2")
		systemContext.Status.ObservedCommit = "c2"
		systemContext.Status.SyncedObserved = observedFacts("f1")
		systemContext.Status.SyncedFingerprint = "f1"
		systemContext.Status.SyncedCommit = "c1"
	}))
	controller := testController(cluster)

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	names := cluster.names(specapi.SpecChangeGVR)
	if len(names) != 1 {
		t.Fatalf("spec changes = %v, want one", names)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, names[0])
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change := typed.(*spec.SpecChange)
	if change.Spec.Direction != specapi.DirectionCodeToSpec {
		t.Fatalf("direction = %q", change.Spec.Direction)
	}
	if change.Spec.Delta == nil || change.Spec.Delta.Observed == nil {
		t.Fatal("the change carries no observed delta")
	}
	if change.Spec.Delta.Observed.Fingerprint == nil {
		t.Error("the fingerprint change is missing")
	}
}

func realizedHashOf(t *testing.T, specification spec.SystemContextSpec) string {
	t.Helper()
	hash, err := spec.HashSystemContextSpec(specification)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestRealizeBranchAndWorktreeAreNamedAfterTheChange(t *testing.T) {
	if got := realizeBranch("calc", "0123456789abcdef"); got != "spec/calc/01234567" {
		t.Errorf("branch = %q", got)
	}
	if got := realizeBranch("calc", "abc"); got != "spec/calc/abc" {
		t.Errorf("branch = %q", got)
	}
	first, err := worktreeDir("calc-s2c-abc")
	if err != nil {
		t.Fatal(err)
	}
	second, err := worktreeDir("calc-s2c-abc")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("two attempts share one worktree directory")
	}
	if !strings.Contains(first, "calc-s2c-abc") {
		t.Errorf("worktree = %q", first)
	}
}

func TestRetryInstructionFeedsTheFailedVerifyBack(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Status.RealizedSpecHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}))
	base := spec.ChangeNameSpecToCode("calc", strings.Repeat("b", 64))
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: base, Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionSpecToCode,
			ToSpecHash: strings.Repeat("b", 64),
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseFailed, AgentLog: "Subtract(5, 3) = 8, want 2"},
	})
	controller := testController(cluster)

	change := &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: base, Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionSpecToCode,
			ToSpecHash: strings.Repeat("b", 64),
		},
	}
	instruction := controller.retryInstruction(context.Background(), specapi.DefaultNamespace, change)
	if !strings.Contains(instruction, "Subtract(5, 3) = 8, want 2") {
		t.Errorf("instruction = %q, want the failed verify output", instruction)
	}
	change.Spec.ToSpecHash = strings.Repeat("c", 64)
	if got := controller.retryInstruction(context.Background(), specapi.DefaultNamespace, change); got != "" {
		t.Errorf("a fresh episode got the instruction %q", got)
	}
}

func TestSpecToCodeIsLeftForAHumanWithoutAnAgent(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {}))
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: t.TempDir()},
	})
	apply(t, cluster, &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-abc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionSpecToCode,
			ToSpecHash: strings.Repeat("d", 64),
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	})
	controller := testController(cluster)

	if _, err := controller.reconcileSpecChange(context.Background(), specapi.DefaultNamespace, "calc-s2c-abc"); err != nil {
		t.Fatal(err)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-abc")
	if err != nil {
		t.Fatal(err)
	}
	if phase, _, _ := unstructured.NestedString(object.Object, "status", "phase"); phase != specapi.PhasePending {
		t.Errorf("phase = %q, want Pending: no agent is configured", phase)
	}
}

// An origin: clm spec edit is a real edit, so it raises a SpecToCode change —
// exactly like a human's. The exception is the one phase 8 needs: while the
// context's own change is Running, a model refining the spec it is realizing
// must not spawn a second change for itself. The controller already folds it
// (the unfinished rule below), and the change's status.progress carries the
// record; this pins the rule so the fold cannot regress.
func TestACLMSpecEditWhileAChangeRunsRaisesNoSecondChange(t *testing.T) {
	edit := func() *spec.SystemContext {
		return systemContext("calc", func(systemContext *spec.SystemContext) {
			systemContext.Status.RealizedSpecHash = strings.Repeat("a", 64)
			systemContext.Status.RealizedSpec = &spec.SystemContextSpec{
				Repository: "calc",
				Upstream:   spec.RefSelf,
			}
			systemContext.Annotations = map[string]string{specapi.OriginAnnotation: specapi.OriginCLM}
		})
	}
	running := func() *spec.SpecChange {
		return &spec.SpecChange{
			ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-b", Namespace: specapi.DefaultNamespace},
			Spec: spec.SpecChangeSpec{
				SystemContext: "calc",
				Direction:     specapi.DirectionSpecToCode,
				ToSpecHash:    strings.Repeat("b", 64),
			},
			Status: spec.SpecChangeStatus{Phase: specapi.PhaseRunning},
		}
	}

	idle := newFakeCluster()
	apply(t, idle, edit())
	if _, err := testController(idle).reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	raised := idle.names(specapi.SpecChangeGVR)
	if len(raised) != 1 {
		t.Fatalf("an idle context raised %v, want one spec to code change", raised)
	}
	object, err := idle.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, raised[0])
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	change, ok := typed.(*spec.SpecChange)
	if !ok {
		t.Fatalf("%s is not a SpecChange", raised[0])
	}
	if change.Spec.Delta == nil || change.Spec.Delta.Empty() {
		t.Error("the clm edit raised a change with no delta")
	}

	busy := newFakeCluster()
	apply(t, busy, edit())
	apply(t, busy, running())
	if _, err := testController(busy).reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	if after := busy.names(specapi.SpecChangeGVR); len(after) != 1 {
		t.Errorf("changes = %v, want only the running one: the clm edit folded into it", after)
	}
}
