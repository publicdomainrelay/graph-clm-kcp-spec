package specd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/watchinformer"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/watchpoll"
)

const (
	WatchInformer = "informer"

	WatchPoll = "poll"

	DefaultResync = 5 * time.Second

	DefaultPollInterval = 2 * time.Second

	DefaultWorkers = 4
)

type Options struct {
	Kubeconfig string

	Context string

	Workspace string

	Namespace string

	QPS float32

	Burst int

	Watch string

	PollInterval time.Duration

	Resync time.Duration

	Workers int

	Tool string

	Graph graph.Writer

	Log *slog.Logger
}

type key struct {
	Kind string

	Namespace string

	Name string
}

// Cluster is the part of a kcp workspace the reconcilers use. It is an
// interface so a test can reconcile against an in-memory workspace.
type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	Create(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type Controller struct {
	opts Options

	client Cluster

	source watch.Source

	queue workqueue.TypedRateLimitingInterface[key]

	log *slog.Logger
}

// Resources is the set of objects the controller watches. It is exported so a
// test can watch the same set with its own source.
func Resources() []watch.Resource {
	return []watch.Resource{
		{Kind: specapi.RepositoryKind, GVR: specapi.RepositoryGVR},
		{Kind: specapi.SystemContextKind, GVR: specapi.SystemContextGVR},
		{Kind: specapi.SpecChangeKind, GVR: specapi.SpecChangeGVR},
	}
}

func New(opts Options) (*Controller, error) {
	if opts.Namespace == "" {
		opts.Namespace = specapi.DefaultNamespace
	}
	if opts.Watch == "" {
		opts.Watch = WatchInformer
	}
	if opts.Resync <= 0 {
		opts.Resync = DefaultResync
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = DefaultPollInterval
	}
	if opts.Workers <= 0 {
		opts.Workers = DefaultWorkers
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}

	client, err := kcpclient.New(kcpclient.Options{
		Kubeconfig: opts.Kubeconfig,
		Context:    opts.Context,
		Workspace:  opts.Workspace,
		Namespace:  opts.Namespace,
		QPS:        opts.QPS,
		Burst:      opts.Burst,
	})
	if err != nil {
		return nil, err
	}
	source, err := newSource(opts, client)
	if err != nil {
		return nil, err
	}
	return &Controller{
		opts:   opts,
		client: client,
		source: source,
		queue:  workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[key]()),
		log:    opts.Log,
	}, nil
}

func newSource(opts Options, client *kcpclient.Client) (watch.Source, error) {
	switch opts.Watch {
	case WatchPoll:
		return watchpoll.New(watchpoll.Options{
			Cluster:   client,
			Resources: Resources(),
			Interval:  opts.PollInterval,
			Log:       opts.Log,
		})
	case WatchInformer:
		config, err := kcpclient.WorkspaceRestConfig(opts.Kubeconfig, opts.Context, opts.Workspace, opts.QPS, opts.Burst)
		if err != nil {
			return nil, err
		}
		return watchinformer.New(config, watchinformer.Options{Resources: Resources(), Log: opts.Log})
	}
	return nil, fmt.Errorf("specd: %q is not %s or %s", opts.Watch, WatchInformer, WatchPoll)
}

func (c *Controller) Client() Cluster {
	return c.client
}

func (c *Controller) QueueDepth() int {
	return c.queue.Len()
}

// Enqueue asks for a reconcile of one object. A watch calls it; so does a test
// that drives the controller without a watch.
func (c *Controller) Enqueue(resource watch.Resource, object watch.Key) {
	namespace := object.Namespace
	if namespace == "" {
		namespace = c.opts.Namespace
	}
	c.queue.Add(key{Kind: resource.Kind, Namespace: namespace, Name: object.Name})
}

func (c *Controller) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	watchErr := make(chan error, 1)
	go func() {
		watchErr <- c.source.Run(runCtx, func(resource watch.Resource, _ watch.Event, object watch.Key) {
			c.Enqueue(resource, object)
		})
	}()

	var workers sync.WaitGroup
	for index := 0; index < c.opts.Workers; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			c.worker(runCtx)
		}()
	}

	var err error
	select {
	case <-ctx.Done():
	case err = <-watchErr:
		cancel()
	}
	c.queue.ShutDown()
	workers.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (c *Controller) worker(ctx context.Context) {
	for {
		item, shutdown := c.queue.Get()
		if shutdown {
			return
		}
		func() {
			defer c.queue.Done(item)
			requeue, err := c.reconcile(ctx, item)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				c.log.Error("reconcile failed", "kind", item.Kind, "name", item.Name, "err", err)
				c.queue.AddRateLimited(item)
				return
			}
			c.queue.Forget(item)
			if requeue > 0 {
				c.queue.AddAfter(item, requeue)
			}
		}()
	}
}

func (c *Controller) reconcile(ctx context.Context, item key) (time.Duration, error) {
	namespace := item.Namespace
	if namespace == "" {
		namespace = c.opts.Namespace
	}
	switch item.Kind {
	case specapi.RepositoryKind:
		return c.reconcileRepository(ctx, namespace, item.Name)
	case specapi.SystemContextKind:
		return c.reconcileSystemContext(ctx, namespace, item.Name)
	case specapi.SpecChangeKind:
		return c.reconcileSpecChange(ctx, namespace, item.Name)
	}
	return 0, fmt.Errorf("specd: no reconciler for %q", item.Kind)
}
