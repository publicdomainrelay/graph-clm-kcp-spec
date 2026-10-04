package specd

import (
	"context"
	"errors"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/persist"
)

const (
	PersistKind = "OpenArchitecturePersist"

	DefaultPersistDelay = time.Second
)

func (c *Controller) schedulePersist(ctx context.Context, item key) {
	if !c.opts.Persist {
		return
	}
	repository := c.repositoryOf(ctx, item)
	if repository == "" {
		return
	}
	c.queue.AddAfter(key{Kind: PersistKind, Cluster: item.Cluster, Namespace: item.Namespace, Name: repository}, c.opts.PersistDelay)
}

func (c *Controller) repositoryOf(ctx context.Context, item key) string {
	namespace := item.Namespace
	if namespace == "" {
		namespace = c.opts.Namespace
	}
	switch item.Kind {
	case specapi.RepositoryKind:
		return item.Name
	case specapi.SystemContextKind:
		return c.contextRepository(ctx, namespace, item.Name)
	case specapi.SpecChangeKind:
		object, err := c.client.Get(ctx, specapi.SpecChangeGVR, namespace, item.Name)
		if err != nil {
			return ""
		}
		typed, err := kcpclient.Typed(object)
		if err != nil {
			return ""
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok {
			return ""
		}
		return c.contextRepository(ctx, namespace, change.Spec.SystemContext)
	}
	return ""
}

func (c *Controller) contextRepository(ctx context.Context, namespace, name string) string {
	object, err := c.client.Get(ctx, specapi.SystemContextGVR, namespace, name)
	if err != nil {
		return ""
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return ""
	}
	context, ok := typed.(*spec.SystemContext)
	if !ok {
		return ""
	}
	return context.Spec.Repository
}

func (c *Controller) reconcilePersist(ctx context.Context, namespace, repository string) (time.Duration, error) {
	object, err := c.client.Get(ctx, specapi.RepositoryGVR, namespace, repository)
	if err != nil {
		if kcpclient.IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return 0, err
	}
	current, ok := typed.(*spec.Repository)
	if !ok || current.WorkPath() == "" {
		return 0, nil
	}
	if status := c.branchStatus(ctx, current); status.Mismatch {
		c.log.Debug("open-architecture persist skipped: the checkout is on another branch",
			"repository", repository, "message", status.Message)
		return 0, nil
	}
	result, err := persist.Persist(ctx, persist.Options{
		Cluster:       c.client,
		Namespace:     namespace,
		Repository:    repository,
		Remote:        c.opts.PersistRemote,
		ManagedBudget: c.opts.ManagedBudget,
		Adopt:         true,
	})
	if errors.Is(err, persist.ErrNotTopLevel) {
		c.log.Debug("open-architecture persist skipped", "repository", repository, "reason", err.Error())
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if result.Committed || len(result.Imported) > 0 || len(result.Created) > 0 || len(result.Conflicts) > 0 {
		c.log.Info("open-architecture persisted",
			"repository", repository,
			"branch", result.Branch,
			"commit", result.Commit,
			"paths", len(result.Paths),
			"imported", result.Imported,
			"created", result.Created,
			"conflicts", result.Conflicts,
			"deferred", result.Deferred,
			"pushed", result.Pushed)
	}
	return 0, nil
}
