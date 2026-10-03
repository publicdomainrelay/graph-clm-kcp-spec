package watchpoll

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
)

type Options struct {
	Cluster watch.Cluster

	Resources []watch.Resource

	Interval time.Duration

	Log *slog.Logger
}

// Source lists the workspace on a timer and turns the difference between two
// listings into events. It is the fallback for an API server whose watch a
// client cannot hold open, and it costs one list per resource per interval.
type Source struct {
	opts Options

	seen map[string]map[string]string
}

func New(opts Options) (*Source, error) {
	if opts.Cluster == nil {
		return nil, errors.New("watchpoll: a cluster is required")
	}
	if len(opts.Resources) == 0 {
		return nil, errors.New("watchpoll: at least one resource is required")
	}
	if opts.Interval <= 0 {
		return nil, errors.New("watchpoll: a positive interval is required")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Source{opts: opts, seen: map[string]map[string]string{}}, nil
}

func (s *Source) Run(ctx context.Context, notify watch.Notify) error {
	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	for {
		if err := s.poll(ctx, notify); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.opts.Log.Error("poll failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Source) poll(ctx context.Context, notify watch.Notify) error {
	for _, resource := range s.opts.Resources {
		listed, err := s.opts.Cluster.ListAll(ctx, resource.GVR)
		if err != nil {
			return fmt.Errorf("watchpoll: list %s: %w", resource.Kind, err)
		}
		current := map[string]string{}
		for index := range listed.Items {
			object := &listed.Items[index]
			key := watch.Key{Namespace: object.GetNamespace(), Name: object.GetName()}
			current[objectKey(key)] = object.GetResourceVersion()
		}
		previous := s.seen[resource.Kind]
		for objectKey, resourceVersion := range current {
			key := parseObjectKey(objectKey)
			before, known := previous[objectKey]
			switch {
			case !known:
				notify(resource, watch.Added, key)
			case before != resourceVersion:
				notify(resource, watch.Updated, key)
			}
		}
		for objectKey := range previous {
			if _, still := current[objectKey]; !still {
				notify(resource, watch.Deleted, parseObjectKey(objectKey))
			}
		}
		s.seen[resource.Kind] = current
	}
	return nil
}

func objectKey(key watch.Key) string {
	return key.Namespace + "/" + key.Name
}

func parseObjectKey(objectKey string) watch.Key {
	namespace, name, _ := strings.Cut(objectKey, "/")
	return watch.Key{Namespace: namespace, Name: name}
}

var _ watch.Source = (*Source)(nil)
