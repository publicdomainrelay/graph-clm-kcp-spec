package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"strconv"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured

	writes int

	beforeUpdate func(f *fakeCluster)
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
}

func (f *fakeCluster) key(object *unstructured.Unstructured) string {
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

func (f *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	stored, ok := f.objects[f.key(object)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: object.GroupVersionKind().Group}, object.GetName())
	}
	updated := object.DeepCopy()
	updated.SetResourceVersion(stored.GetResourceVersion())
	updated.SetGeneration(stored.GetGeneration() + 1)
	f.objects[f.key(object)] = updated
	f.writes++
	return updated.DeepCopy(), nil
}

func (f *fakeCluster) Update(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if hook := f.beforeUpdate; hook != nil {
		f.beforeUpdate = nil
		hook(f)
	}
	stored, ok := f.objects[f.key(object)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: object.GroupVersionKind().Group}, object.GetName())
	}
	if object.GetResourceVersion() != stored.GetResourceVersion() {
		return nil, apierrors.NewConflict(schema.GroupResource{Group: object.GroupVersionKind().Group}, object.GetName(), errors.New("the object has been modified"))
	}
	updated := object.DeepCopy()
	updated.SetResourceVersion(nextVersion(stored.GetResourceVersion()))
	updated.SetGeneration(stored.GetGeneration() + 1)
	f.objects[f.key(object)] = updated
	f.writes++
	return updated.DeepCopy(), nil
}

func nextVersion(version string) string {
	number, _ := strconv.Atoi(version)
	return strconv.Itoa(number + 1)
}

func (f *fakeCluster) PatchStatus(_ context.Context, gvr schema.GroupVersionResource, _, name string, status map[string]any) (*unstructured.Unstructured, error) {
	for key, object := range f.objects {
		if object.GetName() == name && specapi.ResourceForKind(object.GetKind()) == gvr.Resource {
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
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
}

func setup(t *testing.T) (*fakeCluster, *spec.Repository) {
	t.Helper()
	dir := t.TempDir()
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: dir},
	}
	systemContext := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf},
		Status: spec.SystemContextStatus{
			ObservedCommit: "c2",
			Observed: spec.ObservedFacts{
				Files:       []string{"calc/calc.go"},
				Interfaces:  []spec.ObservedInterface{{Name: "Add", Kind: "function", CodegraphID: "function:add", File: "calc/calc.go"}},
				Fingerprint: "f2",
			},
			SyncedFingerprint: "f1",
			SyncedCommit:      "c1",
		},
	}
	cluster := newFakeCluster()
	for _, object := range []any{repository, systemContext} {
		spec.SetDefaults(object)
		stamped, err := kcpclient.Unstructured(object)
		if err != nil {
			t.Fatal(err)
		}
		cluster.objects[cluster.key(stamped)] = stamped
	}
	return cluster, repository
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

func scripted(t *testing.T, contents string) *scriptedagent.Agent {
	t.Helper()
	scenario, err := scriptedagent.LoadBytes([]byte(contents))
	if err != nil {
		t.Fatal(err)
	}
	return scriptedagent.New(scenario)
}

const oneRequirement = `
contexts:
  calc:
    summary: Arithmetic.
    intent: Arithmetic over two integers.
    requirements:
      - id: r.add
        level: MUST
        text: Add adds.
        codeRefs: ["function:add"]
    interfaces:
      - name: Add
        kind: function
`

func TestRunWritesTheSpecEndsTheDriftAndLeavesADocument(t *testing.T) {
	cluster, repository := setup(t)
	result, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "calc", Repository: repository, Agent: scripted(t, oneRequirement),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.SpecHash == "" {
		t.Fatalf("result = %+v", result)
	}

	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	systemContext, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	typed := systemContext.(*spec.SystemContext)
	if typed.Spec.Intent != "Arithmetic over two integers." || len(typed.Spec.Interfaces) != 1 {
		t.Errorf("spec = %+v", typed.Spec)
	}
	if typed.Annotations[specapi.OriginAnnotation] != specapi.OriginIngest {
		t.Errorf("annotations = %v", typed.Annotations)
	}
	if typed.Status.SyncedFingerprint != "f2" {
		t.Errorf("syncedFingerprint = %q, want the observed facts", typed.Status.SyncedFingerprint)
	}
	if typed.Status.RealizedSpecHash != result.SpecHash {
		t.Errorf("realizedSpecHash = %q, want %q", typed.Status.RealizedSpecHash, result.SpecHash)
	}

	document := contextDocPath("calc", "calc")
	contents, err := os.ReadFile(document)
	if err != nil {
		t.Fatalf("context document: %v", err)
	}
	if !strings.Contains(string(contents), "Arithmetic.") {
		t.Errorf("document = %q, want the summary", contents)
	}
}

func TestRunWritesNothingWhenTheSpecAlreadySaysThis(t *testing.T) {
	cluster, repository := setup(t)
	options := Options{Cluster: cluster, Context: "calc", Repository: repository, Agent: scripted(t, oneRequirement)}
	if _, err := Run(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	before := cluster.writes

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied {
		t.Error("a second run rewrote the spec")
	}
	if cluster.writes != before {
		t.Errorf("a second run wrote the cluster: %d -> %d", before, cluster.writes)
	}
}

func TestRunRefusesADraftTheValidatorWould(t *testing.T) {
	cluster, repository := setup(t)
	bad := scripted(t, "contexts:\n  calc:\n    intent: i\n    requirements:\n      - id: r\n        level: MUSTARD\n        text: t\n")
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "calc", Repository: repository, Agent: bad,
	}); err == nil {
		t.Fatal("a bad draft was written")
	}
}

func TestRunRefusesAMissingContext(t *testing.T) {
	cluster, repository := setup(t)
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "other", Repository: repository, Agent: scripted(t, oneRequirement),
	}); err == nil {
		t.Fatal("a missing context was summarized")
	}
}

func TestSummarizingTwiceLeavesTheDocumentByteIdentical(t *testing.T) {
	cluster, repository := setup(t)
	options := Options{Cluster: cluster, Context: "calc", Repository: repository, Agent: scripted(t, oneRequirement)}
	if _, err := Run(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	document := contextDocPath("calc", "calc")
	first, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Applied {
		t.Error("the second summarize rewrote the spec")
	}
	after, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, after) {
		t.Errorf("the document changed:\nbefore %q\nafter  %q", first, after)
	}
}

func TestTheWrittenDocumentParsesAsACLMModelZone(t *testing.T) {
	cluster, repository := setup(t)
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "calc", Repository: repository, Agent: scripted(t, oneRequirement),
	}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(contextDocPath("calc", "calc"))
	if err != nil {
		t.Fatal(err)
	}
	model, managed := agent.SplitContextDoc(string(contents))
	if managed == "" {
		t.Fatal("the document has no managed zone")
	}
	parsed, err := clm.ParseModelZone(model)
	if err != nil {
		t.Fatalf("the written document is not a CLM model zone: %v", err)
	}
	if len(parsed.Requirements) != 1 || parsed.Requirements[0].ID != "r.add" {
		t.Errorf("requirements = %+v", parsed.Requirements)
	}
	if len(parsed.Interfaces) != 1 || parsed.Interfaces[0].Name != "Add" {
		t.Errorf("interfaces = %+v", parsed.Interfaces)
	}
	if !strings.Contains(parsed.Intent, "Arithmetic.") {
		t.Errorf("intent = %q, want the model's prose", parsed.Intent)
	}
}

func TestRunReportsARequirementThatOnlyEnumeratesNames(t *testing.T) {
	cluster, repository := setup(t)
	scenario := scripted(t, `
contexts:
  calc:
    summary: Arithmetic.
    intent: Arithmetic over two integers.
    requirements:
      - id: r.surface
        level: MUST
        text: Add, Multiply, Subtract
`)
	result, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "calc", Repository: repository, Agent: scenario,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "r.surface") {
		t.Fatalf("warnings = %v, want one naming r.surface so the caller can report it", result.Warnings)
	}
}

func storedContext(t *testing.T, cluster *fakeCluster) *spec.SystemContext {
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

func storeContext(t *testing.T, cluster *fakeCluster, systemContext *spec.SystemContext) {
	t.Helper()
	systemContext.SetDefaults()
	stamped, err := kcpclient.Unstructured(systemContext)
	if err != nil {
		t.Fatal(err)
	}
	stamped.SetResourceVersion(nextVersion(systemContext.GetResourceVersion()))
	cluster.objects[cluster.key(stamped)] = stamped
}

func personRequirement() spec.Requirement {
	return spec.Requirement{ID: "r.divide", Level: spec.LevelMust, Text: "Divide by zero returns an error."}
}

func TestRunKeepsADeclaredRequirementAndProposesInstead(t *testing.T) {
	cluster, repository := setup(t)
	declared := storedContext(t, cluster)
	declared.Spec.Requirements = []spec.Requirement{personRequirement()}
	declared.Status.RealizedSpec = &spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf,
		Requirements: []spec.Requirement{personRequirement()}}
	declared.Annotations = map[string]string{specapi.DeclaredAnnotation: `["requirement/r.divide"]`}
	storeContext(t, cluster, declared)

	result, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "calc", Repository: repository, Agent: scripted(t, oneRequirement),
	})
	if err != nil {
		t.Fatal(err)
	}
	after := storedContext(t, cluster)
	ids := []string{}
	for _, requirement := range spec.CanonicalRequirements(after.Spec.Requirements) {
		ids = append(ids, requirement.ID)
	}
	if strings.Join(ids, ",") != "r.add,r.divide" {
		t.Errorf("requirements = %v, want the model's r.add next to the person's r.divide", ids)
	}
	if len(result.Proposals) != 1 || result.Proposals[0].Unit != "requirement/r.divide" || result.Proposals[0].Proposed != "" {
		t.Errorf("proposals = %+v, want the removal of r.divide proposed", result.Proposals)
	}
	hash, err := spec.HashSystemContextSpec(after.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status.RealizedSpecHash != hash || after.Annotations[specapi.OriginHashAnnotation] != hash {
		t.Errorf("a realized declared unit must not open a spec to code change: realized %q origin %q spec %q",
			after.Status.RealizedSpecHash, after.Annotations[specapi.OriginHashAnnotation], hash)
	}
}

func TestRunLeavesAPendingEditForTheRealize(t *testing.T) {
	cluster, repository := setup(t)
	pending := storedContext(t, cluster)
	pending.Spec.Requirements = []spec.Requirement{personRequirement()}
	pending.Status.RealizedSpec = &spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf}
	storeContext(t, cluster, pending)

	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "calc", Repository: repository, Agent: scripted(t, oneRequirement),
	}); err != nil {
		t.Fatal(err)
	}
	after := storedContext(t, cluster)
	hash, err := spec.HashSystemContextSpec(after.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status.RealizedSpecHash == hash {
		t.Error("the summary marked the person's unrealized edit as realized")
	}
	if _, stamped := after.Annotations[specapi.OriginHashAnnotation]; stamped {
		t.Error("the origin hash would hide the pending edit from the spec to code reconcile")
	}
	if after.Status.RealizedSpec == nil || len(after.Status.RealizedSpec.Requirements) != 1 ||
		after.Status.RealizedSpec.Requirements[0].ID != "r.add" {
		t.Errorf("realizedSpec = %+v, want the model's view of the code without r.divide", after.Status.RealizedSpec)
	}
	found := false
	for _, requirement := range after.Spec.Requirements {
		found = found || requirement.ID == "r.divide"
	}
	if !found {
		t.Error("the pending r.divide was dropped")
	}
}

func TestRunMergesAnEditThatLandsWhileTheModelWorks(t *testing.T) {
	cluster, repository := setup(t)
	cluster.beforeUpdate = func(f *fakeCluster) {
		edited := storedContext(t, f)
		edited.Spec.Requirements = append(edited.Spec.Requirements, personRequirement())
		storeContext(t, f, edited)
	}
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Context: "calc", Repository: repository, Agent: scripted(t, oneRequirement),
	}); err != nil {
		t.Fatal(err)
	}
	after := storedContext(t, cluster)
	ids := []string{}
	for _, requirement := range spec.CanonicalRequirements(after.Spec.Requirements) {
		ids = append(ids, requirement.ID)
	}
	if strings.Join(ids, ",") != "r.add,r.divide" {
		t.Errorf("requirements = %v, want the edit made during the run kept", ids)
	}
}
