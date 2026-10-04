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
	Cluster string

	Namespace string

	Name string
}

type Notify func(resource Resource, event Event, key Key)

type clusterKey struct{}

func WithCluster(ctx context.Context, cluster string) context.Context {
	return context.WithValue(ctx, clusterKey{}, cluster)
}

func ClusterOf(ctx context.Context) string {
	cluster, _ := ctx.Value(clusterKey{}).(string)
	return cluster
}

type Source interface {
	Run(ctx context.Context, notify Notify) error
}

type Cluster interface {
	ListAll(ctx context.Context, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error)
}
