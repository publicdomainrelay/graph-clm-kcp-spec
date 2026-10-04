package archkcp

import (
	"context"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const seedDocument = `
metadata: {name: calc, root: sc.calc}
system_contexts:
  - id: sc.calc
    upstream: sc.kcp-local
    orchestrator: orch.demo
  - id: sc.kcp-local
    upstream: up.kcp
  - id: sc.runner
    source: cmd/calc
    upstream: sc.calc
    depends_on: [sc.calc]
  - id: sc.other
    source: other/mod.go
`

func seedCluster(t *testing.T, contexts ...*spec.SystemContext) *fakeCluster {
	t.Helper()
	cluster := newFakeCluster()
	for _, context := range contexts {
		context.SetDefaults()
		object, err := kcpclient.Unstructured(context)
		if err != nil {
			t.Fatal(err)
		}
		object.SetGeneration(1)
		object.SetResourceVersion("1")
		cluster.objects[key(specapi.SystemContextGVR, context.Namespace, context.Name)] = object
	}
	return cluster
}

func contextIn(t *testing.T, cluster *fakeCluster, name string) *spec.SystemContext {
	t.Helper()
	object, ok := cluster.objects[key(specapi.SystemContextGVR, specapi.DefaultNamespace, name)]
	if !ok {
		t.Fatalf("%s is not in the cluster", name)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return typed.(*spec.SystemContext)
}

func TestSeedCarriesTheArchFactsOntoAGeneratedContext(t *testing.T) {
	cluster := seedCluster(t,
		&spec.SystemContext{
			ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc", Namespace: specapi.DefaultNamespace},
			Spec:       spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf},
		},
		&spec.SystemContext{
			ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
			Spec:       spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf},
		},
	)
	partitions := []specsync.Partition{
		{Name: "calc", Directory: ".", TreeFiles: []string{"Makefile"}},
		{Name: "cmd-calc", Directory: "cmd/calc", Files: []string{"cmd/calc/main.go"}},
	}
	seeded, err := Seed(context.Background(), cluster, SeedOptions{
		Repository:  "calc",
		Data:        []byte(seedDocument),
		Partitions:  partitions,
		RootContext: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seeded.Seeded, []string{"calc", "cmd-calc"}) {
		t.Fatalf("seeded = %v, want the root and the command context", seeded.Seeded)
	}
	root := contextIn(t, cluster, "calc")
	if root.Spec.Upstream != "up.kcp" || root.Spec.Orchestrator != "orch.demo" {
		t.Errorf("root = %+v, want the upstream the root node chains to and its orchestrator", root.Spec)
	}
	context := contextIn(t, cluster, "cmd-calc")
	if context.Spec.Upstream != "sc.calc" {
		t.Errorf("upstream = %q, want sc.calc from the arch node", context.Spec.Upstream)
	}
	if !reflect.DeepEqual(context.Spec.DependsOn, []string{"sc.calc"}) {
		t.Errorf("dependsOn = %v", context.Spec.DependsOn)
	}
	if context.Spec.Arch == nil || context.Spec.Arch.ID != "sc.runner" {
		t.Fatalf("arch = %+v, want the matched node", context.Spec.Arch)
	}
	if len(context.Spec.CodeRefs) != 1 || context.Spec.CodeRefs[0] != "file:cmd/calc/main.go" {
		t.Errorf("code refs = %v, want only the tracked file, not the directory", context.Spec.CodeRefs)
	}
	if context.Spec.Arch == nil || len(context.Spec.Arch.Code) != 1 {
		t.Errorf("arch code = %+v, want only the tracked file", context.Spec.Arch)
	}
}

func TestSeedIsIdempotentAndLeavesUnmatchedContextsAlone(t *testing.T) {
	cluster := seedCluster(t,
		&spec.SystemContext{
			ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc", Namespace: specapi.DefaultNamespace},
			Spec:       spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf},
		},
		&spec.SystemContext{
			ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: specapi.DefaultNamespace},
			Spec:       spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf, Intent: "human intent"},
		},
	)
	partitions := []specsync.Partition{
		{Name: "cmd-calc", Directory: "cmd/calc", Files: []string{"cmd/calc/main.go"}},
		{Name: "store", Directory: "store", Files: []string{"store/store.go"}},
	}
	options := SeedOptions{Repository: "calc", Data: []byte(seedDocument), Partitions: partitions}
	if _, err := Seed(context.Background(), cluster, options); err != nil {
		t.Fatal(err)
	}
	second, err := Seed(context.Background(), cluster, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Seeded) != 0 {
		t.Fatalf("the second seed wrote %v: it must be idempotent", second.Seeded)
	}
	store := contextIn(t, cluster, "store")
	if store.Spec.Arch != nil || store.Spec.Intent != "human intent" {
		t.Errorf("store = %+v, want it untouched", store.Spec)
	}
}

func TestSeedKeepsHumanEditsAndTheIngestDependencies(t *testing.T) {
	cluster := seedCluster(t, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   "up.go",
			DependsOn:  []string{"sc.store"},
			Introduces: []string{"sc.new-thing"},
		},
	})
	partitions := []specsync.Partition{
		{Name: "calc", Directory: ".", TreeFiles: []string{"Makefile"}},
		{Name: "cmd-calc", Directory: "cmd/calc", Files: []string{"cmd/calc/main.go"}},
	}
	if _, err := Seed(context.Background(), cluster, SeedOptions{
		Repository:  "calc",
		Data:        []byte(seedDocument),
		Partitions:  partitions,
		RootContext: true,
	}); err != nil {
		t.Fatal(err)
	}
	context := contextIn(t, cluster, "cmd-calc")
	if context.Spec.Upstream != "up.go" {
		t.Errorf("upstream = %q, want the human value kept", context.Spec.Upstream)
	}
	if !reflect.DeepEqual(context.Spec.DependsOn, []string{"sc.calc", "sc.store"}) {
		t.Errorf("dependsOn = %v, want the ingest dependency and the arch one", context.Spec.DependsOn)
	}
	if !reflect.DeepEqual(context.Spec.Introduces, []string{"sc.new-thing"}) {
		t.Errorf("introduces = %v", context.Spec.Introduces)
	}
}

func TestSeedLooksLikeIngestNotLikeAHumanEdit(t *testing.T) {
	cluster := seedCluster(t, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf},
	})
	partitions := []specsync.Partition{{Name: "cmd-calc", Directory: "cmd/calc", Files: []string{"cmd/calc/main.go"}}}
	if _, err := Seed(context.Background(), cluster, SeedOptions{
		Repository: "calc",
		Data:       []byte(seedDocument),
		Partitions: partitions,
	}); err != nil {
		t.Fatal(err)
	}
	object := cluster.objects[key(specapi.SystemContextGVR, specapi.DefaultNamespace, "cmd-calc")]
	if origin := object.GetAnnotations()[specapi.OriginAnnotation]; origin != specapi.OriginIngest {
		t.Errorf("origin = %q, want %q", origin, specapi.OriginIngest)
	}
	context := contextIn(t, cluster, "cmd-calc")
	hash, err := spec.HashSystemContextSpec(context.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if context.Status.RealizedSpecHash != hash {
		t.Errorf("realizedSpecHash = %q, want the seeded spec so no spec to code change is raised", context.Status.RealizedSpecHash)
	}
	if object.GetAnnotations()[specapi.OriginHashAnnotation] != hash {
		t.Errorf("origin hash = %q, want the seeded spec hash", object.GetAnnotations()[specapi.OriginHashAnnotation])
	}
}

func TestSeedKeepsAnOverlayRefTheDocumentSpellsAndNeverTurnsItIntoAnUpstream(t *testing.T) {
	const document = `
metadata: {name: calc, root: sc.calc}
system_contexts:
  - id: sc.calc
    overlay: [sc.helper, ov.notes]
  - id: sc.helper
    upstream: up.go
  - id: ov.notes
    source: notes.md
`
	cluster := seedCluster(t, &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf},
	})
	partitions := []specsync.Partition{{Name: "calc", Directory: ".", TreeFiles: []string{"Makefile"}}}
	if _, err := Seed(context.Background(), cluster, SeedOptions{
		Repository:  "calc",
		Data:        []byte(document),
		Partitions:  partitions,
		RootContext: true,
	}); err != nil {
		t.Fatal(err)
	}
	context := contextIn(t, cluster, "calc")
	if !reflect.DeepEqual(context.Spec.Overlay, []string{"ov.notes", "sc.helper"}) {
		t.Fatalf("overlay = %v, want the document's own spellings", context.Spec.Overlay)
	}
	if result := spec.ValidateSystemContext(context); !result.OK() {
		t.Fatalf("the seeded root does not validate: %v", result.Err())
	}
}
