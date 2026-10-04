package specd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/summarize"
)

const messageLimit = 2000

func (c *Controller) reconcileCodeToSpec(ctx context.Context, namespace, name string, change *spec.SpecChange) (time.Duration, error) {
	if repository := c.changeRepository(ctx, namespace, change); repository != nil {
		if status := c.branchStatus(ctx, repository); status.Mismatch {
			c.log.Info("a code to spec change waits: the checkout is on another branch",
				"change", name, "repository", repository.Name, "message", status.Message)
			return 0, nil
		}
	}

	running, err := c.runningChanges(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		return 0, err
	}
	if !specsync.RunningAdmitted(change.Name, running) {
		c.failChange(ctx, namespace, change, "admission: another change of "+change.Spec.SystemContext+" is already running")
		return 0, nil
	}

	if c.attemptsTaken(ctx, namespace, change) > c.opts.MaxAttempts && c.opts.MaxAttempts > 0 {
		c.failChange(ctx, namespace, change, fmt.Sprintf("attempt cap: %s already has %d attempts", episodeBase(change), c.opts.MaxAttempts))
		return 0, nil
	}

	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, name, map[string]any{
		"phase":   specapi.PhaseRunning,
		"message": "the agent is summarizing " + change.Spec.SystemContext,
	}); err != nil {
		return 0, err
	}

	result, err := c.summarize(ctx, namespace, change)
	if err != nil {
		c.failChange(ctx, namespace, change, err.Error())
		return 0, nil
	}

	message := fmt.Sprintf("wrote %d requirement(s) and %d interface(s) for %s",
		len(result.Draft.Requirements), len(result.Draft.Interfaces), result.Context)
	if dropped := len(result.Dropped); dropped > 0 {
		message += fmt.Sprintf("; dropped %d unresolved code ref(s)", dropped)
	}
	for _, warning := range result.Warnings {
		message += "; " + warning
	}
	if !result.Applied {
		message += "; the spec already said this"
	}
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, name, map[string]any{
		"phase":    specapi.PhaseSucceeded,
		"message":  message,
		"agentLog": tailMessage(result.Draft.Summary),
	}); err != nil {
		return 0, err
	}
	c.log.Info("code to spec done",
		"change", name, "systemcontext", change.Spec.SystemContext, "applied", result.Applied)
	return 0, nil
}

func (c *Controller) summarize(ctx context.Context, namespace string, change *spec.SpecChange) (summarize.Result, error) {
	object, err := c.client.Get(ctx, specapi.SystemContextGVR, namespace, change.Spec.SystemContext)
	if err != nil {
		return summarize.Result{}, fmt.Errorf("specd: read systemcontext %s: %w", change.Spec.SystemContext, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return summarize.Result{}, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return summarize.Result{}, fmt.Errorf("specd: %s is not a SystemContext", change.Spec.SystemContext)
	}
	if systemContext.Spec.Repository == "" {
		return summarize.Result{}, fmt.Errorf("specd: %s names no repository", change.Spec.SystemContext)
	}

	repositoryObject, err := c.client.Get(ctx, specapi.RepositoryGVR, namespace, systemContext.Spec.Repository)
	if err != nil {
		return summarize.Result{}, fmt.Errorf("specd: read repository %s: %w", systemContext.Spec.Repository, err)
	}
	repositoryTyped, err := kcpclient.Typed(repositoryObject)
	if err != nil {
		return summarize.Result{}, err
	}
	repository, ok := repositoryTyped.(*spec.Repository)
	if !ok {
		return summarize.Result{}, fmt.Errorf("specd: %s is not a Repository", systemContext.Spec.Repository)
	}

	agentFor, err := c.agents.AgentFor(repository.PopulateAgent(), repository, repository.WorkPath())
	if err != nil {
		return summarize.Result{}, err
	}

	return summarize.Run(ctx, summarize.Options{
		Cluster:       c.client,
		Namespace:     namespace,
		Context:       systemContext.Name,
		Repository:    repository,
		Agent:         agentFor,
		Writer:        c.opts.Graph,
		Codegraph:     codegraphcli.Runner{Tool: c.opts.Tool, Dir: repository.WorkPath()},
		Budget:        c.opts.Budget,
		NodeLimit:     c.opts.NodeLimit,
		ManagedBudget: c.opts.ManagedBudget,
	})
}

func (c *Controller) failChange(ctx context.Context, namespace string, change *spec.SpecChange, message string) {
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, change.Name, map[string]any{
		"phase":    specapi.PhaseFailed,
		"message":  tailMessage(message),
		"agentLog": tailMessage(message),
	}); err != nil {
		c.log.Error("could not record the failed change", "change", change.Name, "err", err)
		return
	}
	c.log.Warn("code to spec failed", "change", change.Name, "systemcontext", change.Spec.SystemContext, "err", message)
	c.enqueueContext(ctx, namespace, change.Spec.SystemContext)
}

func (c *Controller) enqueueContext(ctx context.Context, namespace, name string) {
	c.queue.Add(key{
		Kind:      specapi.SystemContextKind,
		Cluster:   watch.ClusterOf(ctx),
		Namespace: namespace,
		Name:      name,
	})
}

func tailMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if len(trimmed) <= messageLimit {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-messageLimit:]
}
