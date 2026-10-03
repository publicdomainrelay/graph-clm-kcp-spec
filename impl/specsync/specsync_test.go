package specsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured
	applied int
}

func newFake(contexts ...spec.SystemContext) *fakeCluster {
	cluster := &fakeCluster{objects: map[string]*unstructured.Unstructured{}}
	for index := range contexts {
		cluster.put(&contexts[index])
	}
	return cluster
}

func (c *fakeCluster) put(context *spec.SystemContext) {
	if context.Namespace == "" {
		context.Namespace = specapi.DefaultNamespace
	}
	object, err := kcpclient.Unstructured(context)
	if err != nil {
		panic(err)
	}
	object.SetResourceVersion("1")
	c.objects[context.Name] = object
}

func (c *fakeCluster) List(_ context.Context, _ schema.GroupVersionResource, _ string) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	for _, object := range c.objects {
		list.Items = append(list.Items, *object.DeepCopy())
	}
	return list, nil
}

func (c *fakeCluster) Apply(_ context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	c.applied++
	c.objects[object.GetName()] = object.DeepCopy()
	return object, nil
}

func (c *fakeCluster) context(t *testing.T, name string) *spec.SystemContext {
	t.Helper()
	object, ok := c.objects[name]
	if !ok {
		t.Fatalf("no context %s in the fake cluster", name)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return typed.(*spec.SystemContext)
}

func calcContext() spec.SystemContext {
	return spec.SystemContext{
		TypeMeta:   metav1.TypeMeta{APIVersion: specapi.Group + "/" + specapi.Version, Kind: specapi.SystemContextKind},
		ObjectMeta: meta("calc", nil),
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Upstream:   "self",
			Intent:     "a calculator",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: "MUST", Text: "add two ints"},
			},
		},
	}
}

func TestPullWritesOneFilePerContext(t *testing.T) {
	dir := t.TempDir()
	cluster := newFake(calcContext())
	result, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Pull,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pulled != 1 || result.Pushed != 0 {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(FilePath(dir, "calc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "status") || !strings.Contains(string(data), "intent: a calculator") {
		t.Errorf("the file is not a spec-only mirror:\n%s", data)
	}
	// The baseline is recorded, so the next run is a no-op.
	second, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Both,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Pulled != 0 || second.Pushed != 0 {
		t.Errorf("a second sync wrote again: %+v", second.Records)
	}
	if got := cluster.context(t, "calc").GetAnnotations()[specapi.SyncedHashAnnotation]; got == "" {
		t.Error("the synced hash annotation is empty")
	}
}

func TestPushAppliesAnEditedFile(t *testing.T) {
	dir := t.TempDir()
	cluster := newFake(calcContext())
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Pull,
	}); err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(readFile(t, FilePath(dir, "calc")), "intent: a calculator", "intent: a calculator, now with subtract", 1)
	if err := os.WriteFile(FilePath(dir, "calc"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Both,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pushed != 1 {
		t.Fatalf("result = %+v", result)
	}
	context := cluster.context(t, "calc")
	if !strings.Contains(context.Spec.Intent, "subtract") {
		t.Errorf("kcp did not take the file's intent: %q", context.Spec.Intent)
	}
	if origin := context.GetAnnotations()[specapi.OriginAnnotation]; origin != specapi.OriginGit {
		t.Errorf("origin = %q, want %q", origin, specapi.OriginGit)
	}
	if _, stamped := context.GetAnnotations()[specapi.OriginHashAnnotation]; stamped {
		t.Error("a pushed spec carries origin-hash, which would hide the edit from the controller")
	}
	// Both sides are in step afterwards.
	file := readFile(t, FilePath(dir, "calc"))
	if !strings.Contains(file, "now with subtract") {
		t.Errorf("the file did not come back canonical:\n%s", file)
	}
	if hashesDiffer(t, cluster, dir) {
		t.Error("kcp and the file still differ after both")
	}
}

func TestBothChangedIsAConflict(t *testing.T) {
	dir := t.TempDir()
	cluster := newFake(calcContext())
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Pull,
	}); err != nil {
		t.Fatal(err)
	}
	// kcp moves.
	moved := cluster.context(t, "calc")
	moved.Spec.Intent = "changed in kcp"
	cluster.put(moved)
	// The file moves too.
	edited := strings.Replace(readFile(t, FilePath(dir, "calc")), "a calculator", "changed in the file", 1)
	if err := os.WriteFile(FilePath(dir, "calc"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Both,
	})
	if !errors.Is(err, ErrConflicts) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if !strings.Contains(err.Error(), "--prefer") {
		t.Errorf("the error does not say how to resolve the conflict: %v", err)
	}
	if !strings.Contains(readFile(t, FilePath(dir, "calc")), "changed in the file") {
		t.Error("a conflict overwrote the file")
	}
	if cluster.context(t, "calc").Spec.Intent != "changed in kcp" {
		t.Error("a conflict overwrote kcp")
	}
}

func TestPreferGitResolvesAConflict(t *testing.T) {
	dir := t.TempDir()
	cluster := newFake(calcContext())
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Pull,
	}); err != nil {
		t.Fatal(err)
	}
	moved := cluster.context(t, "calc")
	moved.Spec.Intent = "changed in kcp"
	cluster.put(moved)
	edited := strings.Replace(readFile(t, FilePath(dir, "calc")), "a calculator", "changed in the file", 1)
	if err := os.WriteFile(FilePath(dir, "calc"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Both, Prefer: mirror.PreferGit,
	}); err != nil {
		t.Fatal(err)
	}
	if got := cluster.context(t, "calc").Spec.Intent; got != "changed in the file" {
		t.Errorf("intent = %q, want the file to win", got)
	}
}

func TestPreferKCPResolvesAConflict(t *testing.T) {
	dir := t.TempDir()
	cluster := newFake(calcContext())
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Pull,
	}); err != nil {
		t.Fatal(err)
	}
	moved := cluster.context(t, "calc")
	moved.Spec.Intent = "changed in kcp"
	cluster.put(moved)
	edited := strings.Replace(readFile(t, FilePath(dir, "calc")), "a calculator", "changed in the file", 1)
	if err := os.WriteFile(FilePath(dir, "calc"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{
		Cluster: cluster, Repository: "calc", Dir: dir, Direction: mirror.Both, Prefer: mirror.PreferKCP,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, FilePath(dir, "calc")), "changed in kcp") {
		t.Error("kcp did not win the conflict")
	}
	if got := cluster.context(t, "calc").Spec.Intent; got != "changed in kcp" {
		t.Errorf("intent = %q, want kcp untouched", got)
	}
}

func TestWriteFileIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	context := calcContext()
	path, changed, err := WriteFile(dir, context)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("the first write reported no change")
	}
	if _, changed, err = WriteFile(dir, context); err != nil || changed {
		t.Fatalf("a repeated write changed the file: %v %v", changed, err)
	}
	if path != FilePath(dir, "calc") {
		t.Errorf("path = %q", path)
	}
}

func TestRunNeedsARepository(t *testing.T) {
	if _, err := Run(context.Background(), Options{Cluster: newFake(), Dir: t.TempDir()}); err == nil {
		t.Fatal("a sync without a repository name was accepted")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func hashesDiffer(t *testing.T, cluster *fakeCluster, dir string) bool {
	t.Helper()
	context := cluster.context(t, "calc")
	parsed, err := mirror.Parse("calc", []byte(readFile(t, FilePath(dir, "calc"))))
	if err != nil {
		t.Fatal(err)
	}
	kcpHash, err := mirror.DeclaredHash(context.Spec)
	if err != nil {
		t.Fatal(err)
	}
	fileHash, err := mirror.DeclaredHash(parsed.Spec)
	if err != nil {
		t.Fatal(err)
	}
	return kcpHash != fileHash
}

var _ = filepath.Join

func meta(name string, annotations map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace, Annotations: annotations}
}
