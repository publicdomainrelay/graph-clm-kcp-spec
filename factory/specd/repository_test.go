package specd

import (
	"context"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func TestNeedsIndex(t *testing.T) {
	settled := &spec.Repository{
		Spec: spec.RepositorySpec{Source: &spec.RepositorySource{Path: "/src/unseen"}},
		Status: spec.RepositoryStatus{
			ResolvedPath:    "/src/unseen",
			Phase:           specapi.PhasePopulated,
			IndexedCommit:   "c1",
			PopulateRequest: "req-1",
		},
	}
	controller := &Controller{opts: Options{CacheDir: ".kcp-specd/cache"}}

	cases := []struct {
		name       string
		repository *spec.Repository
		path       string
		commit     string
		request    string
		want       bool
	}{
		{"settled", settled, "/src/unseen", "c1", "req-1", false},
		{"head moved", settled, "/src/unseen", "c2", "req-1", true},
		{"another tree", settled, "/src/other", "c1", "req-1", true},
		{"new request", settled, "/src/unseen", "c1", "req-2", true},
		{"no request annotation", settled, "/src/unseen", "c1", "", false},
		{"first run", &spec.Repository{
			Spec:   spec.RepositorySpec{Source: &spec.RepositorySource{Path: "/src/unseen"}},
			Status: spec.RepositoryStatus{Phase: ""},
		}, "/src/unseen", "c1", "", true},
		{"still populating", &spec.Repository{
			Spec: spec.RepositorySpec{Source: &spec.RepositorySource{Path: "/src/unseen"}},
			Status: spec.RepositoryStatus{
				ResolvedPath: "/src/unseen", Phase: specapi.PhasePopulating, IndexedCommit: "c1",
			},
		}, "/src/unseen", "c1", "", false},
		{"a tree without commits is indexed once", &spec.Repository{
			Spec: spec.RepositorySpec{Source: &spec.RepositorySource{Path: "/src/plain"}},
			Status: spec.RepositoryStatus{
				ResolvedPath: "/src/plain", Phase: specapi.PhasePopulated,
			},
		}, "/src/plain", "", "", false},
		{"a tree without commits and no phase yet", &spec.Repository{
			Spec: spec.RepositorySpec{Source: &spec.RepositorySource{Path: "/src/plain"}},
		}, "/src/plain", "", "", true},
	}
	for _, testCase := range cases {
		if got := controller.needsIndex(testCase.repository, testCase.path, testCase.commit, testCase.request); got != testCase.want {
			t.Errorf("%s: needsIndex = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestCacheDirForStaysInsideTheCache(t *testing.T) {
	controller := &Controller{opts: Options{CacheDir: ".kcp-specd/cache"}}
	dir, err := controller.cacheDirFor("unseen")
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(".kcp-specd/cache/unseen")
	if err != nil {
		t.Fatal(err)
	}
	if dir != absolute {
		t.Errorf("dir = %q, want %q", dir, absolute)
	}
}

func TestASpecOnlyRepositoryIsNotIndexed(t *testing.T) {
	cluster := newFakeCluster()
	repository := &spec.Repository{}
	repository.Name = "deno-kcp"
	repository.Namespace = specapi.DefaultNamespace
	apply(t, cluster, repository)
	controller := agentController(t, cluster, calcScenario)
	requeue, err := controller.reconcileRepository(context.Background(), specapi.DefaultNamespace, "deno-kcp")
	if err != nil || requeue != 0 {
		t.Fatalf("requeue %s, err %v", requeue, err)
	}
	object, err := cluster.Get(context.Background(), specapi.RepositoryGVR, specapi.DefaultNamespace, "deno-kcp")
	if err != nil {
		t.Fatal(err)
	}
	if phase, _, _ := unstructured.NestedString(object.Object, "status", "phase"); phase != "" {
		t.Fatalf("a repository with no source was indexed: phase %q", phase)
	}
}
