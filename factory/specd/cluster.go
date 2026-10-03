package specd

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// clusterRouter is the client the reconcilers use in the export mode: the
// object they are handed came from a watch that spans every bound workspace, so
// every read and write has to go back to the logical cluster that object lives
// in. The cluster travels in the context (abc/watch), which the worker sets from
// the watch key, and the router builds one client per cluster against the
// APIExport virtual workspace.
//
// With no cluster in the context it is the plain workspace client, so the single
// workspace mode and every unit test are unaffected.
type clusterRouter struct {
	base Cluster

	kubeconfig string

	context string

	namespace string

	qps float32

	burst int

	mu sync.Mutex

	config *rest.Config

	endpoint string

	clients map[string]Cluster
}

var _ Cluster = (*clusterRouter)(nil)

func newClusterRouter(base Cluster, kubeconfig, configContext, namespace string, qps float32, burst int) *clusterRouter {
	return &clusterRouter{
		base:       base,
		kubeconfig: kubeconfig,
		context:    configContext,
		namespace:  namespace,
		qps:        qps,
		burst:      burst,
		clients:    map[string]Cluster{},
	}
}

// SetEndpoint points the router at the APIExport virtual workspace the export
// mode discovered. Until it is set every call goes to the base client.
func (r *clusterRouter) SetEndpoint(endpoint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if endpoint == r.endpoint {
		return nil
	}
	if r.config == nil {
		config, err := kcpclient.RestConfig(r.kubeconfig, r.context)
		if err != nil {
			return fmt.Errorf("specd: reach the kcp of %s: %w", r.kubeconfig, err)
		}
		r.config = config
	}
	r.endpoint = endpoint
	r.clients = map[string]Cluster{}
	return nil
}

func (r *clusterRouter) target(ctx context.Context) (Cluster, error) {
	cluster := watch.ClusterOf(ctx)
	if cluster == "" {
		return r.base, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.endpoint == "" {
		return r.base, nil
	}
	if client, ok := r.clients[cluster]; ok {
		return client, nil
	}
	config := rest.CopyConfig(r.config)
	config.Host = r.endpoint
	client, err := kcpclient.NewFromRestConfig(config, cluster, r.namespace, r.qps, r.burst)
	if err != nil {
		return nil, fmt.Errorf("specd: reach the workspace %s through the APIExport: %w", cluster, err)
	}
	r.clients[cluster] = client
	return client, nil
}

func (r *clusterRouter) Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	client, err := r.target(ctx)
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, gvr, namespace, name)
}

func (r *clusterRouter) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	client, err := r.target(ctx)
	if err != nil {
		return nil, err
	}
	return client.List(ctx, gvr, namespace)
}

func (r *clusterRouter) Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	client, err := r.target(ctx)
	if err != nil {
		return nil, err
	}
	return client.Apply(ctx, object)
}

func (r *clusterRouter) Create(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	client, err := r.target(ctx)
	if err != nil {
		return nil, err
	}
	return client.Create(ctx, object)
}

func (r *clusterRouter) PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error) {
	client, err := r.target(ctx)
	if err != nil {
		return nil, err
	}
	return client.PatchStatus(ctx, gvr, namespace, name, status)
}
