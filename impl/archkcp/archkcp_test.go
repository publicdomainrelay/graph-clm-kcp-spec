package archkcp

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/archyaml"
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
	return gvr.String() + "/" + namespace + "/" + name
}

func (f *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	found, ok := f.objects[key(gvr, namespace, name)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
	}
	return found.DeepCopy(), nil
}

func (f *fakeCluster) List(_ context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	names := []string{}
	for objectKey, object := range f.objects {
		if gvrOf(object) != gvr {
			continue
		}
		if objectKey != key(gvr, namespace, object.GetName()) {
			continue
		}
		names = append(names, object.GetName())
	}
	sort.Strings(names)
	list := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": specapi.APIVersion, "kind": specapi.SystemContextListKind}}
	for _, name := range names {
		list.Items = append(list.Items, *f.objects[key(gvr, namespace, name)].DeepCopy())
	}
	return list, nil
}

func gvrOf(object *unstructured.Unstructured) schema.GroupVersionResource {
	gvk := object.GroupVersionKind()
	return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: specapi.ResourceForKind(gvk.Kind)}
}

func (f *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	objectKey := key(gvrOf(object), object.GetNamespace(), object.GetName())
	stored, exists := f.objects[objectKey]
	if !exists {
		created := object.DeepCopy()
		created.SetResourceVersion("1")
		created.SetGeneration(1)
		f.objects[objectKey] = created
		return created.DeepCopy(), nil
	}
	updated := object.DeepCopy()
	updated.SetResourceVersion(stored.GetResourceVersion())
	updated.SetGeneration(stored.GetGeneration())
	if !reflect.DeepEqual(stored.Object["spec"], updated.Object["spec"]) {
		updated.SetGeneration(stored.GetGeneration() + 1)
	}
	if status, ok := stored.Object["status"]; ok {
		updated.Object["status"] = status
	}
	f.objects[objectKey] = updated
	return updated.DeepCopy(), nil
}

func (f *fakeCluster) Delete(_ context.Context, gvr schema.GroupVersionResource, namespace, name string) error {
	delete(f.objects, key(gvr, namespace, name))
	return nil
}

func (f *fakeCluster) PatchStatus(_ context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error) {
	stored, ok := f.objects[key(gvr, namespace, name)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
	}
	updated := stored.DeepCopy()
	merged := map[string]any{}
	if existing, ok := updated.Object["status"].(map[string]any); ok {
		for field, value := range existing {
			merged[field] = value
		}
	}
	for field, value := range status {
		merged[field] = value
	}
	updated.Object["status"] = merged
	f.objects[key(gvr, namespace, name)] = updated
	return updated.DeepCopy(), nil
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "open-architecture", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestImportExportRoundTrip(t *testing.T) {
	for _, name := range []string{"arch.yaml", "arch-e77a69f.yaml", "arch-85799e6.yaml"} {
		t.Run(name, func(t *testing.T) {
			data := testdata(t, name)
			original, err := archyaml.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			cluster := newFakeCluster()
			ctx := context.Background()

			result, err := Import(ctx, cluster, data, ImportOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Contexts != len(original.Nodes()) {
				t.Fatalf("imported %d contexts, want %d", result.Contexts, len(original.Nodes()))
			}
			if result.Repository != "deno-kcp" && name == "arch.yaml" {
				t.Fatalf("repository = %q, want the metadata name", result.Repository)
			}

			names := map[string]bool{}
			for id, objectName := range result.Names {
				if problems := validation.IsDNS1123Label(objectName); len(problems) > 0 {
					t.Fatalf("%s: object name %q is not DNS-1123: %s", id, objectName, problems)
				}
				if len(objectName) > 63 {
					t.Fatalf("%s: object name %q is too long", id, objectName)
				}
				if names[objectName] {
					t.Fatalf("%s: object name %q is not unique", id, objectName)
				}
				names[objectName] = true
			}

			exported, err := Export(ctx, cluster, ExportOptions{})
			if err != nil {
				t.Fatal(err)
			}
			reparsed, err := archyaml.Parse(exported)
			if err != nil {
				t.Fatalf("reparse: %v", err)
			}
			if differences := archyaml.Diff(original, reparsed); len(differences) > 0 {
				t.Fatalf("round trip changed the document:\n%s", join(differences))
			}

			again, err := Export(ctx, cluster, ExportOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(exported) {
				t.Fatal("the second export differs from the first")
			}

			before := snapshotCluster(cluster)
			if _, err := Import(ctx, cluster, data, ImportOptions{}); err != nil {
				t.Fatal(err)
			}
			after := snapshotCluster(cluster)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("the second import changed the stored objects")
			}
		})
	}
}

func TestImportStoresTheArchView(t *testing.T) {
	cluster := newFakeCluster()
	ctx := context.Background()
	result, err := Import(ctx, cluster, testdata(t, "arch.yaml"), ImportOptions{RepositoryPath: "/repos/deno-kcp"})
	if err != nil {
		t.Fatal(err)
	}
	context := readContext(t, cluster, result.Names["sc.kind.denopod"])
	if context.Spec.Repository != "deno-kcp" {
		t.Errorf("repository = %q", context.Spec.Repository)
	}
	if context.Spec.Arch == nil || context.Spec.Arch.ID != "sc.kind.denopod" {
		t.Fatalf("arch = %+v", context.Spec.Arch)
	}
	if context.Spec.Arch.Parent != "sc.deno-kcp" || context.Spec.Arch.Slot == "" {
		t.Errorf("parent/slot = %q/%q", context.Spec.Arch.Parent, context.Spec.Arch.Slot)
	}
	if context.Spec.Upstream != spec.RefSelf {
		t.Errorf("upstream = %q, want self", context.Spec.Upstream)
	}
	if context.Spec.Arch.Node["upstream"] == nil {
		t.Error("the manifest upstream must stay in the preserved node body")
	}
	if context.Spec.Arch.Orchestrator != "sc.deno-kcp-provider" {
		t.Errorf("orchestrator = %q", context.Spec.Arch.Orchestrator)
	}
	if len(context.Spec.CodeRefs) == 0 || context.Spec.CodeRefs[0][:5] != "file:" {
		t.Errorf("codeRefs = %v, want file: refs", context.Spec.CodeRefs)
	}

	if got := context.Labels[specapi.ArchIDLabel]; got != "sc.kind.denopod" {
		t.Errorf("%s = %q", specapi.ArchIDLabel, got)
	}
	if got := context.Labels[specapi.ArchKindLabel]; got != spec.ArchKindNode {
		t.Errorf("%s = %q", specapi.ArchKindLabel, got)
	}
	document := readContext(t, cluster, result.Document)
	if got := document.Labels[specapi.ArchKindLabel]; got != spec.ArchKindDocument {
		t.Errorf("the document %s = %q", specapi.ArchKindLabel, got)
	}

	kine := readContext(t, cluster, result.Names["sc.kine-local"])
	if kine.Spec.Upstream != "up.kine" {
		t.Errorf("sc.kine-local upstream = %q, want up.kine", kine.Spec.Upstream)
	}
	if kine.Spec.Arch.Parent != "sc.kcp-local" || kine.Spec.Arch.Slot != "overlay[1]" {
		t.Errorf("sc.kine-local parent/slot = %q/%q", kine.Spec.Arch.Parent, kine.Spec.Arch.Slot)
	}
	if result.Repository != "deno-kcp" {
		t.Errorf("repository = %q", result.Repository)
	}
	repository, err := cluster.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, "deno-kcp")
	if err != nil {
		t.Fatal(err)
	}
	path, _, _ := unstructured.NestedString(repository.Object, "spec", "path")
	if path != "/repos/deno-kcp" {
		t.Errorf("repository path = %q", path)
	}
}

func TestImportPrunesStaleNodes(t *testing.T) {
	cluster := newFakeCluster()
	ctx := context.Background()
	bigger := []byte(`$schema: arch.schema.json
apiVersion: open-architecture.dffml.github.io/v0alpha1
kind: OpenArchitecture
metadata: {name: mini}
upstreams:
  up.a: {bin: a}
system_contexts:
  - id: sc.root
    upstream: up.a
    overlay: [sc.leaf]
    orchestrator: orch.run
  - id: sc.leaf
    upstream: sc.root
    overlay: []
    orchestrator: orch.run
`)
	smaller := []byte(`$schema: arch.schema.json
apiVersion: open-architecture.dffml.github.io/v0alpha1
kind: OpenArchitecture
metadata: {name: mini}
upstreams:
  up.a: {bin: a}
system_contexts:
  - id: sc.root
    upstream: up.a
    overlay: []
    orchestrator: orch.run
`)
	if _, err := Import(ctx, cluster, bigger, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "sc-leaf"); err != nil {
		t.Fatalf("the leaf was not imported: %v", err)
	}
	result, err := Import(ctx, cluster, smaller, ImportOptions{Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Deleted, []string{"sc.leaf"}) {
		t.Fatalf("deleted = %v, want the leaf", result.Deleted)
	}
	if _, err := cluster.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "sc-leaf"); !kcpclient.IsNotFound(err) {
		t.Fatalf("the leaf survived the prune: %v", err)
	}
	exported, err := Export(ctx, cluster, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err := archyaml.Parse(exported)
	if err != nil {
		t.Fatal(err)
	}
	if document.Node("sc.leaf") != nil {
		t.Fatal("the pruned node is still in the export")
	}
}

func TestExportWithoutAnImport(t *testing.T) {
	if _, err := Export(context.Background(), newFakeCluster(), ExportOptions{}); err == nil {
		t.Fatal("export without an imported document must fail")
	}
}

func TestSystemContextCRDPreservesTheArchBlock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "crds", "specs.publicdomainrelay.dev_systemcontexts.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	crd := map[string]any{}
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	specSchema := navigate(t, crd, "spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties")
	for _, field := range []string{"arch", "dependsOn", "introduces"} {
		if _, ok := specSchema[field]; !ok {
			t.Fatalf("the CRD has no spec.%s", field)
		}
	}
	arch := specSchema["arch"].(map[string]any)
	if arch["x-kubernetes-preserve-unknown-fields"] != true {
		t.Fatalf("spec.arch must preserve unknown fields: %v", arch)
	}
}

func navigate(t *testing.T, value any, path ...any) map[string]any {
	t.Helper()
	current := value
	for _, step := range path {
		switch key := step.(type) {
		case string:
			mapping, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("navigate %v: not a mapping", path)
			}
			current = mapping[key]
		case int:
			list, ok := current.([]any)
			if !ok || key >= len(list) {
				t.Fatalf("navigate %v: not a list", path)
			}
			current = list[key]
		}
	}
	mapping, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("navigate %v: ended on %T", path, current)
	}
	return mapping
}

func readContext(t *testing.T, cluster *fakeCluster, name string) *spec.SystemContext {
	t.Helper()
	found, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(found)
	if err != nil {
		t.Fatal(err)
	}
	context, ok := typed.(*spec.SystemContext)
	if !ok {
		t.Fatalf("read back a %T", typed)
	}
	return context
}

func snapshotCluster(cluster *fakeCluster) map[string]any {
	out := map[string]any{}
	for objectKey, object := range cluster.objects {
		encoded, err := yaml.Marshal(object.Object)
		if err != nil {
			continue
		}
		out[objectKey] = string(encoded)
	}
	return out
}

func join(values []string) string {
	return strings.Join(values, "\n")
}

// The generated arch.yaml used to repeat each context's intent, requirement
// text and interfaces; it now carries the structure and points at
// specs/<context>.yaml for the text. import-arch must read both.
func TestGeneratedArchitectureImportsBothShapes(t *testing.T) {
	full := []byte(`apiVersion: open-architecture.dffml.github.io/v0alpha1
kind: GeneratedArchitecture
metadata:
  name: calc
  branch: open-architecture/calc
system_contexts:
- id: sc.calc
  name: calc
  upstream: self
  intent: Integer arithmetic.
  requirements:
  - id: r.add
    level: MUST
    text: Add returns the sum.
  interfaces:
  - name: Add
    kind: function
    signature: func Add(a, b int) int
    file: calc/calc.go
  code:
  - calc/calc.go
  codeRefIndex:
  - function:abc Add@calc/calc.go:3
- id: sc.calc-ui
  name: calc-ui
  upstream: up.calc
  depends_on:
  - sc.calc
  requirements:
  - id: r.button
    level: MUST
    text: A button adds two numbers.
`)
	compact := []byte(`apiVersion: open-architecture.dffml.github.io/v0alpha1
kind: GeneratedArchitecture
metadata:
  name: calc
  branch: open-architecture/calc
system_contexts:
- id: sc.calc
  name: calc
  spec: specs/calc.yaml
  upstream: self
  requirements:
  - id: r.add
    level: MUST
  codeRefIndex:
  - function:abc Add@calc/calc.go:3
- id: sc.calc-ui
  name: calc-ui
  spec: specs/calc-ui.yaml
  upstream: up.calc
  depends_on:
  - sc.calc
  requirements:
  - id: r.button
    level: MUST
`)
	for name, data := range map[string][]byte{"full": full, "compact": compact} {
		t.Run(name, func(t *testing.T) {
			cluster := newFakeCluster()
			result, err := Import(context.Background(), cluster, data, ImportOptions{})
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if result.Repository != "calc" {
				t.Errorf("repository = %q", result.Repository)
			}
			context := readContext(t, cluster, result.Names["sc.calc"])
			if context.Spec.Upstream != spec.RefSelf {
				t.Errorf("sc.calc upstream = %q", context.Spec.Upstream)
			}
			if context.Spec.Arch == nil || context.Spec.Arch.ID != "sc.calc" {
				t.Fatalf("sc.calc arch = %+v", context.Spec.Arch)
			}
			ui := readContext(t, cluster, result.Names["sc.calc-ui"])
			if ui.Spec.Upstream != "up.calc" {
				t.Errorf("sc.calc-ui upstream = %q", ui.Spec.Upstream)
			}
			if len(ui.Spec.DependsOn) != 1 || ui.Spec.DependsOn[0] != "sc.calc" {
				t.Errorf("sc.calc-ui dependsOn = %v", ui.Spec.DependsOn)
			}
		})
	}
}
