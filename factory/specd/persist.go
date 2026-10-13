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
	case specapi.PlanKind:
		return c.planPersistRepository(ctx, namespace, item.Name)
	case specapi.FindingKind:
		return c.findingPersistRepository(ctx, namespace, item.Name)
	}
	return ""
}

// planPersistRepository is the repository a plan's status change persists: the
// repository its spec names, or the only Repository the namespace holds when it
// names none.
func (c *Controller) planPersistRepository(ctx context.Context, namespace, name string) string {
	object, err := c.client.Get(ctx, specapi.PlanGVR, namespace, name)
	if err != nil {
		return ""
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return ""
	}
	plan, ok := typed.(*spec.Plan)
	if !ok {
		return ""
	}
	if plan.Spec.Repository != "" {
		return plan.Spec.Repository
	}
	return c.onlyRepository(ctx, namespace)
}

// findingPersistRepository is the repository a finding's status change
// persists: the repository its spec names, or the only Repository the namespace
// holds when it names none.
func (c *Controller) findingPersistRepository(ctx context.Context, namespace, name string) string {
	object, err := c.client.Get(ctx, specapi.FindingGVR, namespace, name)
	if err != nil {
		return ""
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return ""
	}
	finding, ok := typed.(*spec.Finding)
	if !ok {
		return ""
	}
	if finding.Spec.Repository != "" {
		return finding.Spec.Repository
	}
	return c.onlyRepository(ctx, namespace)
}

// onlyRepository is the single Repository a namespace holds. A namespace with
// none, or with more than one, leaves the plan's repository unknown.
func (c *Controller) onlyRepository(ctx context.Context, namespace string) string {
	list, err := c.client.List(ctx, specapi.RepositoryGVR, namespace)
	if err != nil {
		return ""
	}
	name := ""
	for _, object := range list.Items {
		if name != "" {
			return ""
		}
		name = object.GetName()
	}
	return name
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
