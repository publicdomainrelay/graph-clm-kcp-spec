package watch

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Resource struct {
	Kind string

	GVR schema.GroupVersionResource
}

type Event int

const (
	Added Event = iota
	Updated
	Deleted
)

func (e Event) String() string {
	switch e {
	case Added:
		return "Added"
	case Updated:
		return "Updated"
	case Deleted:
		return "Deleted"
	}
	return "Unknown"
}

type Key struct {
	// Cluster is the logical cluster the object lives in. It is empty in the
	// single workspace mode, where the watch already runs against one cluster,
	// and it names the bound workspace in the export mode, where one watch
	// delivers every tenant's objects.
	Cluster string

	Namespace string

	Name string
}

type Notify func(resource Resource, event Event, key Key)

// clusterKey is the context key the logical cluster travels under. A
// reconciler asks for the cluster of the object it was handed and the client
// routes there, so the reconcilers keep their (namespace, name) signatures in
// both the single workspace and the export mode.
type clusterKey struct{}

func WithCluster(ctx context.Context, cluster string) context.Context {
	return context.WithValue(ctx, clusterKey{}, cluster)
}

func ClusterOf(ctx context.Context) string {
	cluster, _ := ctx.Value(clusterKey{}).(string)
	return cluster
}

// Source delivers workspace object events to a Notify function until the
// context is done. The informer source watches; the poll source lists.
type Source interface {
	Run(ctx context.Context, notify Notify) error
}

// Cluster is the read side a poll source needs: every object of one resource,
// in every namespace.
type Cluster interface {
	ListAll(ctx context.Context, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error)
}
