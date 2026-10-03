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

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/bundle"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
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

	// DefaultMaxAttempts caps one episode of drift: after this many failed
	// attempts the controller stops raising new ones, so a broken agent cannot
	// fill the workspace with retries.
	DefaultMaxAttempts = 3

	// DefaultRetryBackoff is the wait before the second attempt at an episode.
	// Each further attempt doubles it.
	DefaultRetryBackoff = 5 * time.Second

	DefaultAgentTimeout = claudecli.DefaultTimeout

	// DefaultCacheDir is where a Repository with a git source is cloned. It
	// lives under the state directory this repository already keeps.
	DefaultCacheDir = ".kcp-specd/cache"

	// DefaultMaxConcurrentSummaries is how many contexts of one populate may be
	// summarized at the same time across the namespace.
	DefaultMaxConcurrentSummaries = 2
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

	// CacheDir is where a git source is cloned.
	CacheDir string

	// MaxConcurrentSummaries limits how many CodeToSpec changes one populate
	// lets run at once. Zero means the default.
	MaxConcurrentSummaries int

	Graph graph.Writer

	Agent string

	AgentCommand string

	AgentArgs []string

	AgentTimeout time.Duration

	Budget int

	NodeLimit int

	ManagedBudget int

	MaxAttempts int

	RetryBackoff time.Duration

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

	agents *agentfactory.Factory

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
	if opts.AgentTimeout <= 0 {
		opts.AgentTimeout = claudecli.DefaultTimeout
	}
	if opts.Budget <= 0 {
		opts.Budget = bundle.DefaultBudget
	}
	if opts.NodeLimit <= 0 {
		opts.NodeLimit = bundle.DefaultNodeLimit
	}
	if opts.ManagedBudget <= 0 {
		opts.ManagedBudget = agent.DefaultManagedBudget
	}
	if opts.CacheDir == "" {
		opts.CacheDir = DefaultCacheDir
	}
	if opts.MaxConcurrentSummaries <= 0 {
		opts.MaxConcurrentSummaries = DefaultMaxConcurrentSummaries
	}
	if opts.MaxAttempts < 0 {
		opts.MaxAttempts = DefaultMaxAttempts
	}
	if opts.RetryBackoff <= 0 {
		opts.RetryBackoff = DefaultRetryBackoff
	}

	agents, err := agentfactory.New(agentfactory.Options{
		Kind:    opts.Agent,
		Command: opts.AgentCommand,
		Args:    opts.AgentArgs,
		Timeout: opts.AgentTimeout,
	})
	if err != nil {
		return nil, err
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
		agents: agents,
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
