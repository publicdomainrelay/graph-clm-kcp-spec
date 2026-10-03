package exportwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8scache "k8s.io/client-go/tools/cache"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const (
	exportID = "abcd1234efgh5678"

	exportName = "specs.publicdomainrelay.dev"

	provider = "root:specs-provider"
)

// The two objects one virtual workspace watch delivers: one per tenant, each
// carrying the annotation kcp puts on an object it serves through an export.
const (
	objectA = `{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContext","metadata":{"name":"calc","namespace":"tenant-a","resourceVersion":"2","annotations":{"kcp.io/cluster":"cluster-a"}}}`

	objectB = `{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContext","metadata":{"name":"greet","namespace":"tenant-b","resourceVersion":"3","annotations":{"kcp.io/cluster":"cluster-b"}}}`
)

const bookmark = `{"type":"BOOKMARK","object":{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContext","metadata":{"resourceVersion":"3","annotations":{"k8s.io/initial-events-end":"true"}}}}`

// fakeKcp serves the two paths an export mode watch needs: the endpoint slice
// in the provider workspace, and the virtual workspace itself, which answers
// `/clusters/*` with every bound workspace's objects.
func fakeKcp(t *testing.T, items []string) *httptest.Server {
	t.Helper()
	slice := fmt.Sprintf(`{"apiVersion":"apis.kcp.io/v1alpha1","kind":"APIExportEndpointSliceList","metadata":{"resourceVersion":"1"},"items":[{"metadata":{"name":%q},"spec":{"export":{"name":%q,"path":%q}},"status":{"endpoints":[{"url":%q}]}}]}`,
		exportName, exportName, provider, "PLACEHOLDER")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/clusters/"+provider+"/apis/apis.kcp.io/v1alpha1/apiexportendpointslices"):
			fmt.Fprint(w, strings.Replace(slice, "PLACEHOLDER", "http://"+r.Host+"/services/apiexport/"+exportID+"/"+exportName, 1))
		case strings.HasPrefix(r.URL.Path, "/services/apiexport/"+exportID+"/"+exportName+"/clusters/*/apis/specs.publicdomainrelay.dev/v1alpha1/systemcontexts"):
			if r.URL.Query().Get("watch") != "true" {
				fmt.Fprintf(w, `{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContextList","metadata":{"resourceVersion":"1"},"items":[%s]}`, strings.Join(items, ","))
				return
			}
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("the test server cannot flush")
				return
			}
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			if r.URL.Query().Get("sendInitialEvents") == "true" {
				for _, item := range items {
					fmt.Fprintf(w, `{"type":"ADDED","object":%s}`+"\n", item)
				}
				fmt.Fprint(w, bookmark+"\n")
			}
			flusher.Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// kubeconfigFor writes the smallest kubeconfig that points a client at the fake
// server, which is what the source resolves its provider client from.
func kubeconfigFor(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	body := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, server.URL)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewValidatesItsOptions(t *testing.T) {
	if _, err := New(Options{Resources: []watch.Resource{{Kind: specapi.SystemContextKind}}}); err == nil {
		t.Error("an export with no name was accepted")
	}
	if _, err := New(Options{Export: exportName}); err == nil {
		t.Error("a source with no resources was accepted")
	}
}

// TestRunKeysCarryTheLogicalCluster is the whole point of the package: one
// watch over `/clusters/*` delivers every tenant's objects, and each event must
// name the logical cluster it came from so the controller can write back to it.
func TestRunKeysCarryTheLogicalCluster(t *testing.T) {
	server := fakeKcp(t, []string{objectA, objectB})
	source, err := New(Options{
		Kubeconfig:        kubeconfigFor(t, server),
		ProviderWorkspace: provider,
		Export:            exportName,
		Resources:         []watch.Resource{{Kind: specapi.SystemContextKind, GVR: specapi.SystemContextGVR}},
		DiscoveryWait:     10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := source.Endpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := server.URL + "/services/apiexport/" + exportID + "/" + exportName; endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	keys := make(chan watch.Key, 8)
	done := make(chan error, 1)
	go func() {
		done <- source.Run(ctx, func(_ watch.Resource, _ watch.Event, key watch.Key) {
			keys <- key
		})
	}()

	seen := map[string]watch.Key{}
	for len(seen) < 2 {
		select {
		case key := <-keys:
			seen[key.Cluster] = key
		case <-time.After(15 * time.Second):
			t.Fatalf("the watch delivered %v", seen)
		}
	}
	if got := seen["cluster-a"]; got.Namespace != "tenant-a" || got.Name != "calc" {
		t.Errorf("cluster-a key = %+v", got)
	}
	if got := seen["cluster-b"]; got.Namespace != "tenant-b" || got.Name != "greet" {
		t.Errorf("cluster-b key = %+v", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("the source stopped with %v", err)
	}
}

// TestKeyOfUnwrapsATombstone covers the delete path: an informer can deliver a
// deleted object wrapped in a tombstone, and the key must still be read.
func TestKeyOfUnwrapsATombstone(t *testing.T) {
	object := &unstructured.Unstructured{}
	if err := json.Unmarshal([]byte(objectA), &object.Object); err != nil {
		t.Fatal(err)
	}
	key, ok := keyOf(k8scache.DeletedFinalStateUnknown{Key: "tenant-a/calc", Obj: object})
	if !ok {
		t.Fatal("a tombstone was not read")
	}
	if key.Cluster != "cluster-a" || key.Namespace != "tenant-a" || key.Name != "calc" {
		t.Fatalf("key = %+v", key)
	}
	if _, ok := keyOf("not an object"); ok {
		t.Error("something that is not an object was read as a key")
	}
}
