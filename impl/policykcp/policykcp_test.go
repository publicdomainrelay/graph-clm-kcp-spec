package policykcp

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

const testRego = `package nodirectguestconnect

import data.lib.specd

violation[specd.violation(msg, specd.location(file.path, 1))] {
	file := specd.files_matching(input.parameters.globs)[_]
	re_match(input.parameters.pattern, specd.code_graph.spec.texts[file.path])
	msg := sprintf("no direct guest connect: %s", [file.path])
}`

func testTemplate() policy.Template {
	return policy.Template{
		Name: "nodirectguestconnect",
		Slug: "no-direct-guest-connect",
		Kind: "NoDirectGuestConnect",
		Annotations: map[string]string{
			policy.AnnotationTitle: "no direct guest connect",
			policy.AnnotationLevel: "MUST",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"globs":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"pattern": map[string]any{"type": "string"},
			},
		},
		Rego: testRego,
		Libs: []string{policyeval.Lib()},
	}
}

func TestTemplateObjectRoundTrips(t *testing.T) {
	object, err := TemplateObject(testTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if object.GetKind() != policy.ConstraintTemplateKind {
		t.Fatalf("kind = %s", object.GetKind())
	}
	parsed, err := ParseTemplateObject(object)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "nodirectguestconnect" || parsed.Slug != "no-direct-guest-connect" {
		t.Errorf("name/slug = %s/%s", parsed.Name, parsed.Slug)
	}
	if parsed.Kind != "NoDirectGuestConnect" {
		t.Errorf("kind = %s", parsed.Kind)
	}
	if parsed.Rego != testRego {
		t.Errorf("rego did not round trip")
	}
	if parsed.Level != policy.LevelMust {
		t.Errorf("level = %s", parsed.Level)
	}
	if len(parsed.Libs) != 1 {
		t.Errorf("libs = %v", parsed.Libs)
	}
	if parsed.Parameters["type"] != "object" {
		t.Errorf("parameters = %v", parsed.Parameters)
	}
}

func TestConstraintObjectRoundTrips(t *testing.T) {
	constraint := policy.Constraint{
		Name:        "no-direct-guest-connect",
		Kind:        "NoDirectGuestConnect",
		Match:       policy.Match{Kinds: []policy.MatchKind{{APIGroups: []string{policy.Group}, Kinds: []string{policy.CodeGraphKind}}}},
		Parameters:  map[string]any{"globs": []any{"test/**"}, "pattern": `Deno\.connect`},
		Enforcement: policy.EnforcementDeny,
	}
	object, err := ConstraintObject(constraint)
	if err != nil {
		t.Fatal(err)
	}
	if object.GetAPIVersion() != policy.ConstraintAPIVersion {
		t.Fatalf("apiVersion = %s", object.GetAPIVersion())
	}
	parsed, err := ParseConstraintObject(object, "nodirectguestconnect")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != constraint.Name || parsed.Enforcement != policy.EnforcementDeny {
		t.Errorf("parsed = %+v", parsed)
	}
	if parsed.Template != "nodirectguestconnect" {
		t.Errorf("template = %s", parsed.Template)
	}
}

func TestFilesAndStaleCoverTemplatesAndConstraints(t *testing.T) {
	library := policy.Library{
		Manifest:    policy.PolicyLibrary{Repository: "market-mini"},
		Templates:   []policy.Template{testTemplate()},
		Constraints: []policy.Constraint{{Name: "no-direct-guest-connect", Kind: "NoDirectGuestConnect", Enforcement: policy.EnforcementDeny}},
	}
	files, err := Files(library)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"templates/no-direct-guest-connect/src.rego",
		"templates/no-direct-guest-connect/template.yaml",
		"constraints/no-direct-guest-connect.yaml",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s missing from %v", name, files)
		}
	}
	existing := map[string][]byte{
		"templates/no-direct-guest-connect/src.rego": []byte("old"),
		"templates/gone/src.rego":                    []byte("old"),
		"constraints/gone.yaml":                      []byte("old"),
		"dist/gone.yaml":                             []byte("old"),
		"tests/no-direct-guest-connect/suite.yaml":   []byte("keep me"),
		"reports/main.yaml":                          []byte("keep me"),
	}
	stale := Stale(existing, library)
	want := map[string]bool{"templates/gone/src.rego": true, "constraints/gone.yaml": true, "dist/gone.yaml": true}
	if len(stale) != len(want) {
		t.Fatalf("stale = %v", stale)
	}
	for _, name := range stale {
		if !want[name] {
			t.Errorf("%s is not stale", name)
		}
	}
}

func TestDistinctComparesContentNotCommits(t *testing.T) {
	library := policy.Library{Templates: []policy.Template{testTemplate()}}
	same := policy.Library{Templates: []policy.Template{testTemplate()}}
	if Distinct(library, same) {
		t.Error("two libraries with the same template differ")
	}
	changed := policy.Library{Templates: []policy.Template{testTemplate()}}
	changed.Templates[0].Rego = testRego + "\n"
	if !Distinct(library, changed) {
		t.Error("a changed rule is not distinct")
	}
}

func TestConstraintCRDUsesGatekeepersNaming(t *testing.T) {
	ctx := context.Background()
	library := policy.Library{Templates: []policy.Template{testTemplate()}, Lib: policyeval.Lib()}
	engine, err := policyeval.NewEngine(ctx, library, nil)
	if err != nil {
		t.Fatal(err)
	}
	crd, err := engine.ConstraintCRD(ctx, testTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if crd.GetKind() != "CustomResourceDefinition" {
		t.Fatalf("kind = %s", crd.GetKind())
	}
	if crd.GetName() != "nodirectguestconnect.constraints.gatekeeper.sh" {
		t.Errorf("name = %s", crd.GetName())
	}
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	found := false
	for _, raw := range versions {
		version, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if version["name"] == "v1beta1" && version["served"] == true {
			found = true
		}
	}
	if !found {
		t.Errorf("versions = %v", versions)
	}
	entry, ok := versions[0].(map[string]any)
	if !ok {
		t.Fatalf("versions = %v", versions)
	}
	schema, _ := entry["schema"].(map[string]any)
	openAPI, _ := schema["openAPIV3Schema"].(map[string]any)
	properties, _ := openAPI["properties"].(map[string]any)
	spec, _ := properties["spec"].(map[string]any)
	specProperties, _ := spec["properties"].(map[string]any)
	if _, ok := specProperties["parameters"]; !ok {
		t.Errorf("the constraint CRD carries no spec.parameters schema: %v", specProperties)
	}
}

type fakeCluster struct {
	client *dynamicfake.FakeDynamicClient
}

func (f fakeCluster) ListCluster(ctx context.Context, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	return f.client.Resource(gvr).List(ctx, metav1.ListOptions{})
}

func (f fakeCluster) GetCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error) {
	return f.client.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
}

func (f fakeCluster) CreateCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return f.client.Resource(resourceGVR(object)).Create(ctx, object, metav1.CreateOptions{})
}

func (f fakeCluster) ApplyCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	current, err := f.GetCluster(ctx, resourceGVR(object), object.GetName())
	if err == nil {
		object.SetResourceVersion(current.GetResourceVersion())
		return f.client.Resource(resourceGVR(object)).Update(ctx, object, metav1.UpdateOptions{})
	}
	return f.CreateCluster(ctx, object)
}

func (f fakeCluster) DeleteCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) error {
	return f.client.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{})
}

func (f fakeCluster) PatchStatusCluster(ctx context.Context, gvr schema.GroupVersionResource, name string, status map[string]any) (*unstructured.Unstructured, error) {
	patch, err := json.Marshal(map[string]any{"status": status})
	if err != nil {
		return nil, err
	}
	return f.client.Resource(gvr).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
}

func resourceGVR(object *unstructured.Unstructured) schema.GroupVersionResource {
	gvk := object.GroupVersionKind()
	if gvk.Group == policy.ConstraintTemplateGroup {
		return policy.ConstraintTemplateGVR()
	}
	if gvk.Group == policy.ConstraintGroup {
		return policy.ConstraintGVR(gvk.Kind)
	}
	return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "customresourcedefinitions"}
}

func TestApplyAndReadCoverTemplatesConstraintsAndCRDs(t *testing.T) {
	template := testTemplate()
	library := policy.Library{
		Templates: []policy.Template{template},
		Lib:       policyeval.Lib(),
		Constraints: []policy.Constraint{{
			Name: "no-direct-guest-connect", Kind: template.Kind, Template: template.Name,
			Enforcement: policy.EnforcementDeny,
			Parameters:  map[string]any{"globs": []any{"test/**"}, "pattern": "Deno"},
		}},
	}
	cluster := newFakeCluster(t)
	if err := Apply(context.Background(), cluster, library, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.GetCluster(context.Background(),
		schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"},
		"nodirectguestconnect.constraints.gatekeeper.sh"); err != nil {
		t.Errorf("the constraint CRD was not created: %v", err)
	}
	read, err := Read(context.Background(), cluster)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Templates) != 1 || read.Templates[0].Kind != "NoDirectGuestConnect" {
		t.Fatalf("templates = %+v", read.Templates)
	}
	if read.Templates[0].Rego != testRego {
		t.Errorf("the rule did not survive kcp")
	}
	if len(read.Constraints) != 1 || read.Constraints[0].Enforcement != policy.EnforcementDeny {
		t.Fatalf("constraints = %+v", read.Constraints)
	}
	if read.Constraints[0].Template != template.Name {
		t.Errorf("template = %s", read.Constraints[0].Template)
	}
}

func newFakeCluster(t *testing.T) Cluster {
	t.Helper()
	scheme := runtime.NewScheme()
	listKinds := map[schema.GroupVersionResource]string{
		policy.ConstraintTemplateGVR():                                                        "ConstraintTemplateList",
		policy.ConstraintGVR("NoDirectGuestConnect"):                                          "NoDirectGuestConnectList",
		{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}: "CustomResourceDefinitionList",
	}
	return fakeCluster{client: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds)}
}
