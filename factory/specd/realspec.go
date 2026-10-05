package specd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/coverage"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

type batchPlan struct {
	members []*spec.SpecChange

	wait time.Duration

	deferred bool
}

func (c *Controller) reconcileSpecToCode(ctx context.Context, namespace string, change *spec.SpecChange) (time.Duration, error) {
	repository, ready, err := c.realizeTarget(ctx, namespace, change)
	if err != nil {
		return 0, err
	}
	if !ready {
		return 0, nil
	}

	plan, err := c.planBatch(ctx, namespace, change, repository)
	if err != nil {
		return 0, err
	}
	if len(plan.members) == 0 {
		switch {
		case plan.wait > 0:
			return plan.wait, nil
		case plan.deferred:
			return c.opts.Resync, nil
		}
		return 0, nil
	}
	return c.runBatch(ctx, namespace, repository, plan)
}

func (c *Controller) planBatch(ctx context.Context, namespace string, change *spec.SpecChange, repository *spec.Repository) (batchPlan, error) {
	changes, err := c.allChanges(ctx, namespace)
	if err != nil {
		return batchPlan{}, err
	}
	refs, err := c.changeRefs(ctx, namespace, changes)
	if err != nil {
		return batchPlan{}, err
	}
	pending := specsync.PendingForRepository(refs, repository.Name)
	leader, ok := specsync.BatchLeader(pending)
	if !ok {
		return batchPlan{}, nil
	}
	if leader.Name != change.Name || specsync.RepositoryBusy(refs, repository.Name) {
		return batchPlan{deferred: true}, nil
	}
	if wait := specsync.BatchGatherWait(leader, time.Now(), c.opts.BatchWindow); wait > 0 {
		return batchPlan{wait: wait}, nil
	}
	byName := make(map[string]*spec.SpecChange, len(changes))
	for index := range changes {
		byName[changes[index].Name] = &changes[index]
	}
	ordered := specsync.OrderBatch(pending)
	members := make([]*spec.SpecChange, 0, len(ordered))
	for _, ref := range ordered {
		if member, found := byName[ref.Name]; found {
			members = append(members, member)
		}
	}
	return batchPlan{members: members}, nil
}

func (c *Controller) changeRefs(ctx context.Context, namespace string, changes []spec.SpecChange) ([]specsync.ChangeRef, error) {
	contexts, err := c.contextObjects(ctx, namespace)
	if err != nil {
		return nil, err
	}
	refs := make([]specsync.ChangeRef, 0, len(changes))
	for _, change := range changes {
		context := contexts[change.Spec.SystemContext]
		refs = append(refs, specsync.ChangeRef{
			Name:          change.Name,
			SystemContext: change.Spec.SystemContext,
			Repository:    context.Spec.Repository,
			Direction:     change.Spec.Direction,
			Phase:         change.Status.Phase,
			Commit:        change.Status.Commit,
			CreatedAt:     change.GetCreationTimestamp().Time,
			DependsOn:     context.Spec.DependsOn,
		})
	}
	return refs, nil
}

func (c *Controller) contextObjects(ctx context.Context, namespace string) (map[string]spec.SystemContext, error) {
	listed, err := c.client.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	contexts := make(map[string]spec.SystemContext, len(listed.Items))
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok {
			continue
		}
		contexts[systemContext.Name] = *systemContext
	}
	return contexts, nil
}

func (c *Controller) runBatch(ctx context.Context, namespace string, repository *spec.Repository, plan batchPlan) (time.Duration, error) {
	members := c.underTheAttemptCap(ctx, namespace, plan.members)
	if len(members) == 0 {
		return 0, nil
	}

	for _, member := range members {
		if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, member.Name, map[string]any{
			"phase":   specapi.PhaseRunning,
			"message": batchRunningMessage(member, members),
		}); err != nil {
			return 0, err
		}
	}

	work, idle, err := c.splitByDelta(ctx, namespace, members)
	if err != nil {
		c.recordBatchFailure(ctx, namespace, members, realize.Result{}, err, len(repository.Spec.Verify) > 0)
		return 0, nil
	}

	result := realize.Result{Context: members[0].Spec.SystemContext, Branch: batchBranch(work, idle, members)}
	if len(work) > 0 {
		var failure error
		result, failure = c.realizeWork(ctx, namespace, repository, work)
		if failure != nil {
			c.recordBatchFailure(ctx, namespace, members, result, failure, len(repository.Spec.Verify) > 0)
			return 0, nil
		}
	}

	for _, member := range idle {
		options, err := c.settleOptions(ctx, namespace, repository, member)
		if err == nil {
			err = realize.Settle(ctx, options)
		}
		if err != nil {
			c.recordBatchFailure(ctx, namespace, members, result, err, len(repository.Spec.Verify) > 0)
			return 0, nil
		}
	}

	c.recordBatchSuccess(ctx, namespace, repository, members, result)
	return 0, nil
}

func (c *Controller) underTheAttemptCap(ctx context.Context, namespace string, members []*spec.SpecChange) []*spec.SpecChange {
	if c.opts.MaxAttempts <= 0 {
		return members
	}
	kept := make([]*spec.SpecChange, 0, len(members))
	for _, member := range members {
		if !withinAttemptCap(member, c.attemptsTaken(ctx, namespace, member), c.opts.MaxAttempts) {
			c.failChange(ctx, namespace, member, fmt.Sprintf("attempt cap: %s already has %d attempts", episodeBase(member), c.opts.MaxAttempts))
			continue
		}
		kept = append(kept, member)
	}
	return kept
}

func (c *Controller) splitByDelta(ctx context.Context, namespace string, members []*spec.SpecChange) ([]*spec.SpecChange, []*spec.SpecChange, error) {
	work := []*spec.SpecChange{}
	idle := []*spec.SpecChange{}
	for _, member := range members {
		changeDelta, err := c.deltaForChange(ctx, namespace, member)
		if err != nil {
			return nil, nil, err
		}
		if changeDelta.Empty() {
			idle = append(idle, member)
			continue
		}
		work = append(work, member)
	}
	return work, idle, nil
}

func (c *Controller) realizeWork(ctx context.Context, namespace string, repository *spec.Repository, members []*spec.SpecChange) (realize.Result, error) {
	result := realize.Result{Context: members[0].Spec.SystemContext}
	var failure error
	for attempt := 0; attempt < 2; attempt++ {
		options, err := c.batchOptions(ctx, namespace, repository, members)
		if err != nil {
			return result, err
		}
		result, failure = realize.Run(ctx, options)
		if failure == nil {
			return result, nil
		}
		moved, sibling := c.movedSinceBase(ctx, namespace, repository, options.Base, changeNames(members))
		if attempt == 0 && moved {
			kind := "the branch moved"
			if sibling {
				kind = "a sibling change landed"
			}
			c.log.Info("retrying once on the new base",
				"repository", repository.Name, "reason", kind, "base", options.Base, "changes", len(members))
			continue
		}
		return result, failure
	}
	return result, failure
}

func (c *Controller) movedSinceBase(ctx context.Context, namespace string, repository *spec.Repository, base string, members []string) (bool, bool) {
	repoPath, err := filepath.Abs(repository.WorkPath())
	if err != nil {
		return false, false
	}
	head, err := gitrepo.Head(ctx, repoPath)
	if err != nil || head == "" || head == base {
		return false, false
	}
	changes, err := c.allChanges(ctx, namespace)
	if err != nil {
		return true, false
	}
	refs, err := c.changeRefs(ctx, namespace, changes)
	if err != nil {
		return true, false
	}
	return true, specsync.SiblingLanded(refs, repository.Name, base, head, members)
}

func (c *Controller) recordBatchSuccess(ctx context.Context, namespace string, repository *spec.Repository, members []*spec.SpecChange, result realize.Result) {
	outside := c.filesOutsideBatch(ctx, namespace, repository, members, result.FilesTouched)
	for _, member := range members {
		c.writeFullLog(member.Name, result)
		status := map[string]any{
			"phase":          specapi.PhaseSucceeded,
			"branch":         result.Branch,
			"verifyExitCode": result.VerifyExitCode,
			"message":        batchSuccessMessage(member, members, result),
			"agentLog":       tailMessage(changeAgentLog(result, len(repository.Spec.Verify) > 0)),
		}
		if result.Commit != "" {
			status["commit"] = result.Commit
		}
		if len(result.FilesTouched) > 0 {
			status["filesTouched"] = result.FilesTouched
		}
		if len(result.Acceptance) > 0 {
			status["acceptance"] = result.Acceptance
		}
		conditions := condition.Copy(member.Status.Conditions)
		conditionsChanged := false
		if verdicts, found := result.Coverage[member.Name]; found && len(verdicts) > 0 {
			status["requirementCoverage"] = verdicts
			missing := coverage.Missing(verdicts)
			if len(missing) > 0 {
				condition.SetTrue(&conditions, member.GetGeneration(), specapi.ConditionRequirementsUnimplemented,
					specapi.ReasonRequirementsMissing, "unimplemented: "+strings.Join(verdictIDs(missing), ", "))
				status["message"] = batchSuccessMessage(member, members, result) +
					"; requirements unimplemented: " + strings.Join(verdictIDs(missing), ", ")
			} else {
				condition.SetFalse(&conditions, member.GetGeneration(), specapi.ConditionRequirementsUnimplemented,
					specapi.ReasonRequirementsImplemented, "every added or changed requirement is implemented")
			}
			conditionsChanged = true
		}
		if len(outside) > 0 {
			condition.SetTrue(&conditions, member.GetGeneration(), specapi.ConditionFilesOutsideContext,
				specapi.ReasonFilesOutsideContext, outsideContextsMessage(outside))
			conditionsChanged = true
		}
		if conditionsChanged {
			status["conditions"] = conditions
		}
		if record, ok := batchProgress(members, result); ok {
			member.Status.AppendProgress(record)
			status["progress"] = member.Status.Progress
		}
		if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, member.Name, status); err != nil {
			c.log.Error("could not record the realized change", "change", member.Name, "err", err)
		}
	}
	logged := []any{"repository", repository.Name, "changes", len(members),
		"branch", result.Branch, "commit", result.Commit, "files", len(result.FilesTouched)}
	if result.CoverageError != "" {
		logged = append(logged, "coverageError", result.CoverageError)
	}
	if len(outside) > 0 {
		logged = append(logged, "filesOutsideContext", len(outside))
		c.log.Warn("the realize touched files another context owns",
			"repository", repository.Name, "files", outsideContextsMessage(outside))
	}
	c.log.Info("spec to code done", logged...)
}

func (c *Controller) filesOutsideBatch(ctx context.Context, namespace string, repository *spec.Repository, members []*spec.SpecChange, touched []string) map[string][]string {
	if len(touched) == 0 {
		return nil
	}
	contexts, err := c.contextObjects(ctx, namespace)
	if err != nil {
		return nil
	}
	observed := map[string]spec.ObservedFacts{}
	for name, context := range contexts {
		if context.Spec.Repository == repository.Name {
			observed[name] = context.Status.Observed
		}
	}
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.Spec.SystemContext)
	}
	return specsync.FilesOwnedElsewhere(touched, names, observed)
}

func outsideContextsMessage(outside map[string][]string) string {
	files := make([]string, 0, len(outside))
	for file := range outside {
		files = append(files, file)
	}
	sort.Strings(files)
	parts := make([]string, 0, len(files))
	for _, file := range files {
		parts = append(parts, file+" ("+strings.Join(outside[file], ", ")+")")
	}
	message := "the realize touched files another context owns: " + strings.Join(parts, ", ")
	if len(message) > messageLimit {
		message = message[:messageLimit]
	}
	return message
}

func verdictIDs(verdicts []coverage.Verdict) []string {
	out := make([]string, 0, len(verdicts))
	for _, verdict := range verdicts {
		out = append(out, verdict.ID)
	}
	return out
}

func (c *Controller) recordBatchFailure(ctx context.Context, namespace string, members []*spec.SpecChange, result realize.Result, failure error, verifyConfigured bool) {
	message := failure.Error()
	if verifyErr, ok := errors.AsType[*realize.VerifyError](failure); ok {
		message = fmt.Sprintf("verify exited %d: %s", verifyErr.ExitCode, tailMessage(verifyErr.Output))
	}
	if acceptanceErr, ok := errors.AsType[*realize.AcceptanceError](failure); ok {
		message = fmt.Sprintf("acceptance %s failed (gate): exit %d: %s",
			acceptanceErr.Result.Name, acceptanceErr.Result.ExitCode, tailMessage(acceptanceErr.Result.OutputTail))
	}
	for _, member := range members {
		c.writeFullLog(member.Name, result)
		status := map[string]any{
			"phase":          specapi.PhaseFailed,
			"branch":         result.Branch,
			"verifyExitCode": result.VerifyExitCode,
			"message":        tailMessage(message),
			"agentLog":       tailMessage(changeAgentLog(result, verifyConfigured)),
		}
		if len(result.Acceptance) > 0 {
			status["acceptance"] = result.Acceptance
		}
		if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, member.Name, status); err != nil {
			c.log.Error("could not record the failed change", "change", member.Name, "err", err)
			continue
		}
		c.enqueueContext(ctx, namespace, member.Spec.SystemContext)
	}
	c.log.Warn("spec to code failed",
		"context", members[0].Spec.SystemContext, "changes", len(members), "err", message)
}

func (c *Controller) batchOptions(ctx context.Context, namespace string, repository *spec.Repository, members []*spec.SpecChange) (realize.Options, error) {
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
	worktreeRoot, worktree, err := gitrepo.SiblingView(repoPath)
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

	leader := members[0]
	options := realize.Options{
		Cluster:       c.client,
		Namespace:     namespace,
		Context:       leader.Spec.SystemContext,
		Change:        leader.Name,
		Repository:    &scoped,
		Agent:         built,
		WorktreeRoot:  worktreeRoot,
		Codegraph:     codegraphcli.Runner{Tool: c.opts.Tool, Dir: repoPath},
		Writer:        c.opts.Graph,
		Budget:        c.opts.Budget,
		NodeLimit:     c.opts.NodeLimit,
		ManagedBudget: c.opts.ManagedBudget,
		Worktree:      worktree,
		Branch:        realizeBranch(leader.Spec.SystemContext, leader.Spec.ToSpecHash),
		Base:          base,
		Instruction:   c.retryInstruction(ctx, namespace, leader),
		Tool:          c.opts.Tool,
		Coverage:      c.coverageJudge(repository, repoPath),
	}
	for _, member := range members {
		changeDelta, err := c.deltaForChange(ctx, namespace, member)
		if err != nil {
			return realize.Options{}, err
		}
		options.Members = append(options.Members, realize.Member{
			Context: member.Spec.SystemContext,
			Change:  member.Name,
			Delta:   changeDelta,
		})
	}
	return options, nil
}

// coverageJudge asks the same model the summarize path uses, one call per
// change, whether the realized diff implements each added or changed
// requirement. It is nil for an agent this controller cannot question that way,
// so coverage is off when no model is configured.
func (c *Controller) coverageJudge(repository *spec.Repository, dir string) *coverage.Judge {
	if !c.agents.ModelCoverageFor(repository) {
		return nil
	}
	return coverage.New(coverage.Options{
		Command: c.opts.AgentCommand,
		Args:    c.opts.AgentArgs,
		Dir:     dir,
		Timeout: c.opts.AgentTimeout,
	})
}

func (c *Controller) settleOptions(ctx context.Context, namespace string, repository *spec.Repository, member *spec.SpecChange) (realize.Options, error) {
	repoPath, err := filepath.Abs(repository.WorkPath())
	if err != nil {
		return realize.Options{}, err
	}
	if !gitrepo.IsRepo(ctx, repoPath) {
		return realize.Options{}, fmt.Errorf("specd: %s is not a git working tree", repoPath)
	}
	scoped := *repository
	scoped.Spec.Path = repoPath
	scoped.Status.ResolvedPath = repoPath
	return realize.Options{
		Cluster:       c.client,
		Namespace:     namespace,
		Context:       member.Spec.SystemContext,
		Change:        member.Name,
		Repository:    &scoped,
		Codegraph:     codegraphcli.Runner{Tool: c.opts.Tool, Dir: repoPath},
		Writer:        c.opts.Graph,
		Budget:        c.opts.Budget,
		NodeLimit:     c.opts.NodeLimit,
		ManagedBudget: c.opts.ManagedBudget,
		Tool:          c.opts.Tool,
	}, nil
}

func (c *Controller) deltaForChange(ctx context.Context, namespace string, change *spec.SpecChange) (spec.Delta, error) {
	if change.Spec.Delta != nil {
		return *change.Spec.Delta, nil
	}
	systemContext, err := c.readContext(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		return spec.Delta{}, err
	}
	return delta.Diff(realizedSpecOf(systemContext), systemContext.Spec), nil
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
	if status := c.branchStatus(ctx, repository); status.Mismatch {
		c.log.Info("a spec to code change waits: the checkout is on another branch",
			"change", change.Name, "repository", repository.Name, "message", status.Message)
		return repository, false, nil
	}
	if !c.agents.ConfiguredFor(repository) {
		c.log.Info("leaving a spec to code change for a human: no agent is configured",
			"change", change.Name, "repository", repository.Name)
		return repository, false, nil
	}
	return repository, true, nil
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

func changeNames(members []*spec.SpecChange) []string {
	out := make([]string, 0, len(members))
	for _, member := range members {
		out = append(out, member.Name)
	}
	return out
}

func batchRunningMessage(member *spec.SpecChange, members []*spec.SpecChange) string {
	if len(members) == 1 {
		return "the agent is realizing " + member.Spec.SystemContext
	}
	return fmt.Sprintf("the agent is realizing %s in a batch of %d led by %s",
		member.Spec.SystemContext, len(members), members[0].Spec.SystemContext)
}

func batchSuccessMessage(member *spec.SpecChange, members []*spec.SpecChange, result realize.Result) string {
	who := "realized " + member.Spec.SystemContext
	if len(members) > 1 {
		who = fmt.Sprintf("realized %s in a batch of %d led by %s",
			member.Spec.SystemContext, len(members), members[0].Spec.SystemContext)
	}
	if !result.Landed {
		return who + "; the agent changed nothing, the baseline moved"
	}
	return fmt.Sprintf("%s on %s: %d file(s) touched, verify exit %d",
		who, result.Branch, len(result.FilesTouched), result.VerifyExitCode)
}

func batchBranch(work, idle, members []*spec.SpecChange) string {
	if len(work) > 0 {
		return realizeBranch(work[0].Spec.SystemContext, work[0].Spec.ToSpecHash)
	}
	if len(idle) > 0 {
		return realizeBranch(idle[0].Spec.SystemContext, idle[0].Spec.ToSpecHash)
	}
	return realizeBranch(members[0].Spec.SystemContext, members[0].Spec.ToSpecHash)
}

func batchProgress(members []*spec.SpecChange, result realize.Result) (spec.ProgressRecord, bool) {
	if len(members) < 2 {
		return spec.ProgressRecord{}, false
	}
	return spec.ProgressRecord{
		Turn:  1,
		Tool:  "realize",
		Files: result.FilesTouched,
		Note: fmt.Sprintf("batch of %d led by %s, commit %s",
			len(members), members[0].Spec.SystemContext, shortenHash(result.Commit)),
		At: time.Now().UTC().Format(time.RFC3339),
	}, true
}
