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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/exportwatch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/watchinformer"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/watchpoll"
)

const (
	WatchInformer = "informer"

	WatchPoll = "poll"

	ModeWorkspace = "workspace"

	ModeExport = "export"

	DefaultResync = 5 * time.Second

	DefaultPollInterval = 2 * time.Second

	DefaultWorkers = 4

	DefaultMaxAttempts = 3

	DefaultRetryBackoff = 5 * time.Second

	DefaultBatchWindow = 5 * time.Second

	DefaultAgentTimeout = claudecli.DefaultTimeout

	DefaultCacheDir = ".kcp-specd/cache"

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

	Mode string

	ProviderWorkspace string

	ExportName string

	PollInterval time.Duration

	Resync time.Duration

	Workers int

	Tool string

	CacheDir string

	MaxConcurrentSummaries int

	Graph graph.Writer

	Agent string

	AgentCommand string

	AgentArgs []string

	AgentTimeout time.Duration

	ClmMod string

	PiExtension string

	AgentEnv map[string]string

	Budget int

	NodeLimit int

	ManagedBudget int

	Persist bool

	PersistDelay time.Duration

	PersistRemote string

	MaxAttempts int

	RetryBackoff time.Duration

	BatchWindow time.Duration

	Log *slog.Logger
}

type key struct {
	Kind string

	Cluster string

	Namespace string

	Name string
}

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

	router *clusterRouter

	source watch.Source

	queue workqueue.TypedRateLimitingInterface[key]

	agents *agentfactory.Factory

	log *slog.Logger
}

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
	if opts.BatchWindow <= 0 {
		opts.BatchWindow = DefaultBatchWindow
	}
	if opts.PersistDelay <= 0 {
		opts.PersistDelay = DefaultPersistDelay
	}

	opts.Agent = agentKind(opts)
	agents, err := agentfactory.New(agentfactory.Options{
		Kind:        opts.Agent,
		Command:     opts.AgentCommand,
		Args:        opts.AgentArgs,
		Timeout:     opts.AgentTimeout,
		ClmMod:      opts.ClmMod,
		PiExtension: opts.PiExtension,
		Env:         opts.AgentEnv,
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
	controller := &Controller{
		opts:   opts,
		client: client,
		queue:  workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[key]()),
		agents: agents,
		log:    opts.Log,
	}
	if mode(opts) == ModeExport {
		controller.router = newClusterRouter(client, opts.Kubeconfig, opts.Context, opts.Namespace, opts.QPS, opts.Burst)
		controller.client = controller.router
	}
	source, err := newSource(opts, controller.client)
	if err != nil {
		return nil, err
	}
	controller.source = source
	return controller, nil
}

func mode(opts Options) string {
	if opts.Mode == "" {
		return ModeWorkspace
	}
	return opts.Mode
}

func agentKind(opts Options) string {
	if opts.Agent == "" && opts.ClmMod != "" {
		return agentfactory.ClaudeMod
	}
	return opts.Agent
}

func newSource(opts Options, client Cluster) (watch.Source, error) {
	if mode(opts) == ModeExport {
		if opts.Watch != WatchInformer {
			return nil, fmt.Errorf("specd: the export mode needs the %s watch, not %q", WatchInformer, opts.Watch)
		}
		return exportwatch.New(exportwatch.Options{
			Kubeconfig:        opts.Kubeconfig,
			Context:           opts.Context,
			ProviderWorkspace: opts.ProviderWorkspace,
			Export:            opts.ExportName,
			Resources:         Resources(),
			Resync:            opts.Resync,
			QPS:               opts.QPS,
			Burst:             opts.Burst,
			Log:               opts.Log,
		})
	}
	switch opts.Watch {
	case WatchPoll:
		lister, ok := client.(watch.Cluster)
		if !ok {
			return nil, fmt.Errorf("specd: the %s watch needs a client that can list every namespace", WatchPoll)
		}
		return watchpoll.New(watchpoll.Options{
			Cluster:   lister,
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

func (c *Controller) Enqueue(resource watch.Resource, object watch.Key) {
	namespace := object.Namespace
	if namespace == "" {
		namespace = c.opts.Namespace
	}
	c.queue.Add(key{Kind: resource.Kind, Cluster: object.Cluster, Namespace: namespace, Name: object.Name})
}

func (c *Controller) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if c.router != nil {
		source, ok := c.source.(*exportwatch.Source)
		if !ok {
			return errors.New("specd: the export mode has no APIExport watch")
		}
		endpoint, err := source.Endpoint(runCtx)
		if err != nil {
			return err
		}
		if err := c.router.SetEndpoint(endpoint); err != nil {
			return err
		}
	}

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
	ctx = watch.WithCluster(ctx, item.Cluster)
	var requeue time.Duration
	var err error
	switch item.Kind {
	case specapi.RepositoryKind:
		requeue, err = c.reconcileRepository(ctx, namespace, item.Name)
	case specapi.SystemContextKind:
		requeue, err = c.reconcileSystemContext(ctx, namespace, item.Name)
	case specapi.SpecChangeKind:
		requeue, err = c.reconcileSpecChange(ctx, namespace, item.Name)
	case PersistKind:
		return c.reconcilePersist(ctx, namespace, item.Name)
	default:
		return 0, fmt.Errorf("specd: no reconciler for %q", item.Kind)
	}
	if err == nil {
		c.schedulePersist(ctx, item)
	}
	return requeue, err
}
