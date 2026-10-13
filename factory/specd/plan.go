package specd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/finding"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/plan"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

const (
	PlanConditionValid = "Valid"

	PlanReasonInvalid = "Invalid"
)

// reconcilePlan drives a Plan with the pure decision core in abc/plan: it reads
// the plan, hands the core the repository, the contexts and the changes kcp
// holds, carries out the actions the core returns, and writes the item statuses
// back. It never decides an item phase itself.
func (c *Controller) reconcilePlan(ctx context.Context, namespace, name string) (time.Duration, error) {
	object, err := c.client.Get(ctx, specapi.PlanGVR, namespace, name)
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
	planObject, ok := typed.(*spec.Plan)
	if !ok {
		return 0, fmt.Errorf("specd: %s is not a Plan", name)
	}

	if err := plan.Validate(planObject.Spec); err != nil {
		return 0, c.failPlan(ctx, planObject, namespace, err)
	}

	repository, err := c.planRepository(ctx, namespace, planObject)
	if err != nil {
		return 0, err
	}
	changes, err := c.allChanges(ctx, namespace)
	if err != nil {
		return 0, err
	}
	repositoryName, contexts, head := "", map[string]spec.SystemContext{}, ""
	if repository != nil {
		repositoryName = repository.Name
		contexts, err = c.planContexts(ctx, namespace, repositoryName)
		if err != nil {
			return 0, err
		}
		head = headOf(ctx, repository)
	}

	evaluated := plan.Evaluate(plan.Input{
		Plan:        *planObject,
		Repository:  repositoryName,
		Contexts:    contexts,
		Changes:     changes,
		Head:        head,
		MaxAttempts: c.opts.MaxAttempts,
	})

	requeue := time.Duration(0)
	switch {
	case repository == nil:
		c.log.Info("the plan has no repository to act for; it only gets its status",
			"plan", name, "phase", evaluated.Phase)
	case c.branchStatus(ctx, repository).Mismatch:
		c.log.Info("the plan waits: the checkout is on another branch",
			"plan", name, "repository", repositoryName)
		requeue = c.opts.Resync
	default:
		for _, action := range evaluated.Actions {
			if err := c.carryOut(ctx, namespace, planObject, repository, head, action, &evaluated); err != nil {
				return 0, err
			}
		}
	}

	status := map[string]any{
		"observedGeneration": planObject.GetGeneration(),
		"phase":              evaluated.Phase,
		"items":              evaluated.Items,
		"conditions":         evaluated.Conditions,
	}
	if !specapi.StatusMatches(planObject.Status, status) {
		if _, err := c.client.PatchStatus(ctx, specapi.PlanGVR, namespace, name, status); err != nil {
			return 0, err
		}
		c.log.Info("plan updated", "plan", name, "phase", evaluated.Phase)
	}
	if err := c.moveFindings(ctx, namespace, planObject, evaluated); err != nil {
		return 0, err
	}
	switch evaluated.Phase {
	case plan.PhaseApproved, plan.PhaseRunning:
		requeue = max(requeue, c.opts.Resync)
	}
	return requeue, nil
}

// reconcileFinding gives a finding with no state the state a new finding
// starts in. Everything else about a finding is the plan's to move, so this
// does nothing else. reconcileAgentRun keeps a watched AgentRun from logging
// "no reconciler"; nothing reads it back into a decision yet.
func (c *Controller) reconcileFinding(ctx context.Context, namespace, name string) (time.Duration, error) {
	object, err := c.client.Get(ctx, specapi.FindingGVR, namespace, name)
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
	recorded, ok := typed.(*spec.Finding)
	if !ok {
		return 0, nil
	}
	if recorded.Status.State != "" {
		return 0, nil
	}
	if _, err := c.client.PatchStatus(ctx, specapi.FindingGVR, namespace, name, map[string]any{"state": finding.Open}); err != nil {
		return 0, err
	}
	c.log.Info("finding opened", "finding", name)
	return 0, nil
}

func (c *Controller) reconcileAgentRun(context.Context, string, string) (time.Duration, error) {
	return 0, nil
}

func (c *Controller) failPlan(ctx context.Context, planObject *spec.Plan, namespace string, failure error) error {
	conditions := condition.Copy(planObject.Status.Conditions)
	condition.SetFalse(&conditions, planObject.GetGeneration(), PlanConditionValid, PlanReasonInvalid, failure.Error())
	status := map[string]any{
		"observedGeneration": planObject.GetGeneration(),
		"phase":              plan.PhaseFailed,
		"conditions":         conditions,
	}
	if specapi.StatusMatches(planObject.Status, status) {
		return nil
	}
	_, err := c.client.PatchStatus(ctx, specapi.PlanGVR, namespace, planObject.Name, status)
	return err
}

// planRepository names the repository this instance acts for. One Repository in
// the namespace is it; with several it is the one the plan names. None leaves
// the plan with no repository, so its items are only previewed.
func (c *Controller) planRepository(ctx context.Context, namespace string, planObject *spec.Plan) (*spec.Repository, error) {
	listed, err := c.client.List(ctx, specapi.RepositoryGVR, namespace)
	if err != nil {
		return nil, err
	}
	repositories := []*spec.Repository{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		repository, ok := typed.(*spec.Repository)
		if !ok {
			continue
		}
		repositories = append(repositories, repository)
	}
	if len(repositories) == 1 {
		return repositories[0], nil
	}
	for _, repository := range repositories {
		if repository.Name == planObject.Spec.Repository {
			return repository, nil
		}
	}
	return nil, nil
}

func (c *Controller) planContexts(ctx context.Context, namespace, repository string) (map[string]spec.SystemContext, error) {
	out := map[string]spec.SystemContext{}
	listed, err := c.client.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok || systemContext.Spec.Repository != repository {
			continue
		}
		out[systemContext.Name] = *systemContext
	}
	return out, nil
}

func headOf(ctx context.Context, repository *spec.Repository) string {
	path := repository.WorkPath()
	if path == "" {
		return ""
	}
	head, err := gitrepo.Head(ctx, path)
	if err != nil {
		return ""
	}
	return head
}

func (c *Controller) carryOut(
	ctx context.Context,
	namespace string,
	planObject *spec.Plan,
	repository *spec.Repository,
	head string,
	action plan.Action,
	evaluated *plan.Evaluation,
) error {
	switch action.Kind {
	case plan.ActionApply:
		hashes, err := c.applyPlanEdits(ctx, namespace, planObject, action)
		if err != nil {
			return err
		}
		for index := range evaluated.Items {
			if evaluated.Items[index].ID != action.Item {
				continue
			}
			merged := map[string]string{}
			for name, hash := range evaluated.Items[index].AppliedSpecHashes {
				merged[name] = hash
			}
			for name, hash := range hashes {
				merged[name] = hash
			}
			evaluated.Items[index].AppliedSpecHashes = merged
		}
	case plan.ActionAcceptance:
		status := spec.PlanItemStatus{}
		for _, item := range evaluated.Items {
			if item.ID == action.Item {
				status = item
				break
			}
		}
		finished, err := c.runPlanAcceptance(ctx, repository, head, action, status)
		if err != nil {
			return err
		}
		for index := range evaluated.Items {
			if evaluated.Items[index].ID == action.Item {
				evaluated.Items[index] = finished
			}
		}
	}
	return nil
}

// applyPlanEdits writes each of the action's edits to its SystemContext: the
// spec the edit produces, the plan as its origin, the plan item that asked for
// it, and the units the edit declares. A conflict is returned so the plan is
// retried against a fresher context. specd's own edit detection raises the spec
// to code change from there; the plan never creates a SpecChange itself.
func (c *Controller) applyPlanEdits(ctx context.Context, namespace string, planObject *spec.Plan, action plan.Action) (map[string]string, error) {
	applied := map[string]string{}
	for _, edit := range action.Edits {
		object, err := c.client.Get(ctx, specapi.SystemContextGVR, namespace, edit.Context)
		if err != nil {
			return nil, err
		}
		typed, err := kcpclient.Typed(object)
		if err != nil {
			return nil, err
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok {
			return nil, fmt.Errorf("specd: %s is not a SystemContext", edit.Context)
		}
		recorded, err := authority.Parse(object.GetAnnotations()[specapi.DeclaredAnnotation])
		if err != nil {
			c.log.Warn("the declared annotation does not parse; the plan rewrites it",
				"systemcontext", edit.Context, "err", err)
			recorded = authority.Set{}
		}
		next := plan.ApplyEdit(systemContext.Spec, edit)
		encoded, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&next)
		if err != nil {
			return nil, err
		}
		if err := unstructured.SetNestedMap(object.Object, encoded, "spec"); err != nil {
			return nil, err
		}
		annotations := object.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[specapi.OriginAnnotation] = specapi.OriginPlan
		annotations[specapi.PlanItemAnnotation] = planObject.Name + "/" + action.Item
		annotations[specapi.DeclaredAnnotation] = authority.Format(recorded.Union(plan.EditUnits(edit)))
		object.SetAnnotations(annotations)

		written, err := c.client.Update(ctx, object)
		if err != nil {
			return nil, err
		}
		hash, err := specHashOf(written)
		if err != nil {
			return nil, err
		}
		applied[edit.Context] = hash
		c.log.Info("plan edit applied",
			"plan", planObject.Name, "item", action.Item, "systemcontext", edit.Context, "specHash", hash)
	}
	return applied, nil
}

func specHashOf(object *unstructured.Unstructured) (string, error) {
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return "", err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return "", fmt.Errorf("specd: %s is not a SystemContext", object.GetName())
	}
	return spec.HashSystemContextSpec(systemContext.Spec)
}

// runPlanAcceptance runs the item's acceptance steps at the HEAD the evaluation
// used, in a detached worktree of the checkout at that commit: the steps never
// run in the checkout itself. The root SiblingView creates keeps the replace
// paths of a go.mod that point beside the repository resolvable.
func (c *Controller) runPlanAcceptance(
	ctx context.Context,
	repository *spec.Repository,
	head string,
	action plan.Action,
	status spec.PlanItemStatus,
) (spec.PlanItemStatus, error) {
	repoPath, err := filepath.Abs(repository.WorkPath())
	if err != nil {
		return status, err
	}
	if !gitrepo.IsRepo(ctx, repoPath) {
		return status, fmt.Errorf("specd: %s is not a git working tree", repoPath)
	}
	root, worktree, err := gitrepo.SiblingView(repoPath)
	if err != nil {
		return status, err
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			c.log.Warn("could not remove the acceptance worktree root", "root", root, "err", err)
		}
	}()
	if err := gitrepo.WorktreeAddDetached(ctx, repoPath, worktree, head); err != nil {
		return status, err
	}
	results := realize.RunAcceptance(ctx, action.Acceptance, worktree, 0)
	if err := gitrepo.WorktreeRemove(ctx, repoPath, worktree); err != nil {
		c.log.Warn("could not remove the acceptance worktree", "worktree", worktree, "err", err)
	}
	return plan.Finish(status, head, results, plan.AcceptanceHash(action.Acceptance)), nil
}

// moveFindings records what the plan decided about the findings its items name:
// planned while the plan is approved, resolved with the commit once the item is
// done. A finding the namespace does not hold is skipped.
func (c *Controller) moveFindings(ctx context.Context, namespace string, planObject *spec.Plan, evaluated plan.Evaluation) error {
	names := make([]string, 0, len(evaluated.Planned)+len(evaluated.Resolved))
	for name := range evaluated.Planned {
		names = append(names, name)
	}
	for name := range evaluated.Resolved {
		if _, planned := evaluated.Planned[name]; !planned {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		object, err := c.client.Get(ctx, specapi.FindingGVR, namespace, name)
		if err != nil {
			if kcpclient.IsNotFound(err) {
				c.log.Debug("a plan names a finding the namespace does not hold",
					"plan", planObject.Name, "finding", name)
				continue
			}
			return err
		}
		typed, err := kcpclient.Typed(object)
		if err != nil {
			return err
		}
		recorded, ok := typed.(*spec.Finding)
		if !ok {
			continue
		}
		status := planFindingStatus(planObject, recorded, name, evaluated)
		if status == nil || specapi.StatusMatches(recorded.Status, status) {
			continue
		}
		if _, err := c.client.PatchStatus(ctx, specapi.FindingGVR, namespace, name, status); err != nil {
			return err
		}
		c.log.Info("finding moved", "finding", name, "plan", planObject.Name, "state", status["state"])
	}
	return nil
}

func planFindingStatus(planObject *spec.Plan, recorded *spec.Finding, name string, evaluated plan.Evaluation) map[string]any {
	item := evaluated.Planned[name]
	if item == "" {
		item = recorded.Status.Item
	}
	if commit, resolved := evaluated.Resolved[name]; resolved {
		return map[string]any{
			"state":          finding.Resolved,
			"plan":           planObject.Name,
			"item":           item,
			"resolvedCommit": commit,
		}
	}
	if recorded.Status.State == finding.Resolved || recorded.Status.State == finding.Dismissed {
		return nil
	}
	return map[string]any{
		"state": finding.Planned,
		"plan":  planObject.Name,
		"item":  item,
	}
}
