package specd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
)

func (c *Controller) reconcileSpecToCode(ctx context.Context, namespace, name string, change *spec.SpecChange) (time.Duration, error) {
	repository, ready, err := c.realizeTarget(ctx, namespace, change)
	if err != nil {
		return 0, err
	}
	if !ready {
		return 0, nil
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

	options, err := c.realizeOptions(ctx, namespace, change, repository)
	if err != nil {
		return 0, err
	}

	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, name, map[string]any{
		"phase":   specapi.PhaseRunning,
		"message": "the agent is realizing " + change.Spec.SystemContext,
	}); err != nil {
		return 0, err
	}

	if options.Delta.Empty() {
		if err := realize.Settle(ctx, options); err != nil {
			c.failChange(ctx, namespace, change, err.Error())
			return 0, nil
		}
		if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, name, map[string]any{
			"phase":   specapi.PhaseSucceeded,
			"message": "the spec already says this; the baseline moved",
		}); err != nil {
			return 0, err
		}
		return 0, nil
	}

	result, err := realize.Run(ctx, options)
	if err != nil {
		c.recordRealizeFailure(ctx, namespace, change, result, err)
		return 0, nil
	}

	message := fmt.Sprintf("realized %s on %s: %d file(s) touched, verify exit %d",
		change.Spec.SystemContext, result.Branch, len(result.FilesTouched), result.VerifyExitCode)
	if !result.Landed {
		message = "the agent changed nothing; the baseline moved"
	}
	status := map[string]any{
		"phase":          specapi.PhaseSucceeded,
		"branch":         result.Branch,
		"verifyExitCode": result.VerifyExitCode,
		"message":        message,
		"agentLog":       tailMessage(result.Agent.Log + "\n" + result.VerifyOutput),
	}
	if result.Commit != "" {
		status["commit"] = result.Commit
	}
	if len(result.FilesTouched) > 0 {
		status["filesTouched"] = result.FilesTouched
	}
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, name, status); err != nil {
		return 0, err
	}
	c.log.Info("spec to code done",
		"change", name, "systemcontext", change.Spec.SystemContext,
		"branch", result.Branch, "commit", result.Commit, "files", len(result.FilesTouched))
	return 0, nil
}

func (c *Controller) recordRealizeFailure(ctx context.Context, namespace string, change *spec.SpecChange, result realize.Result, failure error) {
	message := failure.Error()
	log := result.Agent.Log
	log = log + "\n" + result.VerifyOutput
	if verifyErr, ok := errors.AsType[*realize.VerifyError](failure); ok {
		message = fmt.Sprintf("verify exited %d: %s", verifyErr.ExitCode, tailMessage(verifyErr.Output))
	}
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, change.Name, map[string]any{
		"phase":          specapi.PhaseFailed,
		"branch":         result.Branch,
		"verifyExitCode": result.VerifyExitCode,
		"message":        tailMessage(message),
		"agentLog":       tailMessage(log),
	}); err != nil {
		c.log.Error("could not record the failed change", "change", change.Name, "err", err)
		return
	}
	c.log.Warn("spec to code failed",
		"change", change.Name, "systemcontext", change.Spec.SystemContext, "err", message)
	c.enqueueContext(ctx, namespace, change.Spec.SystemContext)
}

func (c *Controller) realizeTarget(ctx context.Context, namespace string, change *spec.SpecChange) (*spec.Repository, bool, error) {
	systemContext, err := c.readContext(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		if kcpclient.IsNotFound(err) {
			c.log.Warn("a spec to code change names a context that does not exist",
				"change", change.Name, "systemcontext", change.Spec.SystemContext)
			return nil, false, nil
		}
		return nil, false, err
	}
	if systemContext.Spec.Repository == "" {
		return nil, false, fmt.Errorf("specd: %s names no repository", change.Spec.SystemContext)
	}
	repository, err := c.readRepository(ctx, namespace, systemContext.Spec.Repository)
	if err != nil {
		if kcpclient.IsNotFound(err) {
			c.log.Warn("a spec to code change names a repository that does not exist",
				"change", change.Name, "repository", systemContext.Spec.Repository)
			return nil, false, nil
		}
		return nil, false, err
	}
	if repository.WorkPath() == "" {
		return nil, false, fmt.Errorf("specd: repository %s names no path", repository.Name)
	}
	if !c.agents.ConfiguredFor(repository) {
		c.log.Info("leaving a spec to code change for a human: no agent is configured",
			"change", change.Name, "repository", repository.Name)
		return repository, false, nil
	}
	return repository, true, nil
}

func (c *Controller) realizeOptions(ctx context.Context, namespace string, change *spec.SpecChange, repository *spec.Repository) (realize.Options, error) {
	systemContext, err := c.readContext(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		return realize.Options{}, err
	}
	repoPath, err := filepath.Abs(repository.WorkPath())
	if err != nil {
		return realize.Options{}, err
	}
	if !gitrepo.IsRepo(ctx, repoPath) {
		return realize.Options{}, fmt.Errorf("specd: %s is not a git working tree", repoPath)
	}
	base, err := gitrepo.Head(ctx, repoPath)
	if err != nil {
		return realize.Options{}, err
	}
	worktree, err := worktreeDir(change.Name)
	if err != nil {
		return realize.Options{}, err
	}
	scoped := *repository
	scoped.Spec.Path = repoPath
	scoped.Status.ResolvedPath = repoPath
	built, err := c.agents.Agent(&scoped, worktree)
	if err != nil {
		return realize.Options{}, err
	}

	changeDelta := spec.Delta{}
	if change.Spec.Delta != nil {
		changeDelta = *change.Spec.Delta
	} else {
		changeDelta = delta.Diff(realizedSpecOf(systemContext), systemContext.Spec)
	}

	return realize.Options{
		Cluster:       c.client,
		Namespace:     namespace,
		Context:       systemContext.Name,
		Change:        change.Name,
		Repository:    &scoped,
		Agent:         built,
		Delta:         changeDelta,
		Codegraph:     codegraphcli.Runner{Tool: c.opts.Tool, Dir: repoPath},
		Writer:        c.opts.Graph,
		Budget:        c.opts.Budget,
		NodeLimit:     c.opts.NodeLimit,
		ManagedBudget: c.opts.ManagedBudget,
		Worktree:      worktree,
		Branch:        realizeBranch(systemContext.Name, change.Spec.ToSpecHash),
		Base:          base,
		Instruction:   c.retryInstruction(ctx, namespace, change),
		Tool:          c.opts.Tool,
	}, nil
}

func (c *Controller) retryInstruction(ctx context.Context, namespace string, change *spec.SpecChange) string {
	changes, err := c.changesFor(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		return ""
	}
	base := episodeBase(change)
	newest := ""
	newestAt := time.Time{}
	for _, recorded := range changes {
		if recorded.Status.Phase != specapi.PhaseFailed {
			continue
		}
		if recorded.Name != base && !strings.HasPrefix(recorded.Name, base+"-a") {
			continue
		}
		if created := recorded.GetCreationTimestamp().Time; created.After(newestAt) {
			newestAt = created
			newest = recorded.Status.AgentLog
		}
	}
	if newest == "" {
		return ""
	}
	return "A previous attempt at this change failed verification. Its output was:\n\n" + tailMessage(newest)
}

func (c *Controller) readContext(ctx context.Context, namespace, name string) (*spec.SystemContext, error) {
	object, err := c.client.Get(ctx, specapi.SystemContextGVR, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("specd: read systemcontext %s: %w", name, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return nil, fmt.Errorf("specd: %s is not a SystemContext", name)
	}
	return systemContext, nil
}

func (c *Controller) readRepository(ctx context.Context, namespace, name string) (*spec.Repository, error) {
	object, err := c.client.Get(ctx, specapi.RepositoryGVR, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("specd: read repository %s: %w", name, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		return nil, fmt.Errorf("specd: %s is not a Repository", name)
	}
	return repository, nil
}

func realizedSpecOf(systemContext *spec.SystemContext) spec.SystemContextSpec {
	if systemContext.Status.RealizedSpec != nil {
		return *systemContext.Status.RealizedSpec
	}
	return spec.SystemContextSpec{Repository: systemContext.Spec.Repository, Upstream: systemContext.Spec.Upstream}
}

func realizeBranch(context, specHash string) string {
	return gitrepo.BranchPrefix + context + "/" + shortenHash(specHash)
}

func shortenHash(hash string) string {
	if len(hash) < 8 {
		return hash
	}
	return hash[:8]
}

func worktreeDir(changeName string) (string, error) {
	root, err := os.MkdirTemp("", "specd-worktree-")
	if err != nil {
		return "", fmt.Errorf("specd: create a worktree directory: %w", err)
	}
	return filepath.Join(root, changeName), nil
}
