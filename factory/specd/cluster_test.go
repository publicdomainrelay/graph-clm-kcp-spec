package specd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// TestModeDefaultsToTheWorkspaceMode: a caller that says nothing gets the
// single workspace behaviour it had before the export mode existed.
func TestModeDefaultsToTheWorkspaceMode(t *testing.T) {
	if got := mode(Options{}); got != ModeWorkspace {
		t.Fatalf("mode = %q, want %q", got, ModeWorkspace)
	}
	if got := mode(Options{Mode: ModeExport}); got != ModeExport {
		t.Fatalf("mode = %q, want %q", got, ModeExport)
	}
}

// TestClusterRouterFallsBackWithoutACluster is the guarantee the plain mode and
// every unit test rely on: with no logical cluster in the context, the router is
// the client it was given.
func TestClusterRouterFallsBackWithoutACluster(t *testing.T) {
	base := newFakeCluster()
	router := newClusterRouter(base, testKubeconfig(t), "", specapi.DefaultNamespace, 0, 0)
	if err := router.SetEndpoint("http://127.0.0.1:1/services/apiexport/x/specs"); err != nil {
		t.Fatal(err)
	}
	target, err := router.target(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if target != Cluster(base) {
		t.Fatal("a request with no logical cluster did not use the base client")
	}
}

// TestClusterRouterIsTheBaseBeforeTheEndpointIsKnown: the endpoint arrives only
// once the export's slice is published, and until then nothing may be built.
func TestClusterRouterIsTheBaseBeforeTheEndpointIsKnown(t *testing.T) {
	base := newFakeCluster()
	router := newClusterRouter(base, "", "", specapi.DefaultNamespace, 0, 0)
	target, err := router.target(watch.WithCluster(context.Background(), "cluster-a"))
	if err != nil {
		t.Fatal(err)
	}
	if target != Cluster(base) {
		t.Fatal("a cluster was routed before the endpoint was known")
	}
}

// TestClusterRouterBuildsOneClientPerCluster checks the export mode's write
// path: a request for a logical cluster becomes a client of its own, cached, so
// the second reconcile of the same tenant does not rebuild it.
func TestClusterRouterBuildsOneClientPerCluster(t *testing.T) {
	base := newFakeCluster()
	router := newClusterRouter(base, testKubeconfig(t), "", specapi.DefaultNamespace, 50, 100)
	if err := router.SetEndpoint("http://127.0.0.1:1/services/apiexport/x/specs"); err != nil {
		t.Fatal(err)
	}
	ctx := watch.WithCluster(context.Background(), "cluster-a")
	first, err := router.target(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == Cluster(base) {
		t.Fatal("a cluster with a known endpoint used the base client")
	}
	second, err := router.target(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("the router built a second client for the same cluster")
	}
	// A different cluster gets a different client: the tenants must not share
	// one, or a write would land in the wrong workspace.
	other, err := router.target(watch.WithCluster(context.Background(), "cluster-b"))
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("two logical clusters share one client")
	}
}

// TestClusterRouterDelegatesListsAndWrites proves the Cluster interface is
// satisfied by delegation rather than by a second implementation.
// testKubeconfig writes the smallest kubeconfig a client can be built from.
// The cluster is never contacted: the ids are only compared.
func testKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	body := "apiVersion: v1\nkind: Config\nclusters:\n- name: test\n  cluster:\n    server: https://127.0.0.1:1\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\ncurrent-context: test\nusers:\n- name: test\n  user: {}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClusterRouterDelegatesListsAndWrites(t *testing.T) {
	base := newFakeCluster()
	router := newClusterRouter(base, "", "", specapi.DefaultNamespace, 0, 0)
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.Group + "/" + specapi.Version,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "calc", "namespace": specapi.DefaultNamespace},
	}}
	if _, err := router.Apply(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	listed, err := router.List(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].GetName() != "calc" {
		t.Fatalf("list = %+v", listed.Items)
	}
	if _, err := router.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
}
