package watchpoll

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
)

var widgets = watch.Resource{
	Kind: "Widget",
	GVR:  schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"},
}

type fakeCluster struct {
	items []unstructured.Unstructured
}

func (f *fakeCluster) ListAll(context.Context, schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{Items: append([]unstructured.Unstructured{}, f.items...)}
	return list, nil
}

func widget(name, resourceVersion string) unstructured.Unstructured {
	object := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata": map[string]any{
			"name":            name,
			"namespace":       "default",
			"resourceVersion": resourceVersion,
		},
	}}
	return object
}

func TestSourceTurnsListDifferencesIntoEvents(t *testing.T) {
	cluster := &fakeCluster{items: []unstructured.Unstructured{widget("one", "1")}}
	source, err := New(Options{Cluster: cluster, Resources: []watch.Resource{widgets}, Interval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 16)
	go func() {
		_ = source.Run(ctx, func(_ watch.Resource, event watch.Event, key watch.Key) {
			events <- fmt.Sprintf("%s %s/%s", event, key.Namespace, key.Name)
		})
	}()

	expectEvent(t, events, "Added default/one")

	cluster.items = []unstructured.Unstructured{widget("one", "2")}
	expectEvent(t, events, "Updated default/one")

	cluster.items = []unstructured.Unstructured{widget("one", "2"), widget("two", "1")}
	expectEvent(t, events, "Added default/two")

	cluster.items = []unstructured.Unstructured{widget("two", "1")}
	expectEvent(t, events, "Deleted default/one")
}

func TestNewRejectsAnIncompleteSource(t *testing.T) {
	if _, err := New(Options{Resources: []watch.Resource{widgets}, Interval: time.Second}); err == nil {
		t.Error("a source without a cluster must not build")
	}
	if _, err := New(Options{Cluster: &fakeCluster{}, Interval: time.Second}); err == nil {
		t.Error("a source without resources must not build")
	}
	if _, err := New(Options{Cluster: &fakeCluster{}, Resources: []watch.Resource{widgets}}); err == nil {
		t.Error("a source without an interval must not build")
	}
}

func expectEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-events:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}
