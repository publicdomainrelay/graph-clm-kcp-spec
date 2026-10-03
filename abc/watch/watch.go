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
	Namespace string

	Name string
}

type Notify func(resource Resource, event Event, key Key)

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
