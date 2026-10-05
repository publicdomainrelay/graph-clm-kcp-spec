package persist

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
)

const maxAttempts = 5

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type Options struct {
	Cluster Cluster

	Namespace string

	Repository string

	RepoPath string

	Remote string

	ManagedBudget int

	Adopt bool

	CodeBranch string
}

type Result struct {
	Branch string

	Parent string

	Commit string

	Committed bool

	Paths []string

	Pushed bool

	Imported []string

	Created []string

	Conflicts map[string][]string

	Deferred []string
}

type state struct {
	snapshot oabranch.Snapshot

	origins map[string]string

	repoPath string
}

func (o Options) namespace() string {
	if o.Namespace != "" {
		return o.Namespace
	}
	return specapi.DefaultNamespace
}

func Persist(ctx context.Context, options Options) (Result, error) {
	if options.Repository == "" {
		return Result{}, fmt.Errorf("persist: a repository name is required")
	}
	current, err := read(ctx, options)
	if err != nil {
		return Result{}, err
	}
	store := oagit.Store{Repo: current.repoPath}
	result := Result{Conflicts: map[string][]string{}}
	top, err := store.IsTopLevel(ctx)
	if err != nil {
		return result, err
	}
	if !top {
		return result, fmt.Errorf("%w: %s", ErrNotTopLevel, current.repoPath)
	}
	defaultBranch := store.DefaultBranch(ctx)
	codeBranch := options.CodeBranch
	if codeBranch == "" {
		codeBranch = current.snapshot.Repository.Spec.Branch
	}
	result.Branch = oabranch.BranchFor(options.Repository, codeBranch, defaultBranch)
	ref := "refs/heads/" + result.Branch
	if err := ensureBaseArchitecture(ctx, store, options.Repository, current, codeBranch, defaultBranch); err != nil {
		return result, err
	}
	if err := branchOffDefault(ctx, store, options.Repository, ref, defaultBranch); err != nil {
		return result, err
	}

	tip, err := store.Tip(ctx, ref)
	if err != nil {
		return result, err
	}
	recorded := ""
	if status := current.snapshot.Repository.Status.OpenArchitecture; status != nil {
		recorded = status.Commit
		if status.Branch != "" && status.Branch != result.Branch {
			recorded = tip
		}
	}
	if tip != "" && tip != recorded && (recorded != "" || options.Adopt) {
		if err := adopt(ctx, options, store, current, recorded, tip, &result); err != nil {
			return result, err
		}
		current, err = read(ctx, options)
		if err != nil {
			return result, err
		}
	}

	current.snapshot.Branch = result.Branch
	if current.snapshot.Branch != oabranch.Branch(options.Repository) {
		baseline, err := readBaseline(ctx, store, options.Repository, codeBranch, defaultBranch, ref)
		if err != nil {
			return result, err
		}
		current.snapshot.Baseline = baseline
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		tip, err = store.Tip(ctx, ref)
		if err != nil {
			return result, err
		}
		result.Parent = tip
		files, err := oabranch.Files(current.snapshot)
		if err != nil {
			return result, err
		}
		blobs, err := store.Blobs(ctx, tip)
		if err != nil {
			return result, err
		}
		plan := oabranch.PlanCommit(blobs, files)
		if plan.Empty() {
			result.Commit = tip
			break
		}
		previous, err := store.ReadFilesAt(ctx, tip, append(plan.Paths(), oabranch.ChangePaths(blobs)...))
		if err != nil {
			return result, err
		}
		oabranch.PreserveChanges(files, previous)
		plan = oabranch.PlanCommit(blobs, files)
		plan, deferred := oabranch.CoalesceProgress(plan, previous)
		plan, noise := oabranch.CoalesceNoise(plan, previous)
		result.Deferred = append(deferred, noise...)
		sort.Strings(result.Deferred)
		if plan.Empty() {
			result.Commit = tip
			break
		}
		objects, commits := oabranch.TouchedObjects(plan, current.snapshot, func(kind, name string) string {
			return current.origins[kind+"/"+name]
		})
		message := oabranch.Message(options.Repository, plan, current.snapshot, previous, objects, commits, conflictLines(result.Conflicts, tip)...)
		commit, err := store.Commit(ctx, ref, tip, plan, message)
		if errors.Is(err, oagit.ErrRaced) {
			continue
		}
		if err != nil {
			return result, err
		}
		result.Commit = commit
		result.Committed = true
		result.Paths = plan.Paths()
		break
	}
	if result.Commit == "" && tip != "" {
		return result, fmt.Errorf("persist: %s kept moving for %d attempts", result.Branch, maxAttempts)
	}

	status := current.snapshot.Repository.Status.OpenArchitecture
	pushed := ""
	if status != nil {
		pushed = status.Pushed
	}
	if options.Remote != "" && result.Commit != "" && result.Commit != pushed {
		if err := store.Push(ctx, options.Remote, ref); err != nil {
			return result, err
		}
		result.Pushed = true
		pushed = result.Commit
	}
	if err := record(ctx, options, current.snapshot.Repository, result, pushed); err != nil {
		return result, err
	}
	return result, nil
}

func adopt(ctx context.Context, options Options, store oagit.Store, current state, recorded, tip string, result *Result) error {
	theirs, err := store.ReadFiles(ctx, tip)
	if err != nil {
		return err
	}
	theirSpecs, err := oabranch.SpecFiles(theirs)
	if err != nil {
		return err
	}
	baseSpecs := map[string]spec.SystemContext{}
	if recorded != "" {
		baseFiles, err := store.ReadFiles(ctx, recorded)
		if err != nil {
			return err
		}
		baseSpecs, err = oabranch.SpecFiles(baseFiles)
		if err != nil {
			return err
		}
	}
	ours := map[string]spec.SystemContext{}
	for _, context := range current.snapshot.Contexts {
		ours[context.Name] = context
	}
	names := make([]string, 0, len(theirSpecs))
	for name := range theirSpecs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		theirContext := theirSpecs[name]
		ourContext, exists := ours[name]
		if !exists {
			created := spec.SystemContext{}
			created.Name = name
			created.Namespace = options.namespace()
			created.Spec = theirContext.Spec
			created.Spec.Repository = options.Repository
			if err := applyContext(ctx, options, created); err != nil {
				return err
			}
			result.Created = append(result.Created, name)
			continue
		}
		base, inBase := baseSpecs[name]
		baseSpec := ourContext.Spec
		if inBase {
			baseSpec = base.Spec
		}
		merged := oabranch.Merge(baseSpec, ourContext.Spec, theirContext.Spec)
		if len(merged.Conflicts) > 0 {
			result.Conflicts[name] = merged.Conflicts
			continue
		}
		if !merged.Changed {
			continue
		}
		updated := ourContext
		updated.Spec = merged.Spec
		if err := applyContext(ctx, options, updated); err != nil {
			return err
		}
		result.Imported = append(result.Imported, name)
	}
	return nil
}

func applyContext(ctx context.Context, options Options, context spec.SystemContext) error {
	context.APIVersion = specapi.Group + "/" + specapi.Version
	context.Kind = specapi.SystemContextKind
	context.ResourceVersion = ""
	context.ManagedFields = nil
	if context.Annotations == nil {
		context.Annotations = map[string]string{}
	}
	context.Annotations[specapi.OriginAnnotation] = specapi.OriginGit
	delete(context.Annotations, specapi.OriginHashAnnotation)
	object, err := kcpclient.Unstructured(&context)
	if err != nil {
		return err
	}
	unstructured.RemoveNestedField(object.Object, "status")
	if _, err := options.Cluster.Apply(ctx, object); err != nil {
		return fmt.Errorf("persist: apply systemcontext %s: %w", context.Name, err)
	}
	return nil
}

func record(ctx context.Context, options Options, repository spec.Repository, result Result, pushed string) error {
	conflicts := []string{}
	for name, keys := range result.Conflicts {
		for _, key := range keys {
			conflicts = append(conflicts, name+": "+key)
		}
	}
	sort.Strings(conflicts)
	next := spec.OpenArchitectureStatus{Branch: result.Branch, Commit: result.Commit, Pushed: pushed, Conflicts: conflicts}
	if previous := repository.Status.OpenArchitecture; previous != nil && equalStatus(*previous, next) {
		return nil
	}
	value := map[string]any{"branch": next.Branch, "commit": next.Commit}
	if next.Pushed != "" {
		value["pushed"] = next.Pushed
	}
	if len(next.Conflicts) > 0 {
		items := make([]any, 0, len(next.Conflicts))
		for _, conflict := range next.Conflicts {
			items = append(items, conflict)
		}
		value["conflicts"] = items
	} else {
		value["conflicts"] = nil
	}
	_, err := options.Cluster.PatchStatus(ctx, specapi.RepositoryGVR, options.namespace(), repository.Name, map[string]any{"openArchitecture": value})
	if err != nil {
		return fmt.Errorf("persist: record %s on repository %s: %w", result.Commit, repository.Name, err)
	}
	return nil
}

func conflictLines(conflicts map[string][]string, branchCommit string) []string {
	lines := []string{}
	for context, keys := range conflicts {
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("%s %s kept from kcp over branch commit %s", context, key, branchCommit))
		}
	}
	sort.Strings(lines)
	return lines
}

func equalStatus(left, right spec.OpenArchitectureStatus) bool {
	if left.Branch != right.Branch || left.Commit != right.Commit || left.Pushed != right.Pushed || len(left.Conflicts) != len(right.Conflicts) {
		return false
	}
	for index := range left.Conflicts {
		if left.Conflicts[index] != right.Conflicts[index] {
			return false
		}
	}
	return true
}

func read(ctx context.Context, options Options) (state, error) {
	namespace := options.namespace()
	object, err := options.Cluster.Get(ctx, specapi.RepositoryGVR, namespace, options.Repository)
	if err != nil {
		return state{}, fmt.Errorf("persist: read repository %s: %w", options.Repository, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return state{}, err
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		return state{}, fmt.Errorf("persist: %s is not a Repository", options.Repository)
	}
	current := state{
		snapshot: oabranch.Snapshot{Repository: *repository, ManagedBudget: options.ManagedBudget},
		origins:  map[string]string{specapi.RepositoryKind + "/" + repository.Name: repository.Annotations[specapi.OriginAnnotation]},
		repoPath: options.RepoPath,
	}
	if current.repoPath == "" {
		current.repoPath = repository.WorkPath()
	}
	if current.repoPath == "" {
		return state{}, fmt.Errorf("persist: repository %s has no working tree yet", options.Repository)
	}
	current.snapshot.Guarded = guardedRequirements(ctx, oagit.Store{Repo: current.repoPath}, repository)

	contexts, err := options.Cluster.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return state{}, fmt.Errorf("persist: list systemcontexts: %w", err)
	}
	names := map[string]bool{}
	for index := range contexts.Items {
		typed, err := kcpclient.Typed(&contexts.Items[index])
		if err != nil {
			return state{}, err
		}
		context, ok := typed.(*spec.SystemContext)
		if !ok || context.Spec.Repository != options.Repository {
			continue
		}
		names[context.Name] = true
		current.snapshot.Contexts = append(current.snapshot.Contexts, *context)
		current.origins[specapi.SystemContextKind+"/"+context.Name] = context.Annotations[specapi.OriginAnnotation]
	}

	changes, err := options.Cluster.List(ctx, specapi.SpecChangeGVR, namespace)
	if err != nil {
		return state{}, fmt.Errorf("persist: list specchanges: %w", err)
	}
	for index := range changes.Items {
		typed, err := kcpclient.Typed(&changes.Items[index])
		if err != nil {
			return state{}, err
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok || !names[change.Spec.SystemContext] {
			continue
		}
		current.snapshot.Changes = append(current.snapshot.Changes, *change)
	}
	sort.Slice(current.snapshot.Changes, func(left, right int) bool {
		return current.snapshot.Changes[left].Name < current.snapshot.Changes[right].Name
	})
	return current, nil
}

// readBaseline reads the architecture a feature branch's CHANGES.md measures
// its requirement delta against: the architecture branch of the code branch it
// was cut from, falling back to the default branch's, and, on a branch that was
// populated before any base was persisted, to the branch's own last commit
// before its first spec edit.
func readBaseline(ctx context.Context, store oagit.Store, repository, codeBranch, defaultBranch, featureRef string) (*oabranch.Baseline, error) {
	for _, name := range baselineCandidates(ctx, store, repository, codeBranch, defaultBranch) {
		tip, err := store.Tip(ctx, "refs/heads/"+name)
		if err != nil {
			return nil, err
		}
		if tip != "" {
			baseline, err := baselineFromCommit(ctx, store, tip)
			if baseline != nil {
				baseline.Branch = name
			}
			return baseline, err
		}
	}
	tip, err := store.Tip(ctx, featureRef)
	if err != nil || tip == "" {
		return &oabranch.Baseline{}, err
	}
	commit, err := featureBaselineCommit(ctx, store, featureRef)
	if err != nil {
		return nil, err
	}
	if commit == "" {
		return &oabranch.Baseline{Branch: strings.TrimPrefix(featureRef, "refs/heads/")}, nil
	}
	baseline, err := baselineFromCommit(ctx, store, commit)
	if baseline != nil {
		baseline.Branch = strings.TrimPrefix(featureRef, "refs/heads/")
	}
	return baseline, err
}

func baselineCandidates(ctx context.Context, store oagit.Store, repository, codeBranch, defaultBranch string) []string {
	names := []string{}
	add := func(name string) {
		if name == "" || slices.Contains(names, name) {
			return
		}
		names = append(names, name)
	}
	base := baseCodeBranch(ctx, store, codeBranch, defaultBranch)
	add(oabranch.BranchFor(repository, base, defaultBranch))
	add(oabranch.Branch(repository))
	return names
}

// baseCodeBranch is the code branch a feature branch was cut from: the branch
// it tracks, else the default branch.
func baseCodeBranch(ctx context.Context, store oagit.Store, codeBranch, defaultBranch string) string {
	if codeBranch == "" || codeBranch == defaultBranch {
		return defaultBranch
	}
	if upstream := store.UpstreamBranch(ctx); upstream != "" && upstream != codeBranch {
		return upstream
	}
	return defaultBranch
}

func baselineFromCommit(ctx context.Context, store oagit.Store, tip string) (*oabranch.Baseline, error) {
	baseline := &oabranch.Baseline{}
	files, err := store.ReadFiles(ctx, tip)
	if err != nil {
		return nil, err
	}
	specs, err := oabranch.SpecFiles(files)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		baseline.Contexts = append(baseline.Contexts, specs[name])
	}
	changes, err := oabranch.ChangeFiles(files)
	if err != nil {
		return nil, err
	}
	baseline.Changes = changes
	return baseline, nil
}

// featureBaselineCommit is the feature branch's own last commit before the
// first spec-to-code change appeared: the state the branch was populated with,
// which is the architecture of the base code branch by another name.
func featureBaselineCommit(ctx context.Context, store oagit.Store, featureRef string) (string, error) {
	history, err := store.History(ctx, featureRef)
	if err != nil || len(history) == 0 {
		return "", err
	}
	for index, commit := range history {
		paths, err := store.Paths(ctx, commit)
		if err != nil {
			return "", err
		}
		if !hasSpecToCodeChange(paths) {
			continue
		}
		if index == 0 {
			return "", nil
		}
		return history[index-1], nil
	}
	return history[0], nil
}

func hasSpecToCodeChange(paths []string) bool {
	for _, path := range paths {
		if oabranch.IsSpecToCodeChangePath(path) {
			return true
		}
	}
	return false
}

// ensureBaseArchitecture writes the architecture of the base code branch when
// a feature branch is persisted before any base exists, so CHANGES.md has a
// baseline instead of reporting the whole spec as added. It writes nothing once
// a spec has been edited by a person or a model: the snapshot then is no longer
// the base code's architecture.
func ensureBaseArchitecture(ctx context.Context, store oagit.Store, repository string, current state, codeBranch, defaultBranch string) error {
	if codeBranch == "" || codeBranch == defaultBranch {
		return nil
	}
	baseBranch := baseCodeBranch(ctx, store, codeBranch, defaultBranch)
	baseRef := oabranch.RefFor(repository, baseBranch, defaultBranch)
	if baseRef == oabranch.RefFor(repository, codeBranch, defaultBranch) {
		return nil
	}
	tip, err := store.Tip(ctx, baseRef)
	if err != nil || tip != "" {
		return err
	}
	if !fullyPopulated(current.snapshot.Repository, len(current.snapshot.Contexts)) {
		return nil
	}
	if humanEdited(current.origins) {
		return nil
	}
	base := current.snapshot
	base.Branch = oabranch.BranchFor(repository, baseBranch, defaultBranch)
	files, err := oabranch.Files(base)
	if err != nil {
		return err
	}
	plan := oabranch.PlanCommit(map[string]string{}, files)
	message := fmt.Sprintf("architecture(%s): baseline for %s\n\n", repository, oabranch.BranchFor(repository, codeBranch, defaultBranch))
	for _, path := range plan.Added {
		message += "A " + path + "\n"
	}
	if _, err := store.Commit(ctx, baseRef, "", plan, message); err != nil {
		if raced, raceErr := store.Tip(ctx, baseRef); raceErr == nil && raced != "" {
			return nil
		}
		return err
	}
	return nil
}

// fullyPopulated reports whether the populate that owns the snapshot has
// finished: writing a base architecture from a half-built repository would pin
// every context that arrived later as a spec this branch added.
func fullyPopulated(repository spec.Repository, contexts int) bool {
	if repository.Status.Phase != specapi.PhasePopulated {
		return false
	}
	if counts := repository.Status.Contexts; counts != nil && counts.Total > contexts {
		return false
	}
	return true
}

// humanEdited reports whether any context of the snapshot carries an origin
// that is neither the ingest nor a reviewed branch edit, which is what a person
// or a model editing the spec leaves behind.
func humanEdited(origins map[string]string) bool {
	for kind, origin := range origins {
		if !strings.HasPrefix(kind, specapi.SystemContextKind+"/") {
			continue
		}
		switch origin {
		case specapi.OriginIngest, specapi.OriginGit:
		default:
			return true
		}
	}
	return false
}

func branchOffDefault(ctx context.Context, store oagit.Store, repository, ref, defaultBranch string) error {
	defaultRef := oabranch.RefFor(repository, defaultBranch, defaultBranch)
	if ref == defaultRef {
		return nil
	}
	tip, err := store.Tip(ctx, ref)
	if err != nil || tip != "" {
		return err
	}
	base, err := store.Tip(ctx, defaultRef)
	if err != nil || base == "" {
		return err
	}
	return store.SetRef(ctx, ref, base, "")
}

var ErrNoBranch = errors.New("persist: the repository has no open-architecture branch")

var ErrNotTopLevel = errors.New("persist: the repository path is not the top of its own git repository")

type RestoreOptions struct {
	Cluster Cluster

	Namespace string

	Repository string

	RepoPath string

	Remote string

	CodeBranch string
}

type RestoreResult struct {
	Branch string

	Commit string

	Fetched bool

	Repository string

	Contexts []string

	Changes []string
}

func Restore(ctx context.Context, options RestoreOptions) (RestoreResult, error) {
	if options.Repository == "" || options.RepoPath == "" {
		return RestoreResult{}, fmt.Errorf("persist: restore needs a repository name and a repo path")
	}
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	store := oagit.Store{Repo: options.RepoPath}
	defaultBranch := store.DefaultBranch(ctx)
	codeBranch := options.CodeBranch
	if codeBranch == "" {
		codeBranch = store.CurrentBranch(ctx)
	}
	candidates := []string{oabranch.BranchFor(options.Repository, codeBranch, defaultBranch)}
	if fallback := oabranch.Branch(options.Repository); fallback != candidates[0] {
		candidates = append(candidates, fallback)
	}
	result := RestoreResult{}
	tip := ""
	for _, branch := range candidates {
		ref := "refs/heads/" + branch
		found, err := store.Tip(ctx, ref)
		if err != nil {
			return result, err
		}
		if found == "" && options.Remote != "" {
			fetched, err := store.Fetch(ctx, options.Remote, branch)
			if err != nil {
				return result, err
			}
			if fetched != "" {
				if err := store.SetRef(ctx, ref, fetched, ""); err != nil {
					return result, err
				}
				found = fetched
				result.Fetched = true
			}
		}
		if found != "" {
			tip = found
			result.Branch = branch
			break
		}
	}
	if tip == "" {
		return result, ErrNoBranch
	}
	result.Commit = tip
	files, err := store.ReadFiles(ctx, tip)
	if err != nil {
		return result, err
	}
	repository, err := oabranch.RepositoryFile(files)
	if err != nil {
		return result, err
	}
	repository.Name = options.Repository
	repository.Namespace = namespace
	repository.Spec.Path = ""
	repository.Spec.Source = &spec.RepositorySource{Path: options.RepoPath}
	if codeBranch != "" {
		repository.Spec.Branch = codeBranch
	}
	repository.APIVersion = specapi.Group + "/" + specapi.Version
	repository.Kind = specapi.RepositoryKind
	if repository.Annotations == nil {
		repository.Annotations = map[string]string{}
	}
	repository.Annotations[specapi.OriginAnnotation] = specapi.OriginGit
	object, err := kcpclient.Unstructured(&repository)
	if err != nil {
		return result, err
	}
	unstructured.RemoveNestedField(object.Object, "status")
	if _, err := options.Cluster.Apply(ctx, object); err != nil {
		return result, fmt.Errorf("persist: apply repository %s: %w", repository.Name, err)
	}
	result.Repository = repository.Name

	specs, err := oabranch.SpecFiles(files)
	if err != nil {
		return result, err
	}
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	persistOptions := Options{Cluster: options.Cluster, Namespace: namespace, Repository: options.Repository, RepoPath: options.RepoPath}
	for _, name := range names {
		context := spec.SystemContext{}
		context.Name = name
		context.Namespace = namespace
		context.Spec = specs[name].Spec
		context.Spec.Repository = options.Repository
		if existing, err := options.Cluster.Get(ctx, specapi.SystemContextGVR, namespace, name); err == nil {
			context.Labels = existing.GetLabels()
			context.Annotations = existing.GetAnnotations()
		}
		if err := applyContext(ctx, persistOptions, context); err != nil {
			return result, err
		}
		result.Contexts = append(result.Contexts, name)
	}
	adopted := Result{Branch: result.Branch, Commit: tip, Conflicts: map[string][]string{}}
	restored, err := restoreChanges(ctx, options.Cluster, namespace, files)
	if err != nil {
		return result, err
	}
	result.Changes = restored
	if err := record(ctx, persistOptions, repository, adopted, ""); err != nil {
		return result, err
	}
	return result, nil
}

// restoreChanges rebuilds the SpecChange history the branch carries. A CRD's
// status is dropped when it is applied, so the phase, the commit and the
// attempt number are patched back after each record is created. A phase the
// branch recorded but kcp never settled - Pending, Running, or none - becomes
// Failed, so a restored change can never look Running forever and block the
// repository; the operator retries it with specctl retry.
func restoreChanges(ctx context.Context, cluster Cluster, namespace string, files map[string][]byte) ([]string, error) {
	changes, err := oabranch.ChangeHistory(files)
	if err != nil {
		return nil, err
	}
	restored := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Name == "" || change.Spec.SystemContext == "" {
			continue
		}
		change.APIVersion = specapi.Group + "/" + specapi.Version
		change.Kind = specapi.SpecChangeKind
		change.Namespace = namespace
		change.ResourceVersion = ""
		change.ManagedFields = nil
		phase, message := restoredPhase(change)
		object, err := kcpclient.Unstructured(&change)
		if err != nil {
			return nil, fmt.Errorf("persist: encode change %s: %w", change.Name, err)
		}
		unstructured.RemoveNestedField(object.Object, "status")
		if _, err := cluster.Apply(ctx, object); err != nil {
			return nil, fmt.Errorf("persist: apply change %s: %w", change.Name, err)
		}
		status := map[string]any{
			"phase":   phase,
			"message": message,
			"attempt": int64(attemptNumber(change)),
		}
		if change.Status.Commit != "" {
			status["commit"] = change.Status.Commit
		}
		if change.Status.Branch != "" {
			status["branch"] = change.Status.Branch
		}
		if change.Status.VerifyExitCode != 0 {
			status["verifyExitCode"] = int64(change.Status.VerifyExitCode)
		}
		if change.Status.RetryReason != "" {
			status["retryReason"] = change.Status.RetryReason
			status["retryBy"] = change.Status.RetryBy
		}
		if _, err := cluster.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, change.Name, status); err != nil {
			return nil, fmt.Errorf("persist: patch change %s status: %w", change.Name, err)
		}
		restored = append(restored, change.Name)
	}
	sort.Strings(restored)
	return restored, nil
}

func restoredPhase(change spec.SpecChange) (string, string) {
	switch change.Status.Phase {
	case specapi.PhaseSucceeded, specapi.PhaseFailed:
		return change.Status.Phase, change.Status.Message
	case "":
		return specapi.PhaseFailed, "restored: the branch recorded no phase for this change"
	default:
		return specapi.PhaseFailed, "restored: the branch recorded this change as " + change.Status.Phase
	}
}

func attemptNumber(change spec.SpecChange) int {
	if index := strings.LastIndex(change.Name, "-a"); index >= 0 {
		if number, err := strconv.Atoi(change.Name[index+2:]); err == nil && number > 0 {
			return number
		}
	}
	return 1
}
