package specd

import (
	"context"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/watch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/procowner"
)

// RecoveredReason is the message a change carries when specd finds it in flight
// with no live driver: the process that ran it died, so the work has to be
// picked up again.
const RecoveredReason = "specd restarted mid-realize"

// RecoveredPolicyReason is the message a PolicyChange carries when specd finds
// it claimed by a process that is gone and drafts it again.
const RecoveredPolicyReason = "specd restarted mid-draft"

var processAlive = procowner.Alive

// recoverOrphans sweeps the workspace once at startup, before the workers run,
// so a change the previous specd left in flight is re-driven instead of
// blocking the repository's batch queue.
func (c *Controller) recoverOrphans(ctx context.Context) {
	changes, err := c.allChanges(ctx, c.opts.Namespace)
	if err != nil {
		c.log.Warn("could not sweep for orphaned changes at startup", "err", err)
		return
	}
	for index := range changes {
		change := &changes[index]
		if change.Status.Phase != specapi.PhaseRunning {
			continue
		}
		if !c.orphaned(change.Status.Owner, change.Status.OwnerPid) {
			continue
		}
		if err := c.recoverRunningChange(ctx, c.opts.Namespace, change); err != nil {
			c.log.Error("could not recover an orphaned spec change", "change", change.Name, "err", err)
		}
	}
	c.recoverOrphanedPolicyChanges(ctx, c.opts.Namespace)
}

func (c *Controller) orphaned(owner string, pid int) bool {
	return procowner.Orphaned(c.owner, owner, pid, processAlive)
}

// recoverRunningChange moves a Running change whose driver is gone out of the
// way: the stale realize worktree and branch are cleaned up, the change is
// failed with the reason, and the next attempt is created Pending so the
// repository's queue moves again. The attempt is counted, because the new
// change grows the episode and the attempt cap still applies to it.
func (c *Controller) recoverRunningChange(ctx context.Context, namespace string, change *spec.SpecChange) error {
	c.cleanupRealizeWorktree(ctx, namespace, change)

	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, change.Name, map[string]any{
		"phase":    specapi.PhaseFailed,
		"message":  RecoveredReason,
		"agentLog": RecoveredReason,
		"owner":    nil,
		"ownerPid": nil,
	}); err != nil {
		return err
	}

	changes, err := c.allChanges(ctx, namespace)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(changes))
	for index := range changes {
		names = append(names, changes[index].Name)
	}
	base := spec.EpisodeBase(*change)
	if spec.EpisodeOpen(changes, base) {
		c.log.Info("an attempt of the recovered episode is already queued", "change", change.Name, "episode", base)
		return nil
	}
	next := &spec.SpecChange{
		TypeMeta: metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.SpecChangeKind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.NextChangeName(names, base),
			Namespace: namespace,
		},
		Spec: change.Spec,
	}
	object, err := kcpclient.Unstructured(next)
	if err != nil {
		return err
	}
	if _, err := c.client.Apply(ctx, object); err != nil {
		return err
	}
	attempt := spec.AttemptCount(names, base) + 1
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, next.Name, map[string]any{
		"phase":   specapi.PhasePending,
		"attempt": attempt,
		"message": "requeued: " + RecoveredReason,
	}); err != nil {
		return err
	}
	c.log.Warn("recovered a change whose specd died mid-realize",
		"change", change.Name, "requeued", next.Name, "attempt", attempt)
	c.enqueueChange(ctx, namespace, next.Name)
	return nil
}

// cleanupRealizeWorktree removes what a killed realize left in the repository:
// its temporary worktree, which still holds the realize branch, and the branch
// itself. A worktree that still holds the branch makes the next realize fail
// with "cannot force update the branch ... used by worktree", so this runs
// before the change is re-driven.
func (c *Controller) cleanupRealizeWorktree(ctx context.Context, namespace string, change *spec.SpecChange) {
	if change.Spec.Direction != specapi.DirectionSpecToCode {
		return
	}
	repository := c.changeRepository(ctx, namespace, change)
	if repository == nil || repository.WorkPath() == "" {
		return
	}
	repoPath, err := filepath.Abs(repository.WorkPath())
	if err != nil {
		return
	}
	if !gitrepo.IsRepo(ctx, repoPath) {
		return
	}
	if err := gitrepo.PruneWorktrees(ctx, repoPath); err != nil {
		c.log.Warn("could not prune the stale realize worktrees",
			"repository", repository.Name, "err", err)
	}
	branch := realizeBranch(change.Spec.SystemContext, change.Spec.ToSpecHash)
	if err := gitrepo.DeleteBranchIfExists(ctx, repoPath, branch); err != nil {
		c.log.Warn("could not delete the stale realize branch",
			"repository", repository.Name, "branch", branch, "err", err)
	}
}

func (c *Controller) enqueueChange(ctx context.Context, namespace, name string) {
	c.queue.Add(key{
		Kind:      specapi.SpecChangeKind,
		Cluster:   watch.ClusterOf(ctx),
		Namespace: namespace,
		Name:      name,
	})
}

// recoverOrphanedPolicyChanges releases the claim a dead specd left on a
// PolicyChange. Drafting and Testing are already re-driven by the normal
// reconcile, so releasing the claim is enough: the work is picked up again
// instead of being skipped as another live process's.
func (c *Controller) recoverOrphanedPolicyChanges(ctx context.Context, namespace string) {
	listed, err := c.client.List(ctx, specapi.PolicyChangeGVR, namespace)
	if err != nil {
		c.log.Warn("could not sweep for orphaned policy changes at startup", "err", err)
		return
	}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			continue
		}
		change, ok := typed.(*policy.PolicyChange)
		if !ok {
			continue
		}
		if !c.orphaned(change.Status.Owner, change.Status.OwnerPid) {
			continue
		}
		if _, err := c.client.PatchStatus(ctx, specapi.PolicyChangeGVR, namespace, change.Name, map[string]any{
			"owner":    nil,
			"ownerPid": nil,
		}); err != nil {
			c.log.Error("could not release an orphaned policy change", "change", change.Name, "err", err)
			continue
		}
		c.log.Warn("released the claim a dead specd left on a policy change", "change", change.Name)
		c.queue.Add(key{
			Kind:      specapi.PolicyChangeKind,
			Cluster:   watch.ClusterOf(ctx),
			Namespace: namespace,
			Name:      change.Name,
		})
	}
}
