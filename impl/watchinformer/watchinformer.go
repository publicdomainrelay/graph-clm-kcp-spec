package watchinformer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
)

type Options struct {
	Resources []watch.Resource

	Resync time.Duration

	Log *slog.Logger
}

type Source struct {
	resources []watch.Resource

	factory dynamicinformer.DynamicSharedInformerFactory

	log *slog.Logger
}

// New builds a dynamic shared informer source against the workspace the rest
// config already points at.
func New(config *rest.Config, opts Options) (*Source, error) {
	if config == nil {
		return nil, errors.New("watchinformer: a rest config is required")
	}
	if len(opts.Resources) == 0 {
		return nil, errors.New("watchinformer: at least one resource is required")
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("watchinformer: build the dynamic client: %w", err)
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Source{
		resources: opts.Resources,
		factory:   dynamicinformer.NewDynamicSharedInformerFactory(client, opts.Resync),
		log:       log,
	}, nil
}

func (s *Source) Run(ctx context.Context, notify watch.Notify) error {
	for _, resource := range s.resources {
		resource := resource
		informer := s.factory.ForResource(resource.GVR).Informer()
		notifyOf := func(event watch.Event) func(any) {
			return func(object any) {
				key, err := cache.MetaNamespaceKeyFunc(object)
				if err != nil {
					s.log.Error("watch event without a key", "kind", resource.Kind, "err", err)
					return
				}
				namespace, name, err := cache.SplitMetaNamespaceKey(key)
				if err != nil {
					s.log.Error("watch event with an unusable key", "kind", resource.Kind, "key", key, "err", err)
					return
				}
				notify(resource, event, watch.Key{Namespace: namespace, Name: name})
			}
		}
		if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    notifyOf(watch.Added),
			UpdateFunc: func(_, object any) { notifyOf(watch.Updated)(object) },
			DeleteFunc: notifyOf(watch.Deleted),
		}); err != nil {
			return fmt.Errorf("watchinformer: register the %s handler: %w", resource.Kind, err)
		}
	}

	s.factory.Start(ctx.Done())
	for gvr, synced := range s.factory.WaitForCacheSync(ctx.Done()) {
		if !synced {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("watchinformer: the cache for %s did not sync", gvr.Resource)
		}
	}
	<-ctx.Done()
	return nil
}
