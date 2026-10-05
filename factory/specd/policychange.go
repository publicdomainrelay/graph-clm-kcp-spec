package specd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policykcp"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

// policyDraft is one validated generation: what the harness wrote, where it
// wrote it and what the checks said.
type policyDraft struct {
	dir string

	library policy.Library

	manifest policy.PolicyLibrary

	checks []policy.Check

	report policy.Report

	summary string

	refusedLog string
}

// appendAttemptLog keeps every refused attempt in the accepted change's agent
// log: the harness reads it as the next attempt's instruction, so the reason
// the earlier trees were refused has to survive the accepted one.
func appendAttemptLog(log string, attempt int, failures []string) string {
	line := fmt.Sprintf("attempt %d: %s", attempt, strings.Join(failures, "\n"))
	if log == "" {
		return line
	}
	return log + "\n" + line
}

func (d policyDraft) failed() bool {
	for _, check := range d.checks {
		if !check.Passed {
			return true
		}
	}
	return false
}

// reconcilePolicyChange drafts a generated policy or binding, checks it and
// applies it when the spec asks for it (or `specctl policy accept` set apply).
func (c *Controller) reconcilePolicyChange(ctx context.Context, namespace, name string) (time.Duration, error) {
	object, err := c.client.Get(ctx, specapi.PolicyChangeGVR, namespace, name)
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
	change, ok := typed.(*policy.PolicyChange)
	if !ok {
		return 0, fmt.Errorf("specd: %s is not a PolicyChange", name)
	}

	switch change.Status.Phase {
	case "", policy.PolicyPhaseDrafting, policy.PolicyPhaseTesting:
		return c.draftPolicyChange(ctx, namespace, change)
	case policy.PolicyPhaseEvaluated:
		if change.Spec.Apply {
			return c.applyPolicyChange(ctx, namespace, change)
		}
	}
	return 0, nil
}

func (c *Controller) draftPolicyChange(ctx context.Context, namespace string, change *policy.PolicyChange) (time.Duration, error) {
	repository, err := c.readRepository(ctx, namespace, change.Spec.Repository)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
		return 0, nil
	}
	path := repository.WorkPath()
	if path == "" {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError,
			"the repository "+repository.Name+" names no path")
		return 0, nil
	}
	if !gitrepo.IsRepo(ctx, path) {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError,
			path+" is not a git working tree")
		return 0, nil
	}
	if !c.agents.ConfiguredFor(repository) {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError,
			"no agent is configured, so no policy can be authored")
		return 0, nil
	}

	branchLibrary, policyCommit, err := c.draftBranchLibrary(ctx, change, repository)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
		return 0, nil
	}
	contexts, err := c.repositoryContexts(ctx, namespace, repository.Name)
	if err != nil {
		return 0, err
	}
	if clash := slugClash(branchLibrary, change); clash != "" {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError,
			"the repository already carries a template "+clash+"; the slug names the directory, the kind and the constraint, so a generated policy must use a free one")
		return 0, nil
	}
	binding := branchLibrary.Manifest.Binding()
	pack, packLibrary, packFS, err := c.policyPack(change, branchLibrary)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
		return 0, nil
	}
	source, err := c.repositorySource(ctx, namespace, repository, path, contexts)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, "build the model: "+err.Error())
		return 0, nil
	}
	dir, err := c.policyScratchDir(namespace, change.Name)
	if err != nil {
		return 0, err
	}
	draft := policyDraft{dir: dir, manifest: branchLibrary.Manifest}
	maxAttempts := c.opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	failures := []string{}
	attemptsSpent := 0
	refusedLog := ""
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptsSpent = attempt
		if err := os.RemoveAll(dir); err != nil {
			return 0, err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
		generator, err := c.agents.GeneratorFor(repository, dir)
		if err != nil {
			c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
			return 0, nil
		}
		request, err := c.policyGenerateRequest(change, repository, dir, source, binding, pack, contexts, branchLibrary, attempt, failures)
		if err != nil {
			c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
			return 0, nil
		}
		generated, err := generator.Generate(ctx, request)
		if err != nil {
			failures = append(failures, "the harness failed: "+err.Error())
			refusedLog = appendAttemptLog(refusedLog, attempt, failures)
			c.recordPolicyAttempt(ctx, namespace, change, dir, attempt, failures, generated.Summary)
			continue
		}
		draft, err = c.checkPolicyDraft(ctx, change, repository, dir, binding, pack, packLibrary, packFS, source, contexts, branchLibrary, request)
		if err != nil {
			return 0, err
		}
		draft.summary = generated.Summary
		draft.refusedLog = refusedLog
		if !draft.failed() {
			break
		}
		failures = append([]string{}, draft.failures()...)
		refusedLog = appendAttemptLog(refusedLog, attempt, failures)
		c.recordPolicyAttempt(ctx, namespace, change, dir, attempt, failures, generated.Summary)
	}
	if draft.failed() || len(draft.checks) == 0 {
		c.failPolicyChange(ctx, namespace, change, policy.ReasonPolicyTestsFailed,
			"the harness did not author a policy the checks accept: "+strings.Join(failures, "; "))
		return 0, nil
	}

	c.recordPolicyEvaluated(ctx, namespace, change, draft, policyCommit, attemptsSpent)
	if change.Spec.Apply {
		return c.applyPolicyChange(ctx, namespace, change)
	}
	c.log.Info("policy change evaluated",
		"change", change.Name, "mode", change.Spec.Mode(), "slug", change.Spec.TemplateSlug(),
		"violations", len(draft.report.Violations), "attempts", len(failures))
	return 0, nil
}

func (d policyDraft) failures() []string {
	out := []string{}
	for _, check := range d.checks {
		if !check.Passed {
			out = append(out, check.Name+": "+check.Message)
		}
	}
	return out
}

func (c *Controller) policyGenerateRequest(
	change *policy.PolicyChange,
	repository *spec.Repository,
	dir string,
	source repositoryModelSource,
	binding policy.Binding,
	pack *policy.PackManifest,
	contexts []spec.SystemContext,
	branchLibrary policy.Library,
	attempt int,
	failures []string,
) (policy.GenerateRequest, error) {
	bindMode := change.Spec.Mode() == policy.GenerateModeBind
	buildBinding := binding
	if bindMode {
		buildBinding = policy.Binding{}
	}
	model, err := source.model(buildBinding)
	if err != nil {
		return policy.GenerateRequest{}, err
	}
	rendered := renderModel(model)
	if bindMode {
		binding = policy.Binding{}
		rendered = renderBindModel(model)
	}
	return policy.GenerateRequest{
		Mode:         change.Spec.Mode(),
		Repository:   repository.Name,
		Branch:       repository.Spec.Branch,
		Dir:          dir,
		Slug:         change.Spec.TemplateSlug(),
		Prompt:       change.Spec.Prompt,
		Requirements: change.Spec.Requirements,
		Contexts:     generateContexts(contexts, change.Spec.Contexts),
		Enforcement:  change.Spec.EnforcementAction,
		Pack:         pack,
		Binding:      binding,
		Model:        rendered,
		Existing:     templateNames(branchLibrary),
		Attempt:      attempt,
		Failures:     failures,
	}, nil
}

// renderBindModel strips the binding out of the model a bind-mode prompt
// carries: the harness sees the code's own components, effects and flows, never
// the roles, the globs or the vocabulary the branch already declares.
func renderBindModel(model policy.ArchitectureModel) string {
	stripped := model
	stripped.Spec.Roles = nil
	stripped.Spec.Vocabulary = policy.Vocabulary{}
	components := make([]policy.ModelComponent, len(model.Spec.Components))
	copy(components, model.Spec.Components)
	for index := range components {
		components[index].Roles = nil
		components[index].Globs = nil
	}
	stripped.Spec.Components = components
	return renderModel(stripped)
}

func (c *Controller) checkPolicyDraft(
	ctx context.Context,
	change *policy.PolicyChange,
	repository *spec.Repository,
	dir string,
	binding policy.Binding,
	pack *policy.PackManifest,
	packLibrary policy.Library,
	packFS fs.FS,
	source repositoryModelSource,
	contexts []spec.SystemContext,
	branchLibrary policy.Library,
	request policy.GenerateRequest,
) (policyDraft, error) {
	draft := policyDraft{dir: dir, manifest: branchLibrary.Manifest}
	if change.Spec.Mode() == policy.GenerateModeBind {
		generated, err := readBinding(dir)
		if err != nil {
			draft.checks = append(draft.checks, policy.Check{Name: "manifest", Passed: false, Message: err.Error()})
			return draft, nil
		}
		model, inventory, err := source.modelAndInventory(generated.Binding())
		if err != nil {
			return draft, err
		}
		result, err := policyeval.CheckGeneratedBinding(ctx, policyeval.BindingInput{
			Dir:         dir,
			Repository:  repository.Name,
			Pack:        pack,
			PackLibrary: packLibrary,
			PackFS:      packFS,
			Branch:      branchLibrary.Manifest,
			Model:       model,
			Terms:       contextTerms(contexts),
			Commit:      codegraphfacts.GitCommit(ctx, repository.WorkPath()),
			Reviewed:    inventory[:1],
			Inventory:   inventory,
		})
		if err != nil {
			return draft, err
		}
		draft.checks = result.Checks
		draft.manifest = result.Manifest
		draft.report = result.Report
		return draft, nil
	}

	inventory, err := source.onlyInventory(binding)
	if err != nil {
		return draft, err
	}
	result, err := policyeval.CheckGenerated(ctx, policyeval.GeneratedInput{
		Dir:          dir,
		Slug:         request.Slug,
		Binding:      binding,
		Repository:   repository.Name,
		Contexts:     contextNamesOf(contexts),
		Classifiers:  branchLibrary.Manifest.Classifiers,
		Vocabulary:   policy.MutationVocabularyOf(binding, packRoles(binding, pack)),
		GeneratedBy:  change.Name,
		Requirements: change.Spec.Requirements,
		Commit:       codegraphfacts.GitCommit(ctx, repository.WorkPath()),
		Reviewed:     inventory[:1],
		Inventory:    inventory,
	})
	if err != nil {
		return draft, err
	}
	draft.checks = result.Checks
	draft.report = result.Report
	if !result.Passed() {
		// A tree the checks refused is feedback for the next attempt, not a
		// reconcile error: a harness that wrote nothing useful must hear why.
		return draft, nil
	}
	library, err := policyeval.LoadRaw(os.DirFS(dir))
	if err != nil {
		draft.checks = append(draft.checks, policy.Check{Name: "template", Passed: false, Message: err.Error()})
		return draft, nil
	}
	draft.library = library
	return draft, nil
}

// slugClash names the template a change's slug would collide with: the slug is
// the directory, the Rego package and the constraint name, and the template
// name is lower(kind), so a generated policy that reuses either one would
// silently replace or shadow the repository's own template. It is empty when
// the slug is free.
func slugClash(library policy.Library, change *policy.PolicyChange) string {
	slug := change.Spec.TemplateSlug()
	for _, template := range library.Templates {
		switch {
		case policy.TemplateSlug(template) == slug:
			return "with the slug " + slug
		case template.Name == slugToName(slug):
			return "whose kind makes the name " + slugToName(slug)
		}
	}
	return ""
}

// slugToName is lower(kind): the kind is the slug's dash-separated words with
// each first letter capitalized.
func slugToName(slug string) string {
	parts := strings.FieldsFunc(slug, func(char rune) bool {
		return char == '-' || char == '_' || char == '.' || char == '/'
	})
	builder := strings.Builder{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	return strings.ToLower(builder.String())
}

// policyChangeRecord is the append-only record a change leaves on the policy
// branch: the spec and the status, without the cluster's bookkeeping.
func policyChangeRecord(change *policy.PolicyChange) []byte {
	trimmed := *change
	trimmed.ManagedFields = nil
	trimmed.ResourceVersion = ""
	trimmed.UID = ""
	trimmed.Generation = 0
	encoded, err := yaml.Marshal(trimmed)
	if err != nil {
		return nil
	}
	return encoded
}

// applyPolicyChange commits the evaluated policy to the repository's policy
// branch and applies it to kcp, then records the change as Applied.
func (c *Controller) applyPolicyChange(ctx context.Context, namespace string, change *policy.PolicyChange) (time.Duration, error) {
	repository, err := c.readRepository(ctx, namespace, change.Spec.Repository)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
		return 0, nil
	}
	path := repository.WorkPath()
	if path == "" {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError,
			"the repository "+repository.Name+" names no path")
		return 0, nil
	}
	dir, err := c.policyScratchDir(namespace, change.Name)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(dir); err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError,
			"the generated tree is gone; the change must be drafted again")
		return 0, nil
	}
	branchLibrary, _, err := c.policyBranchLibrary(ctx, repository)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
		return 0, nil
	}

	store := oagit.Store{Repo: path}
	ref := policy.RefFor(policyBranchOf(repository), repository.Spec.Branch, store.DefaultBranch(ctx))
	add, merged, err := c.policyApplyFiles(ctx, change, dir, branchLibrary)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
		return 0, nil
	}

	// kcp first, the branch second. The sync treats a template the branch holds
	// and kcp does not as stale and removes it, so a branch that names a policy
	// before kcp holds it can lose that policy to a concurrent reconcile.
	cluster, ok := c.client.(policykcp.Cluster)
	if !ok {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, "the cluster cannot hold policies")
		return 0, nil
	}
	if err := c.applyPolicyLibrary(ctx, cluster, merged, branchLibrary, change); err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, "apply to kcp: "+err.Error())
		return 0, nil
	}

	dist, err := policyeval.Dist(merged)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, err.Error())
		return 0, nil
	}
	for name, data := range dist {
		add[name] = data
	}
	add[policy.CataloguePath] = policyeval.Catalogue(merged)
	add[policy.ChangePath(change.Name)] = policyChangeRecord(change)

	message := fmt.Sprintf("policy(%s): %s %s\n", repository.Name, change.Spec.Mode(), change.Spec.TemplateSlug())
	commit, err := policygit.Update(ctx, store, ref, add, nil, message)
	if err != nil {
		c.failPolicyChange(ctx, namespace, change, specapi.ReasonPolicyGateError, "commit to "+ref+": "+err.Error())
		return 0, nil
	}

	conditions := condition.Copy(change.Status.Conditions)
	condition.SetTrue(&conditions, change.GetGeneration(), policy.ConditionPolicyReady,
		policy.ReasonPolicyApplied, "applied to "+ref+" at "+shortenHash(commit))
	if _, err := c.client.PatchStatus(ctx, specapi.PolicyChangeGVR, namespace, change.Name, map[string]any{
		"phase":        policy.PolicyPhaseApplied,
		"policyCommit": commit,
		"message":      "applied to " + ref + " at " + shortenHash(commit),
		"conditions":   conditions,
	}); err != nil {
		return 0, err
	}
	c.enqueueRepository(ctx, namespace, repository.Name)
	c.log.Info("policy change applied",
		"change", change.Name, "repository", repository.Name, "ref", ref, "commit", shortenHash(commit))
	return 0, nil
}

// policyApplyFiles builds the branch files and the library the change joins:
// the generated tree in policy mode, the proposed manifest in bind mode.
func (c *Controller) policyApplyFiles(ctx context.Context, change *policy.PolicyChange, dir string, branchLibrary policy.Library) (map[string][]byte, policy.Library, error) {
	if change.Spec.Mode() == policy.GenerateModeBind {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(policy.PoliciesPath)))
		if err != nil {
			return nil, policy.Library{}, fmt.Errorf("the generated binding is gone: %w", err)
		}
		manifest := branchLibrary.Manifest
		proposed := policy.PolicyLibrary{}
		if err := yaml.Unmarshal(data, &proposed); err != nil {
			return nil, policy.Library{}, err
		}
		manifest.Roles = proposed.Roles
		manifest.Vocabulary = proposed.Vocabulary
		encoded, err := yaml.Marshal(manifest)
		if err != nil {
			return nil, policy.Library{}, err
		}
		merged := branchLibrary
		merged.Manifest = manifest
		return map[string][]byte{policy.PoliciesPath: encoded}, merged, nil
	}

	files, err := readTree(dir)
	if err != nil {
		return nil, policy.Library{}, err
	}
	generated, err := policyeval.LoadRaw(os.DirFS(dir))
	if err != nil {
		return nil, policy.Library{}, err
	}
	merged := branchLibrary
	merged.Templates = append(append([]policy.Template{}, branchLibrary.Templates...), generated.Templates...)
	merged.Constraints = append(append([]policy.Constraint{}, branchLibrary.Constraints...), generated.Constraints...)
	merged.Sort()
	own, err := policykcp.Files(policy.Library{Templates: generated.Templates, Constraints: generated.Constraints})
	if err != nil {
		return nil, policy.Library{}, err
	}
	for name, data := range own {
		files[name] = data
	}
	return files, merged, nil
}

// applyPolicyLibrary applies the change's own members to kcp: a generated
// template and its constraint, or none at all when the binding only moved
// roles and vocabulary.
func (c *Controller) applyPolicyLibrary(ctx context.Context, cluster policykcp.Cluster, merged, branchLibrary policy.Library, change *policy.PolicyChange) error {
	if change.Spec.Mode() == policy.GenerateModeBind {
		return nil
	}
	generated := policy.Library{}
	for _, template := range merged.Templates {
		if branchLibrary.ImportedFrom(policy.TemplateSlug(template)) != "" {
			continue
		}
		if _, existed := branchLibrary.Template(template.Name); existed {
			continue
		}
		generated.Templates = append(generated.Templates, template)
	}
	for _, constraint := range merged.Constraints {
		if _, existed := branchLibrary.Constraint(constraint.Name); existed {
			continue
		}
		generated.Constraints = append(generated.Constraints, constraint)
	}
	if len(generated.Templates) == 0 {
		return nil
	}
	engine, err := policyeval.NewEngine(ctx, generated, nil)
	if err != nil {
		return err
	}
	return policykcp.Apply(ctx, cluster, generated, policykcp.ApplyOptions{CRDs: engine})
}

// draftBranchLibrary reads the branch a draft is authored against. A bind-mode
// draft reads it without resolving its imports: the branch that holds the pack
// import and not yet the roles the pack requires is exactly the branch a
// binding is authored for.
func (c *Controller) draftBranchLibrary(ctx context.Context, change *policy.PolicyChange, repository *spec.Repository) (policy.Library, string, error) {
	if change.Spec.Mode() != policy.GenerateModeBind {
		return c.policyBranchLibrary(ctx, repository)
	}
	path := repository.WorkPath()
	if path == "" {
		return policy.Library{}, "", nil
	}
	store := oagit.Store{Repo: path}
	ref := policy.RefFor(policyBranchOf(repository), repository.Spec.Branch, store.DefaultBranch(ctx))
	return policygit.ReadRaw(ctx, store, ref)
}

// policyBranchLibrary reads the repository's policy branch, resolved: the
// templates, the constraints, the roles and the vocabulary the generation
// reads.
func (c *Controller) policyBranchLibrary(ctx context.Context, repository *spec.Repository) (policy.Library, string, error) {
	path := repository.WorkPath()
	if path == "" {
		return policy.Library{}, "", nil
	}
	store := oagit.Store{Repo: path}
	ref := policy.RefFor(policyBranchOf(repository), repository.Spec.Branch, store.DefaultBranch(ctx))
	library, commit, err := policygit.Read(ctx, store, ref)
	if err != nil {
		return policy.Library{}, "", fmt.Errorf("read %s: %w", ref, err)
	}
	return library, commit, nil
}

// policyPack resolves the pack a bind change names: its manifest, its library
// and, when it is embedded, its own tree.
func (c *Controller) policyPack(change *policy.PolicyChange, branchLibrary policy.Library) (*policy.PackManifest, policy.Library, fs.FS, error) {
	if change.Spec.Mode() != policy.GenerateModeBind {
		return nil, policy.Library{}, nil, nil
	}
	name := change.Spec.Pack
	version := change.Spec.PackVersion
	source := policy.SourceEmbedded
	for _, imp := range branchLibrary.Manifest.Imports {
		if imp.Pack != name {
			continue
		}
		if version == "" {
			version = imp.Version
		}
		if imp.Source != "" {
			source = imp.Source
		}
		break
	}
	if source != policy.SourceEmbedded {
		resolved, err := policyeval.ResolveImport(policy.PackImport{Pack: name, Version: version, Source: source}, policyeval.ImportOptions{})
		if err != nil {
			return nil, policy.Library{}, nil, err
		}
		return &resolved.Manifest, resolved.Library, nil, nil
	}
	library, fsys, err := policyeval.EmbeddedPack(name, version)
	if err != nil {
		return nil, policy.Library{}, nil, err
	}
	return library.Pack, library, fsys, nil
}

// repositoryModelSource is a repository's graph, effects and contexts, read
// once, so a model can be built several times with different bindings: the
// prompt's model and the checked model of a generated binding must not be the
// same one.
type repositoryModelSource struct {
	repository string

	graph policy.CodeGraph

	effects []policy.Effect

	contexts []policy.ModelContext

	objects []*unstructured.Unstructured
}

func (s repositoryModelSource) model(binding policy.Binding) (policy.ArchitectureModel, error) {
	return policy.BuildModel(policy.ModelInput{
		Repository: s.repository,
		Graph:      s.graph,
		Effects:    s.effects,
		Contexts:   s.contexts,
		Binding:    binding,
	})
}

func (s repositoryModelSource) inventory(model policy.ArchitectureModel) ([]*unstructured.Unstructured, error) {
	modelObject, err := policy.Unstructured([]byte(renderModel(model)))
	if err != nil {
		return nil, err
	}
	return append([]*unstructured.Unstructured{modelObject}, s.objects...), nil
}

func (s repositoryModelSource) modelAndInventory(binding policy.Binding) (policy.ArchitectureModel, []*unstructured.Unstructured, error) {
	model, err := s.model(binding)
	if err != nil {
		return policy.ArchitectureModel{}, nil, err
	}
	inventory, err := s.inventory(model)
	if err != nil {
		return policy.ArchitectureModel{}, nil, err
	}
	return model, inventory, nil
}

func (s repositoryModelSource) onlyInventory(binding policy.Binding) ([]*unstructured.Unstructured, error) {
	_, inventory, err := s.modelAndInventory(binding)
	return inventory, err
}

// repositorySource reads the head graph and effects of a repository and the
// inventory its rules read, once for every model built from it.
func (c *Controller) repositorySource(
	ctx context.Context,
	namespace string,
	repository *spec.Repository,
	path string,
	contexts []spec.SystemContext,
) (repositoryModelSource, error) {
	commit := codegraphfacts.GitCommit(ctx, path)
	graph, err := codegraphfacts.Build(ctx, path, codegraphfacts.Options{
		Repository: repository.Name,
		Branch:     repository.Spec.Branch,
		Commit:     commit,
		Namespace:  namespace,
		Contexts:   codegraphfacts.ContextsByFile(contexts),
		Tool:       c.opts.Tool,
	})
	if err != nil {
		return repositoryModelSource{}, err
	}
	if _, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: effects.Dirs(path),
		IncludeExtras:   true,
	}); err != nil {
		return repositoryModelSource{}, err
	}
	modelContexts := make([]policy.ModelContext, 0, len(contexts))
	objects := make([]*unstructured.Unstructured, 0, len(contexts)+1)
	for _, context := range contexts {
		modelContexts = append(modelContexts, policy.ModelContext{
			Name:         context.Name,
			Labels:       context.Labels,
			Interactions: modelInteractions(context.Spec.Interactions),
		})
		object, err := kcpclient.Unstructured(&context)
		if err != nil {
			return repositoryModelSource{}, err
		}
		objects = append(objects, object)
	}
	repositoryObject, err := kcpclient.Unstructured(repository)
	if err != nil {
		return repositoryModelSource{}, err
	}
	return repositoryModelSource{
		repository: repository.Name,
		graph:      graph,
		effects:    graph.Spec.Effects,
		contexts:   modelContexts,
		objects:    append([]*unstructured.Unstructured{repositoryObject}, objects...),
	}, nil
}

// readBinding reads the policies.yaml a harness wrote in bind mode.
func readBinding(dir string) (policy.PolicyLibrary, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(policy.PoliciesPath)))
	if err != nil {
		return policy.PolicyLibrary{}, err
	}
	manifest := policy.PolicyLibrary{}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return policy.PolicyLibrary{}, err
	}
	return manifest, nil
}

func modelInteractions(interactions []spec.Interaction) []policy.DeclaredInteraction {
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

// recordPolicyAttempt keeps the failing attempt visible: the phase, the
// attempt number and the messages the next attempt reads.
func (c *Controller) recordPolicyAttempt(ctx context.Context, namespace string, change *policy.PolicyChange, dir string, attempt int, failures []string, summary string) {
	message := fmt.Sprintf("attempt %d: %s", attempt, strings.Join(failures, "; "))
	log := summary
	if log == "" {
		log = strings.Join(failures, "\n")
	}
	if _, err := c.client.PatchStatus(ctx, specapi.PolicyChangeGVR, namespace, change.Name, map[string]any{
		"phase":    policy.PolicyPhaseTesting,
		"attempt":  attempt,
		"message":  tailMessage(message),
		"agentLog": tailMessage(strings.Join(failures, "\n") + "\n" + log),
	}); err != nil {
		c.log.Error("could not record the policy attempt", "change", change.Name, "err", err)
	}
	c.log.Warn("the generated policy did not pass its checks",
		"change", change.Name, "attempt", attempt, "dir", dir, "message", tailMessage(message))
}

// recordPolicyEvaluated writes the accepted generation: the checks, the
// violations against the head model and the module the change would land.
func (c *Controller) recordPolicyEvaluated(ctx context.Context, namespace string, change *policy.PolicyChange, draft policyDraft, policyCommit string, attempt int) {
	status := map[string]any{
		"phase":   policy.PolicyPhaseEvaluated,
		"mode":    change.Spec.Mode(),
		"slug":    change.Spec.TemplateSlug(),
		"attempt": attempt,
		"checks":  draft.checks,
		"message": evaluatedMessage(draft),
	}
	log := draft.refusedLog
	if draft.summary != "" {
		if log == "" {
			log = draft.summary
		} else {
			log = log + "\n" + draft.summary
		}
	}
	if log != "" {
		status["agentLog"] = tailMessage(log)
	}
	if change.Spec.Mode() == policy.GenerateModeBind {
		binding := draft.manifest.Binding()
		status["binding"] = binding
	} else {
		for _, template := range draft.library.Templates {
			status["template"] = template.Name
		}
		names := []string{}
		for _, constraint := range draft.library.Constraints {
			names = append(names, constraint.Name)
		}
		sort.Strings(names)
		status["constraints"] = names
		status["tests"] = policy.PolicyTestResult{
			Passed: passedChecks(draft.checks),
			Failed: len(draft.checks) - passedChecks(draft.checks),
			Output: checksOutput(draft.checks),
		}
	}
	violations := make([]policy.CompactViolation, 0, len(draft.report.Violations))
	for _, violation := range draft.report.Violations {
		violations = append(violations, policy.CompactViolationOf(violation))
	}
	status["violations"] = violations
	if policyCommit != "" {
		status["policyCommit"] = policyCommit
	}
	if _, err := c.client.PatchStatus(ctx, specapi.PolicyChangeGVR, namespace, change.Name, status); err != nil {
		c.log.Error("could not record the evaluated policy", "change", change.Name, "err", err)
	}
}

func (c *Controller) failPolicyChange(ctx context.Context, namespace string, change *policy.PolicyChange, reason, message string) {
	conditions := condition.Copy(change.Status.Conditions)
	condition.SetFalse(&conditions, change.GetGeneration(), policy.ConditionPolicyReady, reason, tailMessage(message))
	status := map[string]any{
		"phase":      policy.PolicyPhaseFailed,
		"message":    tailMessage(message),
		"conditions": conditions,
	}
	if specapi.StatusMatches(change.Status, status) {
		return
	}
	if _, err := c.client.PatchStatus(ctx, specapi.PolicyChangeGVR, namespace, change.Name, status); err != nil {
		c.log.Error("could not record the failed policy change", "change", change.Name, "err", err)
		return
	}
	c.log.Error("policy change failed", "change", change.Name, "repository", change.Spec.Repository, "message", message)
}

func (c *Controller) policyScratchDir(namespace, name string) (string, error) {
	dir := filepath.Join(statedir.Dir(), "policy-changes", namespace+"-"+name)
	return dir, os.MkdirAll(dir, 0o755)
}

func (c *Controller) enqueueRepository(ctx context.Context, namespace, name string) {
	c.queue.Add(key{Kind: specapi.RepositoryKind, Namespace: namespace, Name: name})
}

func generateContexts(contexts []spec.SystemContext, wanted []string) []policy.GenerateContext {
	selected := map[string]bool{}
	for _, name := range wanted {
		selected[name] = true
	}
	out := []policy.GenerateContext{}
	for _, context := range contexts {
		if len(selected) > 0 && !selected[context.Name] {
			continue
		}
		requirements := make([]string, 0, len(context.Spec.Requirements))
		for _, requirement := range context.Spec.Requirements {
			requirements = append(requirements, context.Name+"#"+requirement.ID)
		}
		out = append(out, policy.GenerateContext{
			Name:         context.Name,
			Labels:       context.Labels,
			Requirements: requirements,
		})
	}
	return out
}

func contextTerms(contexts []spec.SystemContext) []string {
	out := []string{}
	for _, context := range contexts {
		for _, requirement := range context.Spec.Requirements {
			out = append(out, requirement.ID, requirement.Text)
		}
		out = append(out, context.Name, context.Spec.Intent)
	}
	return out
}

func contextNamesOf(contexts []spec.SystemContext) []string {
	out := make([]string, 0, len(contexts))
	for _, context := range contexts {
		out = append(out, context.Name)
	}
	sort.Strings(out)
	return out
}

func templateNames(library policy.Library) []string {
	out := make([]string, 0, len(library.Templates))
	for _, template := range library.Templates {
		out = append(out, policy.TemplateSlug(template))
	}
	sort.Strings(out)
	return out
}

func packRoles(binding policy.Binding, pack *policy.PackManifest) []string {
	if pack != nil {
		return pack.Roles
	}
	return binding.RoleNames()
}

func renderModel(model policy.ArchitectureModel) string {
	encoded, err := yaml.Marshal(model)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func passedChecks(checks []policy.Check) int {
	count := 0
	for _, check := range checks {
		if check.Passed {
			count++
		}
	}
	return count
}

func checksOutput(checks []policy.Check) string {
	parts := []string{}
	for _, check := range checks {
		state := "ok"
		if !check.Passed {
			state = "FAIL"
		}
		line := state + " " + check.Name
		if check.Message != "" {
			line += ": " + check.Message
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

func evaluatedMessage(draft policyDraft) string {
	return fmt.Sprintf("%d check(s) passed, %d violation(s) against the head model",
		passedChecks(draft.checks), len(draft.report.Violations))
}

// readTree reads every file of a directory, keyed by its slash path.
func readTree(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
