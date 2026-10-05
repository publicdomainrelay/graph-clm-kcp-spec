package specd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policykcp"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

const StatusViolationLimit = 50

// policyBranchOf is the repository part of the policy branch: the override the
// repository spec names, else the repository name.
func policyBranchOf(repository *spec.Repository) string {
	if repository.Spec.Policy != nil && repository.Spec.Policy.Branch != "" {
		return repository.Spec.Policy.Branch
	}
	return repository.Name
}

func repositoryPolicy(repository *spec.Repository) policy.RepositoryPolicy {
	out := policy.RepositoryPolicy{}
	if repository.Spec.Policy == nil {
		return out
	}
	out.Branch = repository.Spec.Policy.Branch
	out.Enforcement = policy.Enforcement(repository.Spec.Policy.Enforcement)
	out.Disabled = repository.Spec.Policy.Disabled
	return out
}

// PolicyGate is what a realize needs to run the policy gate: the library, the
// repository's cap and the operator's one-shot overrides.
type PolicyGate struct {
	Library policy.Library

	Repository policy.RepositoryPolicy

	Overrides []policy.Override

	Commit string

	Branch string
}

// loadPolicyGate reads the policy branch of a repository for the realize gate.
// A repository without a policy branch gets no gate.
func (c *Controller) loadPolicyGate(ctx context.Context, repository *spec.Repository) (*PolicyGate, error) {
	path := repository.WorkPath()
	if path == "" {
		return nil, nil
	}
	store := oagit.Store{Repo: path}
	ref := policy.RefFor(policyBranchOf(repository), repository.Spec.Branch, store.DefaultBranch(ctx))
	library, commit, err := policygit.Read(ctx, store, ref)
	if err != nil {
		return nil, err
	}
	if len(library.Templates) == 0 && len(library.Constraints) == 0 {
		return nil, nil
	}
	gate := &PolicyGate{
		Library:    library,
		Repository: repositoryPolicy(repository),
		Commit:     commit,
		Branch:     repository.Spec.Branch,
	}
	for _, override := range repository.Spec.AcceptanceOverrides {
		if parsed, ok := policy.ParseOverride(override.Step, override.Reason, override.By); ok {
			gate.Overrides = append(gate.Overrides, parsed)
		}
	}
	return gate, nil
}

// episodeWasPolicyDenied reports whether an earlier attempt of this episode
// failed the policy gate, so the exhausted attempt is recorded as PolicyDenied
// rather than as a plain attempt cap.
func (c *Controller) episodeWasPolicyDenied(ctx context.Context, namespace string, change *spec.SpecChange) bool {
	changes, err := c.changesFor(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		return false
	}
	base := episodeBase(change)
	for _, recorded := range changes {
		if recorded.Name == change.Name || recorded.Status.Phase != specapi.PhaseFailed {
			continue
		}
		if recorded.Name != base && !strings.HasPrefix(recorded.Name, base+"-a") {
			continue
		}
		if recorded.Status.Policy != nil && len(recorded.Status.Policy.Denied) > 0 {
			return true
		}
	}
	return false
}

// realizePolicyGate builds the gate options for a batch: the library, the
// repository's cap, the operator's overrides, and the SpecChanges and
// SystemContexts the evaluation reviews and reads.
func (c *Controller) realizePolicyGate(ctx context.Context, namespace string, repository *spec.Repository, members []*spec.SpecChange, base string) (*realize.PolicyGateOptions, error) {
	gate, err := c.loadPolicyGate(ctx, repository)
	if err != nil || gate == nil {
		return nil, err
	}
	contexts, err := c.repositoryContexts(ctx, namespace, repository.Name)
	if err != nil {
		return nil, err
	}
	reviewed := make([]*unstructured.Unstructured, 0, len(members))
	for _, member := range members {
		object, err := kcpclient.Unstructured(member)
		if err != nil {
			return nil, err
		}
		reviewed = append(reviewed, object)
	}
	repositoryObject, err := kcpclient.Unstructured(repository)
	if err != nil {
		return nil, err
	}
	inventory := []*unstructured.Unstructured{repositoryObject}
	for index := range contexts {
		object, err := kcpclient.Unstructured(&contexts[index])
		if err != nil {
			return nil, err
		}
		inventory = append(inventory, object)
	}
	return &realize.PolicyGateOptions{
		Library:        gate.Library,
		Repository:     gate.Repository,
		Overrides:      gate.Overrides,
		Commit:         base,
		Branch:         repository.Spec.Branch,
		TestGlobs:      gate.Library.Manifest.TestGlobs,
		Contexts:       codegraphfacts.ContextsByFile(contexts),
		ModelContexts:  modelContexts(contexts),
		MemberCacheDir: c.opts.CacheDir,
		Reviewed:       reviewed,
		Inventory:      inventory,
	}, nil
}

// gateLock is the member pins the library's own policies.lock carries, so an
// audit reports the commit a member was pinned to when the branch was built.
func gateLock(library policy.Library) *policy.PackLock {
	data, ok := library.Files[policy.LockPath]
	if !ok {
		return nil
	}
	lock, err := policyeval.ParseLock(data)
	if err != nil {
		return nil
	}
	return &lock
}

// modelContexts is the ArchitectureModel's view of a repository's contexts:
// the labels and the declared interactions a portable rule reads.
func modelContexts(contexts []spec.SystemContext) []policy.ModelContext {
	out := make([]policy.ModelContext, 0, len(contexts))
	for _, context := range contexts {
		out = append(out, policy.ModelContext{
			Name:         context.Name,
			Labels:       context.Labels,
			Interactions: systemContextInteractions(context.Spec.Interactions),
		})
	}
	return out
}

func systemContextInteractions(interactions []spec.Interaction) []policy.DeclaredInteraction {
	out := make([]policy.DeclaredInteraction, 0, len(interactions))
	for _, interaction := range interactions {
		out = append(out, policy.DeclaredInteraction{
			Peer:      interaction.Peer,
			Initiator: interaction.Initiator,
			Channel:   interaction.Channel,
			Carries:   interaction.Carries,
			Purpose:   interaction.Purpose,
			Level:     string(spec.CanonicalInteraction(interaction).Level),
			Forbidden: interaction.Forbidden,
		})
	}
	return out
}

// reconcileRepositoryPolicy keeps kcp and the policy branch in step and audits
// the indexed commit. It never fails the repository: a policy problem lands in
// the Repository condition instead.
func (c *Controller) reconcileRepositoryPolicy(ctx context.Context, namespace string, repository *spec.Repository, path, commit string) {
	cluster, ok := c.client.(policykcp.Cluster)
	if !ok {
		return
	}
	// The caller read this repository before a populate that can take seconds.
	// A stale status would make the sync believe kcp has never held the policy
	// branch, restore over it with a prune, and drop a policy that landed while
	// the populate ran; a policy change in flight is the same hazard, so the
	// sync waits for it.
	current, err := c.readRepository(ctx, namespace, repository.Name)
	if err != nil {
		c.log.Error("could not re-read the repository before the policy sync", "repository", repository.Name, "err", err)
		return
	}
	repository = current
	change, err := c.policyChangeInFlight(ctx, namespace, repository.Name)
	if err != nil {
		return
	}
	if change != "" {
		c.log.Debug("the policy sync waits for a policy change",
			"repository", repository.Name, "change", change)
		return
	}
	store := oagit.Store{Repo: path}
	ref := policy.RefFor(policyBranchOf(repository), repository.Spec.Branch, store.DefaultBranch(ctx))
	library, policyCommit, err := policygit.Read(ctx, store, ref)
	if err != nil {
		c.log.Error("the policy branch could not be read", "repository", repository.Name, "branch", ref, "err", err)
		c.setPolicyCondition(ctx, repository, namespace, metav1.ConditionFalse, specapi.ReasonPolicyInvalid, err.Error())
		return
	}
	if policyCommit == "" {
		return
	}

	if repository.Status.Policy == nil || repository.Status.Policy.PolicyCommit == "" {
		// No prune: kcp serves every repository's policies, so pruning to one
		// repository's library would delete another's, and the restore reads the
		// branch before an apply that may land while it runs, so a prune would
		// delete the policy that apply just wrote.
		engine, err := policyeval.NewEngine(ctx, library, nil)
		if err != nil {
			c.log.Error("policy restore failed", "repository", repository.Name, "branch", ref, "err", err)
			c.setPolicyCondition(ctx, repository, namespace, metav1.ConditionFalse, specapi.ReasonPolicyInvalid, err.Error())
			return
		}
		if err := policykcp.Apply(ctx, cluster, library, policykcp.ApplyOptions{CRDs: engine}); err != nil {
			c.log.Error("policy restore failed", "repository", repository.Name, "branch", ref, "err", err)
			c.setPolicyCondition(ctx, repository, namespace, metav1.ConditionFalse, specapi.ReasonPolicyInvalid,
				"restore from "+ref+": "+err.Error())
			return
		}
		c.log.Info("policy restored from the branch", "repository", repository.Name, "branch", ref, "commit", shortenHash(policyCommit))
	} else if kcpLibrary, err := policykcp.Read(ctx, cluster); err == nil && policykcp.Distinct(kcpLibrary, library) {
		if err := c.persistPolicy(ctx, store, ref, repository, kcpLibrary, library); err != nil {
			c.log.Error("policy persist failed", "repository", repository.Name, "branch", ref, "err", err)
			c.setPolicyCondition(ctx, repository, namespace, metav1.ConditionFalse, specapi.ReasonPolicyInvalid,
				"persist to "+ref+": "+err.Error())
			return
		}
		library, policyCommit, err = policygit.Read(ctx, store, ref)
		if err != nil {
			return
		}
		c.log.Info("policy persisted to the branch", "repository", repository.Name, "branch", ref)
	}

	if commit == "" {
		return
	}
	if repository.Status.Policy != nil &&
		repository.Status.Policy.EvaluatedCommit == commit &&
		repository.Status.Policy.PolicyCommit == policyCommit {
		return
	}
	if err := c.auditRepositoryPolicy(ctx, namespace, repository, path, commit, policyCommit, ref, store, library); err != nil {
		c.log.Error("policy audit failed", "repository", repository.Name, "commit", shortenHash(commit), "err", err)
		c.setPolicyCondition(ctx, repository, namespace, metav1.ConditionFalse, specapi.ReasonPolicyInvalid, err.Error())
		return
	}
	c.setPolicyCondition(ctx, repository, namespace, metav1.ConditionTrue, specapi.ReasonPolicyCompliant,
		fmt.Sprintf("audited %s at %s", shortenHash(commit), shortenHash(policyCommit)))
}

// policyChangeInFlight names a PolicyChange of the repository that is still
// authoring or applying, so the branch-and-kcp sync leaves both alone: a
// restore prunes kcp, and it must not prune a policy an apply just wrote.
func (c *Controller) policyChangeInFlight(ctx context.Context, namespace, repository string) (string, error) {
	listed, err := c.client.List(ctx, specapi.PolicyChangeGVR, namespace)
	if err != nil {
		return "", err
	}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			continue
		}
		change, ok := typed.(*policy.PolicyChange)
		if !ok || change.Spec.Repository != repository {
			continue
		}
		switch change.Status.Phase {
		case "", policy.PolicyPhaseDrafting, policy.PolicyPhaseTesting:
			return change.Name, nil
		case policy.PolicyPhaseEvaluated:
			if change.Spec.Apply {
				return change.Name, nil
			}
		}
	}
	return "", nil
}

// persistPolicy writes the kcp templates and constraints to the branch and
// rebuilds the dist and the catalogue, so a policy edited in kcp is a branch
// commit like any other.
func (c *Controller) persistPolicy(ctx context.Context, store oagit.Store, ref string, repository *spec.Repository, kcpLibrary, branchLibrary policy.Library) error {
	files, err := policykcp.Files(kcpLibrary)
	if err != nil {
		return err
	}
	merged := branchLibrary
	merged.Templates = kcpLibrary.Templates
	merged.Constraints = kcpLibrary.Constraints
	merged.Sort()
	dist, err := policyeval.Dist(merged)
	if err != nil {
		return err
	}
	for name, data := range dist {
		files[name] = data
	}
	files[policy.CataloguePath] = policyeval.Catalogue(merged)
	stale := policykcp.Stale(branchLibrary.Files, kcpLibrary)
	message := fmt.Sprintf("policy(%s): sync from kcp\n", repository.Name)
	_, err = policygit.Update(ctx, store, ref, files, stale, message)
	return err
}

func (c *Controller) auditRepositoryPolicy(
	ctx context.Context,
	namespace string,
	repository *spec.Repository,
	path, commit, policyCommit, ref string,
	store oagit.Store,
	library policy.Library,
) error {
	contexts, err := c.repositoryContexts(ctx, namespace, repository.Name)
	if err != nil {
		return err
	}
	graph, err := codegraphfacts.Build(ctx, path, codegraphfacts.Options{
		Repository: repository.Name,
		Branch:     repository.Spec.Branch,
		Commit:     commit,
		Namespace:  namespace,
		TestGlobs:  library.Manifest.TestGlobs,
		Contexts:   codegraphfacts.ContextsByFile(contexts),
		Tool:       c.opts.Tool,
	})
	if err != nil {
		return fmt.Errorf("build the code graph: %w", err)
	}
	if _, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: effects.Dirs(path),
		IncludeExtras:   true,
	}); err != nil {
		return fmt.Errorf("compute the effects: %w", err)
	}

	inventory := []*unstructured.Unstructured{}
	encoded, err := yaml.Marshal(graph)
	if err != nil {
		return err
	}
	graphObject, err := policy.Unstructured(encoded)
	if err != nil {
		return err
	}
	inventory = append(inventory, graphObject)
	repositoryObject, err := kcpclient.Unstructured(repository)
	if err != nil {
		return err
	}
	inventory = append(inventory, repositoryObject)
	for index := range contexts {
		object, err := kcpclient.Unstructured(&contexts[index])
		if err != nil {
			return err
		}
		inventory = append(inventory, object)
	}

	members, err := policyeval.ResolveMembers(ctx, library.Manifest.Members, library, policyeval.MemberOptions{
		CacheDir: c.opts.CacheDir,
		Lock:     gateLock(library),
	})
	if err != nil {
		return fmt.Errorf("resolve the policy members: %w", err)
	}
	defer func() {
		for _, member := range members {
			member.Cleanup()
		}
	}()
	model, memberPins, err := policyeval.BuildEvaluationModel(ctx, policyeval.ModelRequest{
		Repository: repository.Name,
		Graph:      graph,
		Effects:    graph.Spec.Effects,
		Contexts:   modelContexts(contexts),
		Library:    library,
		Members:    members,
		Tool:       c.opts.Tool,
	})
	if err != nil {
		return fmt.Errorf("build the architecture model: %w", err)
	}
	modelDocument, err := yaml.Marshal(model)
	if err != nil {
		return err
	}
	modelObject, err := policy.Unstructured(modelDocument)
	if err != nil {
		return err
	}
	inventory = append(inventory, modelObject)

	report, err := policyeval.Evaluate(ctx, policyeval.Evaluation{
		Library:    library,
		Repository: repository.Name,
		Commit:     commit,
		Reviewed:   []*unstructured.Unstructured{graphObject, modelObject},
		Inventory:  inventory,
	})
	if err != nil {
		return fmt.Errorf("evaluate: %w", err)
	}
	report.Members = memberPins

	status := policyStatusOf(report, policyCommit)
	if _, err := c.client.PatchStatus(ctx, specapi.RepositoryGVR, namespace, repository.Name,
		map[string]any{"policy": status}); err != nil {
		return err
	}
	if err := c.updateContextConditions(ctx, namespace, contexts, report, library); err != nil {
		return err
	}

	document, err := yaml.Marshal(report)
	if err != nil {
		return err
	}
	if _, err := policygit.WriteReport(ctx, store, ref, repository.Spec.Branch, repository.Name, document); err != nil {
		return err
	}
	c.log.Info("policy audited",
		"repository", repository.Name, "commit", shortenHash(commit),
		"violations", len(report.Violations),
		"deny", report.Totals[policy.EnforcementDeny],
		"warn", report.Totals[policy.EnforcementWarn],
		"dryrun", report.Totals[policy.EnforcementDryRun])
	return nil
}

func (c *Controller) repositoryContexts(ctx context.Context, namespace, repository string) ([]spec.SystemContext, error) {
	listed, err := c.client.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	out := []spec.SystemContext{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok || systemContext.Spec.Repository != repository {
			continue
		}
		out = append(out, *systemContext)
	}
	sort.SliceStable(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out, nil
}

// updateContextConditions marks every SystemContext of the repository: a deny
// or warn violation that names the context or a file it owns makes it False,
// with the count and the first messages. The same pass records which templates
// enforce a requirement of the context, so a requirement a policy guards says
// so in the spec.
func (c *Controller) updateContextConditions(ctx context.Context, namespace string, contexts []spec.SystemContext, report policy.Report, library policy.Library) error {
	for index := range contexts {
		systemContext := contexts[index]
		violations := contextViolations(systemContext, report)
		conditions := condition.Copy(systemContext.Status.Conditions)
		if len(violations) == 0 {
			condition.Set(&conditions, systemContext.GetGeneration(), metav1.ConditionTrue,
				specapi.ConditionPolicyCompliant, specapi.ReasonPolicyCompliant, "no deny or warn violation names this context")
		} else {
			condition.Set(&conditions, systemContext.GetGeneration(), metav1.ConditionFalse,
				specapi.ConditionPolicyCompliant, specapi.ReasonPolicyViolations, violationsMessage(violations))
		}
		status := map[string]any{"conditions": conditions}
		// The field says what enforces the context's requirements now, so a
		// policy that stops being in force clears it rather than leaving the
		// last answer behind.
		status["enforcedBy"] = policy.TemplatesForRequirement(library, systemContext.Name)
		if specapi.StatusMatches(systemContext.Status, status) {
			continue
		}
		if _, err := c.client.PatchStatus(ctx, specapi.SystemContextGVR, namespace, systemContext.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func contextViolations(systemContext spec.SystemContext, report policy.Report) []policy.Violation {
	owned := map[string]bool{}
	for _, file := range systemContext.Status.Observed.Files {
		owned[file] = true
	}
	for _, file := range systemContext.Status.Observed.TreeFiles {
		owned[file] = true
	}
	out := []policy.Violation{}
	for _, violation := range report.Violations {
		if violation.Enforcement == policy.EnforcementDryRun {
			continue
		}
		if violation.Object.Kind == policy.SystemContextKind && violation.Object.Name == systemContext.Name {
			out = append(out, violation)
			continue
		}
		if violation.Location != nil && owned[violation.Location.File] {
			out = append(out, violation)
		}
	}
	return out
}

func violationsMessage(violations []policy.Violation) string {
	parts := make([]string, 0, len(violations))
	for _, violation := range violations {
		parts = append(parts, violation.Constraint+": "+violation.Msg)
	}
	sort.Strings(parts)
	limit := 3
	if len(parts) < limit {
		limit = len(parts)
	}
	message := fmt.Sprintf("%d policy violation(s): %s", len(violations), strings.Join(parts[:limit], "; "))
	if len(message) > messageLimit {
		message = message[:messageLimit]
	}
	return message
}

func policyStatusOf(report policy.Report, policyCommit string) *spec.PolicyStatus {
	status := &spec.PolicyStatus{
		PolicyCommit:    policyCommit,
		EvaluatedCommit: report.Commit,
		Totals: map[string]int{
			string(policy.EnforcementDeny):   report.Totals[policy.EnforcementDeny],
			string(policy.EnforcementWarn):   report.Totals[policy.EnforcementWarn],
			string(policy.EnforcementDryRun): report.Totals[policy.EnforcementDryRun],
		},
		Violations: []spec.PolicyViolation{},
	}
	for _, violation := range report.Violations {
		if len(status.Violations) >= StatusViolationLimit {
			break
		}
		status.Violations = append(status.Violations, compactViolation(violation))
	}
	if report.Empty() {
		status.Message = "clean"
	} else {
		status.Message = fmt.Sprintf("%d violation(s)", len(report.Violations))
	}
	return status
}

func compactViolation(violation policy.Violation) spec.PolicyViolation {
	out := spec.PolicyViolation{
		Policy:      violation.Policy,
		Constraint:  violation.Constraint,
		Enforcement: string(violation.Enforcement),
		Severity:    string(violation.Severity),
		Msg:         violation.Msg,
		Object:      violation.Object.String(),
	}
	if violation.Location != nil {
		out.File = violation.Location.File
		out.Line = violation.Location.Line
	}
	return out
}

func (c *Controller) setPolicyCondition(ctx context.Context, repository *spec.Repository, namespace string, status metav1.ConditionStatus, reason, message string) {
	conditions := condition.Copy(repository.Status.Conditions)
	condition.Set(&conditions, repository.GetGeneration(), status, specapi.ConditionPolicyReady, reason, message)
	if specapi.StatusMatches(repository.Status, map[string]any{"conditions": conditions}) {
		return
	}
	if _, err := c.client.PatchStatus(ctx, specapi.RepositoryGVR, namespace, repository.Name,
		map[string]any{"conditions": conditions}); err != nil {
		c.log.Error("could not record the policy condition", "repository", repository.Name, "err", err)
	}
}
