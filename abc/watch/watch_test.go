package watch

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestEventNamesEveryTransition(t *testing.T) {
	cases := map[Event]string{
		Added:    "Added",
		Updated:  "Updated",
		Deleted:  "Deleted",
		Event(9): "Unknown",
	}
	for event, want := range cases {
		if got := event.String(); got != want {
			t.Errorf("Event(%d).String() = %q, want %q", event, got, want)
		}
	}
}

func TestNotifyCarriesTheResourceTheEventAndTheKey(t *testing.T) {
	resource := Resource{Kind: "SystemContext", GVR: schema.GroupVersionResource{
		Group: "specs.publicdomainrelay.dev", Version: "v1alpha1", Resource: "systemcontexts",
	}}
	var gotResource Resource
	var gotEvent Event
	var gotKey Key

	var source Source = notifySource{resource: resource, key: Key{Namespace: "default", Name: "calc"}}
	err := source.Run(context.Background(), func(delivered Resource, event Event, key Key) {
		gotResource, gotEvent, gotKey = delivered, event, key
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotResource != resource {
		t.Errorf("resource = %+v, want %+v", gotResource, resource)
	}
	if gotEvent != Updated {
		t.Errorf("event = %s, want Updated", gotEvent)
	}
	if gotKey != (Key{Namespace: "default", Name: "calc"}) {
		t.Errorf("key = %+v", gotKey)
	}
}

type notifySource struct {
	resource Resource

	key Key
}

func (s notifySource) Run(ctx context.Context, notify Notify) error {
	notify(s.resource, Updated, s.key)
	return ctx.Err()
}

func TestClusterContractListsEveryNamespace(t *testing.T) {
	var cluster Cluster = listCluster{}
	listed, err := cluster.ListAll(context.Background(), schema.GroupVersionResource{Resource: "repositories"})
	if err != nil {
		t.Fatal(err)
	}
	if listed == nil || len(listed.Items) != 0 {
		t.Errorf("listed = %+v, want an empty list", listed)
	}
}

type listCluster struct{}

func (listCluster) ListAll(context.Context, schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}
