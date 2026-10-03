package watchinformer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const systemContextPath = "/apis/specs.publicdomainrelay.dev/v1alpha1/systemcontexts"

const liveEvent = `{"type":"ADDED","object":{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContext","metadata":{"name":"calc","namespace":"default","resourceVersion":"2"}}}`

// The end of the initial events stream: a client that asks for the streaming
// list waits for this bookmark before it reports the cache synced.
const bookmarkEvent = `{"type":"BOOKMARK","object":{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContext","metadata":{"resourceVersion":"2","annotations":{"k8s.io/initial-events-end":"true"}}}}`

// The wiring a unit test can prove without kcp: the informer lists or streams
// the list, holds the watch, and hands every event to Notify with its
// namespaced key.
func fakeAPIServer(t *testing.T, items []string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != systemContextPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("watch") == "true" {
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
				fmt.Fprint(w, bookmarkEvent+"\n")
			}
			fmt.Fprint(w, liveEvent+"\n")
			flusher.Flush()
			<-r.Context().Done()
			return
		}
		fmt.Fprintf(w, `{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContextList","metadata":{"resourceVersion":"1"},"items":[%s]}`,
			strings.Join(items, ","))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNewRequiresAConfigAndAResource(t *testing.T) {
	if _, err := New(nil, Options{Resources: []watch.Resource{{Kind: "SystemContext"}}}); err == nil {
		t.Error("a nil rest config was accepted")
	}
	if _, err := New(&rest.Config{Host: "http://127.0.0.1:1"}, Options{}); err == nil {
		t.Error("a source with no resources was accepted")
	}
}

func TestRunDeliversAnAddedEventFromTheWatch(t *testing.T) {
	server := fakeAPIServer(t, nil)
	source, err := New(&rest.Config{Host: server.URL}, Options{
		Resources: []watch.Resource{{Kind: specapi.SystemContextKind, GVR: specapi.SystemContextGVR}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	events := make(chan watch.Key, 8)
	resource := make(chan watch.Resource, 8)
	done := make(chan error, 1)
	go func() {
		done <- source.Run(ctx, func(delivered watch.Resource, _ watch.Event, key watch.Key) {
			resource <- delivered
			events <- key
		})
	}()

	select {
	case key := <-events:
		if key.Name != "calc" || key.Namespace != "default" {
			t.Errorf("key = %+v", key)
		}
		if delivered := <-resource; delivered.Kind != specapi.SystemContextKind {
			t.Errorf("resource = %+v", delivered)
		}
	case err := <-done:
		t.Fatalf("the source stopped early: %v", err)
	case <-ctx.Done():
		t.Fatal("no event arrived")
	}

	// A cancel must stop the source instead of leaking the informer.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v after a cancel", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("the source did not stop on cancel")
	}
}

func TestRunDeliversTheListedObjectsToo(t *testing.T) {
	listed := []string{`{"apiVersion":"specs.publicdomainrelay.dev/v1alpha1","kind":"SystemContext","metadata":{"name":"listed","namespace":"team","resourceVersion":"1"}}`}
	server := fakeAPIServer(t, listed)
	source, err := New(&rest.Config{Host: server.URL}, Options{
		Resources: []watch.Resource{{Kind: specapi.SystemContextKind, GVR: specapi.SystemContextGVR}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	events := make(chan watch.Key, 8)
	go func() {
		_ = source.Run(ctx, func(_ watch.Resource, _ watch.Event, key watch.Key) { events <- key })
	}()

	for {
		select {
		case key := <-events:
			if key.Name == "listed" {
				if key.Namespace != "team" {
					t.Errorf("key = %+v", key)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("the listed object never arrived")
		}
	}
}
