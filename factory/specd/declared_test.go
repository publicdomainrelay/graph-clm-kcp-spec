package specd

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// movedOnCluster lands an operator's declaration on the stored context as the
// first write reaches it, so the write that was built from the read before it
// conflicts on the resource version and the declaration has to survive.
type movedOnCluster struct {
	*fakeCluster
	unit string
	once sync.Once
}

func (c *movedOnCluster) Update(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	c.once.Do(func() {
		key := objectKey(gvrOf(object), object.GetNamespace(), object.GetName())
		stored, ok := c.objects[key]
		if !ok {
			return
		}
		annotations := stored.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		declared, err := authority.Parse(annotations[specapi.DeclaredAnnotation])
		if err != nil {
			declared = authority.Set{}
		}
		annotations[specapi.DeclaredAnnotation] = authority.Format(declared.Union(authority.NewSet(c.unit)))
		stored.SetAnnotations(annotations)
		version, _ := strconv.Atoi(stored.GetResourceVersion())
		stored.SetResourceVersion(strconv.Itoa(version + 1))
	})
	return c.fakeCluster.Update(context.Background(), object)
}

// declaredContext is a context whose realized spec differs from its spec: the
// edit drops Subtract and takes up Divide, which is what the recorder sees.
func declaredContext(name string, declared ...string) *spec.SystemContext {
	realized := spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   spec.RefSelf,
		Interfaces: []spec.Interface{{Name: "Subtract", Kind: "function", Signature: "func Subtract(a, b int) int"}},
		Requirements: []spec.Requirement{
			{ID: "r.subtract", Level: spec.LevelMust, Text: "Subtract takes one integer away from another."},
			{ID: "r.gone", Level: spec.LevelMay, Text: "Gone is a unit the edit removes without a declaration."},
		},
	}
	edited := spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   spec.RefSelf,
		Interfaces: []spec.Interface{{Name: "Divide", Kind: "function", Signature: "func Divide(a, b int) int"}},
		Requirements: []spec.Requirement{{
			ID: "r.divide", Level: spec.LevelMust, Text: "Divide shares one integer out over another.",
		}},
	}
	annotations := map[string]string{}
	if len(declared) > 0 {
		annotations[specapi.DeclaredAnnotation] = authority.Format(authority.NewSet(declared...))
	}
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace, Annotations: annotations},
		Spec:       edited,
		Status:     spec.SystemContextStatus{RealizedSpec: &realized, RealizedSpecHash: strings.Repeat("a", 64)},
	}
}

func declaredUnits(t *testing.T, cluster Cluster, name string) []string {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := authority.Parse(object.GetAnnotations()[specapi.DeclaredAnnotation])
	if err != nil {
		t.Fatal(err)
	}
	return declared.Sorted()
}

func assertDeclared(t *testing.T, got []string, want ...string) {
	t.Helper()
	wantSorted := authority.NewSet(want...).Sorted()
	if strings.Join(got, ",") != strings.Join(wantSorted, ",") {
		t.Errorf("declared = %v, want %v", got, wantSorted)
	}
}

// TestADeclarationLandedWhileTheEditIsRecordedIsKept is the race the recorder
// guards against: an operator declares a unit while the edit is being recorded,
// so the write built from an earlier read conflicts and the union is written.
func TestADeclarationLandedWhileTheEditIsRecordedIsKept(t *testing.T) {
	cluster := &movedOnCluster{fakeCluster: newFakeCluster(), unit: authority.RequirementUnit("r.operator")}
	apply(t, cluster.fakeCluster, declaredContext("calc"))
	controller := testController(cluster)

	if err := controller.recordDeclared(context.Background(), specapi.DefaultNamespace, declaredContext("calc")); err != nil {
		t.Fatal(err)
	}

	assertDeclared(t, declaredUnits(t, cluster, "calc"),
		"interface/Divide", "requirement/r.divide", "requirement/r.operator")
}

// TestRecordingAnEditUnionsWhatWasDeclaredBeforeIt is plan 0013's second edit:
// a unit an earlier declaration recorded and this edit removes stays declared,
// so the removal guard lets the change run, while a unit this edit removes
// without any declaration is not declared by its own removal.
func TestRecordingAnEditUnionsWhatWasDeclaredBeforeIt(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, declaredContext("calc", "interface/Subtract", "requirement/r.subtract", "requirement/r.earlier"))
	controller := testController(cluster)

	if err := controller.recordDeclared(context.Background(), specapi.DefaultNamespace, declaredContext("calc")); err != nil {
		t.Fatal(err)
	}

	assertDeclared(t, declaredUnits(t, cluster, "calc"),
		"interface/Divide", "requirement/r.divide", "requirement/r.earlier",
		"interface/Subtract", "requirement/r.subtract")
}

// TestRecordingOneContextLeavesAnotherAlone writes the annotation of the edited
// context only.
func TestRecordingOneContextLeavesAnotherAlone(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, declaredContext("calc"))
	apply(t, cluster, declaredContext("cmd-calc", "requirement/r.cli"))
	controller := testController(cluster)

	if err := controller.recordDeclared(context.Background(), specapi.DefaultNamespace, declaredContext("calc")); err != nil {
		t.Fatal(err)
	}

	assertDeclared(t, declaredUnits(t, cluster, "cmd-calc"), "requirement/r.cli")
	assertDeclared(t, declaredUnits(t, cluster, "calc"), "interface/Divide", "requirement/r.divide")
}
