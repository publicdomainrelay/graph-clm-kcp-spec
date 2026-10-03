// Package exportwatch is the multi workspace half of specd's watch: instead of
// one logical cluster, it watches every workspace that bound an APIExport,
// through the APIExport virtual workspace the provider publishes.
//
// It discovers the virtual workspace URL from the APIExportEndpointSlice in the
// provider workspace (../kcp-libs does the same), then holds one dynamic
// informer per resource against `<url>/clusters/*`. Every object that comes
// back carries the `kcp.io/cluster` annotation of the workspace it lives in, so
// the event names the logical cluster it belongs to and the controller can
// write back to that one cluster.
package exportwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	k8scache "k8s.io/client-go/tools/cache"

	"github.com/publicdomainrelay/kcp-libs/common/kcp"
	kexportwatch "github.com/publicdomainrelay/kcp-libs/impl/exportwatch"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// AllClusters is the kcp path segment that lists every logical cluster bound to
// an APIExport at once.
const AllClusters = "*"

const (
	DefaultProviderWorkspace = "root:specs-provider"

	DefaultExport = "specs.publicdomainrelay.dev"

	DefaultDiscoveryWait = 30 * time.Second
)

type Options struct {
	Kubeconfig string

	Context string

	ProviderWorkspace string

	Export string

	Resources []watch.Resource

	Resync time.Duration

	QPS float32

	Burst int

	DiscoveryWait time.Duration

	Log *slog.Logger
}

// Source is a watch.Source over every bound workspace.
type Source struct {
	config *rest.Config

	providerWorkspace string

	export string

	resources []watch.Resource

	resync time.Duration

	discoveryWait time.Duration

	log *slog.Logger
}

var _ watch.Source = (*Source)(nil)

func New(options Options) (*Source, error) {
	if options.Export == "" {
		return nil, errors.New("exportwatch: an APIExport name is required")
	}
	if len(options.Resources) == 0 {
		return nil, errors.New("exportwatch: at least one resource is required")
	}
	provider := options.ProviderWorkspace
	if provider == "" {
		provider = DefaultProviderWorkspace
	}
	config, err := kcpclient.WorkspaceRestConfig(options.Kubeconfig, options.Context, provider, options.QPS, options.Burst)
	if err != nil {
		return nil, fmt.Errorf("exportwatch: reach the provider workspace %s: %w", provider, err)
	}
	log := options.Log
	if log == nil {
		log = slog.Default()
	}
	wait := options.DiscoveryWait
	if wait == 0 {
		wait = DefaultDiscoveryWait
	}
	return &Source{
		config:            config,
		providerWorkspace: provider,
		export:            options.Export,
		resources:         options.Resources,
		resync:            options.Resync,
		discoveryWait:     wait,
		log:               log,
	}, nil
}

// Endpoint resolves the virtual workspace URL a bound export serves. It is
// exported because a caller that must write back per logical cluster (specd in
// export mode) builds its client from the same URL.
func (s *Source) Endpoint(ctx context.Context) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, s.discoveryWait)
	defer cancel()
	endpoints, err := kexportwatch.Await(waitCtx, kexportwatch.Options{
		Config:            s.config,
		ProviderWorkspace: s.providerWorkspace,
		Exports:           []string{s.export},
		Log:               s.log,
	})
	if err != nil {
		return "", fmt.Errorf("exportwatch: wait for the %s endpoint: %w", s.export, err)
	}
	urls := kexportwatch.Paths(endpoints, s.export)
	if len(urls) == 0 {
		return "", fmt.Errorf("exportwatch: the %s APIExport publishes no virtual workspace", s.export)
	}
	return urls[0], nil
}

func (s *Source) Run(ctx context.Context, notify watch.Notify) error {
	endpoint, err := s.Endpoint(ctx)
	if err != nil {
		return err
	}
	s.log.Info("exportwatch: watching every bound workspace",
		"export", s.export, "provider", s.providerWorkspace, "endpoint", endpoint)

	config := rest.CopyConfig(s.config)
	config.Host = kcpclient.WorkspaceHost(endpoint, AllClusters)
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("exportwatch: build a client for %s: %w", config.Host, err)
	}
	factory := dynamicinformer.NewDynamicSharedInformerFactory(client, s.resync)
	for _, resource := range s.resources {
		if err := s.watch(factory, resource, notify); err != nil {
			return err
		}
	}
	factory.Start(ctx.Done())
	for gvr, synced := range factory.WaitForCacheSync(ctx.Done()) {
		if !synced {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("exportwatch: the cache for %s did not sync", gvr.Resource)
		}
	}
	<-ctx.Done()
	return nil
}

func (s *Source) watch(factory dynamicinformer.DynamicSharedInformerFactory, resource watch.Resource, notify watch.Notify) error {
	informer := factory.ForResource(resource.GVR).Informer()
	_, err := informer.AddEventHandler(k8scache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { emit(notify, resource, watch.Added, obj) },
		UpdateFunc: func(_, obj any) {
			emit(notify, resource, watch.Updated, obj)
		},
		DeleteFunc: func(obj any) { emit(notify, resource, watch.Deleted, obj) },
	})
	if err != nil {
		return fmt.Errorf("exportwatch: watch %s: %w", resource.Kind, err)
	}
	return nil
}

func emit(notify watch.Notify, resource watch.Resource, event watch.Event, obj any) {
	key, ok := keyOf(obj)
	if !ok {
		return
	}
	notify(resource, event, key)
}

// keyOf reads the logical cluster, namespace and name of an event object. A
// delete can arrive as a tombstone, so the wrapped object is unwrapped first.
func keyOf(obj any) (watch.Key, bool) {
	object, ok := obj.(*unstructured.Unstructured)
	if !ok {
		if tombstone, isTombstone := obj.(k8scache.DeletedFinalStateUnknown); isTombstone {
			object, ok = tombstone.Obj.(*unstructured.Unstructured)
		}
		if !ok {
			return watch.Key{}, false
		}
	}
	cluster := object.GetAnnotations()[kcp.ClusterAnnotation]
	namespace := object.GetNamespace()
	name := object.GetName()
	if name == "" {
		return watch.Key{}, false
	}
	return watch.Key{Cluster: cluster, Namespace: namespace, Name: name}, true
}
