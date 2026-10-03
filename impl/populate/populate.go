// Package populate is one "turn a codebase into specs" unit of work: index the
// working tree, create one SystemContext per partition, and, when the
// Repository asks for it, raise one CodeToSpec change per context whose spec is
// still empty. specd's Repository reconciler is the caller; specctl ingest
// applies a Repository and waits for the phase this writes, so there is one
// code path.
package populate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
)

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	Create(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type Options struct {
	Cluster Cluster

	Namespace string

	Repository *spec.Repository

	// Path is the resolved working tree. It is the Repository's spec.path, or
	// the cache directory a git source was cloned into.
	Path string

	// Index runs codegraph and rewrites the observed facts. A reconcile that
	// finds the HEAD already indexed and the pipeline complete leaves it false.
	Index bool

	// Commit is the resolved HEAD of Path, empty when the tree is not a git
	// working tree. It is what the observed facts are recorded against.
	Commit string

	Tool string

	Writer graph.Writer

	MaxConcurrent int

	MaxAttempts int
}

type Result struct {
	Repository string

	Phase string

	Commit string

	Contexts []ingest.ContextResult

	Total int

	Summarized int

	Failed int

	Raised int
}

// Run indexes and populates one Repository. It is idempotent: the status it
// writes is derived from what it reads, and a second run over the same tree
// raises no change and writes no status.
func Run(ctx context.Context, options Options) (Result, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	repository := options.Repository
	result := Result{Repository: repository.Name, Total: 0}

	// Cloning happened before this call and is the caller's phase to write.
	if options.Index {
		if err := patchPhase(ctx, options, namespace, specapi.PhaseIndexing); err != nil {
			return result, err
		}
		indexed, err := ingest.Run(ctx, options.Cluster, ingest.Options{
			RepoPath:       options.Path,
			RepositoryName: repository.Name,
			Namespace:      namespace,
			SpecPath:       options.Path,
			Tool:           options.Tool,
			Commit:         options.Commit,
			Partition:      repository.Partition(),
			Include:        includeOf(repository),
			Exclude:        excludeOf(repository),
			Writer:         options.Writer,
		})
		if err != nil {
			return result, err
		}
		result.Contexts = indexed.Contexts
		result.Commit = indexed.Commit
	}

	// The index step wrote headCommit, indexedCommit and the Indexed condition,
	// so the status this run writes has to start from the object as it is now.
	// A conditions list copied from before the index would drop that write.
	current, err := Read(ctx, options.Cluster, namespace, repository.Name)
	if err != nil {
		return result, err
	}
	repository = current
	options.Repository = current

	contexts, err := listContexts(ctx, options.Cluster, namespace, repository.Name)
	if err != nil {
		return result, err
	}
	changes, err := listChanges(ctx, options.Cluster, namespace)
	if err != nil {
		return result, err
	}

	result.Total = len(contexts)
	tally := tallyContexts(contexts, changes, options.MaxAttempts)
	result.Summarized = tally.summarized
	result.Failed = tally.failed

	if repository.Summarize() && tally.open > 0 {
		raised, err := raise(ctx, options, namespace, contexts, changes, tally.running)
		if err != nil {
			return result, err
		}
		result.Raised = raised
	}

	switch {
	case tally.failed > 0 && tally.summarized+tally.failed == result.Total:
		result.Phase = specapi.PhaseFailed
	case !repository.Summarize() || tally.summarized == result.Total:
		result.Phase = specapi.PhasePopulated
	default:
		result.Phase = specapi.PhasePopulating
	}

	if err := writeStatus(ctx, options, namespace, result); err != nil {
		return result, err
	}
	return result, nil
}

type counts struct {
	summarized int

	failed int

	open int

	running int
}

// tallyContexts reads the population state off the objects themselves, so a
// controller restart resumes exactly where it stopped: a context is summarized
// when its intent is not empty, failed when its newest change of the episode is
// Failed and the attempt cap is reached, and open otherwise.
func tallyContexts(contexts []spec.SystemContext, changes []spec.SpecChange, maxAttempts int) counts {
	out := counts{}
	for index := range contexts {
		context := &contexts[index]
		if context.Spec.Intent != "" {
			out.summarized++
			continue
		}
		base := spec.ChangeNameCodeToSpec(context.Name, context.Status.SyncedCommit, context.Status.ObservedCommit)
		attempts, newest := episode(changes, base)
		if attempts >= maxAttempts && maxAttempts > 0 && newest != nil && newest.Status.Phase == specapi.PhaseFailed {
			out.failed++
			continue
		}
		out.open++
	}
	for index := range changes {
		if changes[index].Spec.Direction == specapi.DirectionCodeToSpec && changes[index].Status.Phase == specapi.PhaseRunning {
			out.running++
		}
	}
	return out
}

func episode(changes []spec.SpecChange, base string) (int, *spec.SpecChange) {
	attempts := 0
	var newest *spec.SpecChange
	for index := range changes {
		change := &changes[index]
		if change.Spec.Direction != specapi.DirectionCodeToSpec {
			continue
		}
		if !spec.ChangeNameMatches(change.Name, base) {
			continue
		}
		attempts++
		if newest == nil || change.GetCreationTimestamp().After(newest.GetCreationTimestamp().Time) {
			newest = change
		}
	}
	return attempts, newest
}

// raise creates the CodeToSpec changes one populate still owes, at most
// MaxConcurrent of them running across the namespace at a time. A context that
// already has an unfinished change is left alone, so a reconcile that runs
// twice raises one change, not two.
func raise(
	ctx context.Context,
	options Options,
	namespace string,
	contexts []spec.SystemContext,
	changes []spec.SpecChange,
	running int,
) (int, error) {
	if options.MaxConcurrent > 0 && running >= options.MaxConcurrent {
		return 0, nil
	}
	taken := make([]string, 0, len(changes))
	unfinished := map[string]bool{}
	for index := range changes {
		change := &changes[index]
		taken = append(taken, change.Name)
		// A change created but not yet phased (the create and the status patch
		// are two calls) counts as unfinished too: treating it as absent would
		// raise a second attempt of the same episode beside it.
		if change.Spec.Direction == specapi.DirectionCodeToSpec &&
			change.Status.Phase != specapi.PhaseFailed && change.Status.Phase != specapi.PhaseSucceeded {
			unfinished[change.Spec.SystemContext] = true
		}
	}
	ordered := append([]spec.SystemContext{}, contexts...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Name < ordered[right].Name })

	raised := 0
	for index := range ordered {
		context := &ordered[index]
		if context.Spec.Intent != "" || unfinished[context.Name] {
			continue
		}
		if options.MaxConcurrent > 0 && running+raised >= options.MaxConcurrent {
			break
		}
		base := spec.ChangeNameCodeToSpec(context.Name, context.Status.SyncedCommit, context.Status.ObservedCommit)
		attempts := spec.AttemptCount(taken, base)
		if options.MaxAttempts > 0 && attempts >= options.MaxAttempts {
			continue
		}
		observedDelta := delta.DiffObserved(context.Status.SyncedObserved, context.Status.Observed)
		change := &spec.SpecChange{
			ObjectMeta: metav1.ObjectMeta{
				Name:      spec.NextChangeName(taken, base),
				Namespace: namespace,
			},
			Spec: spec.SpecChangeSpec{
				SystemContext: context.Name,
				Direction:     specapi.DirectionCodeToSpec,
				FromCommit:    context.Status.SyncedCommit,
				ToCommit:      context.Status.ObservedCommit,
				Delta:         &observedDelta,
			},
		}
		change.SetDefaults()
		if result := spec.ValidateSpecChange(change); !result.OK() {
			return raised, fmt.Errorf("populate: %s is invalid: %w", change.Name, result.Err())
		}
		object, err := kcpclient.Unstructured(change)
		if err != nil {
			return raised, err
		}
		created, err := options.Cluster.Create(ctx, object)
		if err != nil {
			if kcpclient.IsAlreadyExists(err) {
				continue
			}
			return raised, err
		}
		// A create cannot write the status subresource, so the phase is patched
		// in right after it; no reader ever sees a change without one.
		if _, err := options.Cluster.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, created.GetName(), map[string]any{
			"phase":   specapi.PhasePending,
			"message": "waiting for the agent that summarizes " + context.Name,
		}); err != nil {
			return raised, err
		}
		taken = append(taken, created.GetName())
		raised++
	}
	return raised, nil
}

func writeStatus(ctx context.Context, options Options, namespace string, result Result) error {
	repository := options.Repository
	conditions := condition.Copy(repository.Status.Conditions)
	// A failure leaves Indexed False so a reader can see it; when this run has
	// the index current, the condition has to come back to True, or a transient
	// failure of another step would leave a permanent False behind.
	if options.Index || (options.Commit != "" && options.Commit == repository.Status.IndexedCommit) {
		condition.SetTrue(&conditions, repository.GetGeneration(), specapi.ConditionIndexed,
			specapi.ReasonIndexed, "the codegraph index is current")
	}
	switch result.Phase {
	case specapi.PhasePopulated:
		condition.SetTrue(&conditions, repository.GetGeneration(), specapi.ConditionPopulated,
			specapi.ReasonPopulated, fmt.Sprintf("%d context(s), %d summarized", result.Total, result.Summarized))
	case specapi.PhaseFailed:
		condition.SetFalse(&conditions, repository.GetGeneration(), specapi.ConditionPopulated,
			specapi.ReasonPopulateFailed,
			fmt.Sprintf("%d context(s) could not be summarized", result.Failed))
	default:
		condition.SetFalse(&conditions, repository.GetGeneration(), specapi.ConditionPopulated,
			specapi.ReasonPopulating,
			fmt.Sprintf("%d of %d context(s) summarized", result.Summarized, result.Total))
	}
	// The desired status is built as the typed status and marshalled back, so a
	// field this run has nothing to say about is omitted exactly as it is on the
	// object being compared with. A hand written map would carry its zero values
	// and every reconcile would look like a change.
	status := statusObject(spec.RepositoryStatus{
		ObservedGeneration: repository.GetGeneration(),
		ResolvedPath:       options.Path,
		Phase:              result.Phase,
		Contexts: &spec.PopulateCounts{
			Total:      result.Total,
			Summarized: result.Summarized,
			Failed:     result.Failed,
		},
		PopulateRequest: repository.Annotations[specapi.PopulateRequestAnnotation],
		Conditions:      conditions,
	})
	if specapi.StatusMatches(repository.Status, status) {
		return nil
	}
	_, err := options.Cluster.PatchStatus(ctx, specapi.RepositoryGVR, namespace, repository.Name, status)
	return err
}

func statusObject(status spec.RepositoryStatus) map[string]any {
	encoded, err := json.Marshal(status)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func patchPhase(ctx context.Context, options Options, namespace, phase string) error {
	if options.Repository.Status.Phase == phase {
		return nil
	}
	_, err := options.Cluster.PatchStatus(ctx, specapi.RepositoryGVR, namespace, options.Repository.Name,
		map[string]any{"phase": phase})
	return err
}

func listContexts(ctx context.Context, cluster Cluster, namespace, repository string) ([]spec.SystemContext, error) {
	listed, err := cluster.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	out := []spec.SystemContext{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		context, ok := typed.(*spec.SystemContext)
		if !ok || context.Spec.Repository != repository {
			continue
		}
		out = append(out, *context)
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out, nil
}

// listChanges reads every SpecChange in the namespace. The tally only counts
// code -> spec ones, but the names are shared: a new change must never collide
// with a change of the other direction.
func listChanges(ctx context.Context, cluster Cluster, namespace string) ([]spec.SpecChange, error) {
	listed, err := cluster.List(ctx, specapi.SpecChangeGVR, namespace)
	if err != nil {
		return nil, err
	}
	out := []spec.SpecChange{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok {
			continue
		}
		out = append(out, *change)
	}
	return out, nil
}

func includeOf(repository *spec.Repository) []string {
	if repository.Spec.Populate == nil {
		return nil
	}
	return repository.Spec.Populate.Include
}

func excludeOf(repository *spec.Repository) []string {
	if repository.Spec.Populate == nil {
		return nil
	}
	return repository.Spec.Populate.Exclude
}

// WaitForPopulated polls a Repository until the phase answers the request, and
// reports the phase it settled on. specctl ingest uses it; a timeout is not an
// error of the manifest but of the controller, so the caller reports both.
func WaitForPopulated(ctx context.Context, cluster Cluster, namespace, name, request string, interval time.Duration) (*spec.Repository, error) {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	for {
		repository, err := Read(ctx, cluster, namespace, name)
		if err != nil {
			return nil, err
		}
		answered := request == "" || repository.Status.PopulateRequest == request
		switch {
		case answered && repository.Status.Phase == specapi.PhasePopulated:
			return repository, nil
		case answered && repository.Status.Phase == specapi.PhaseFailed:
			return repository, fmt.Errorf("the repository is Failed: %s", conditionMessage(repository.Status.Conditions, specapi.ConditionPopulated))
		}
		select {
		case <-ctx.Done():
			return repository, fmt.Errorf("the repository did not reach Populated: %w", ctx.Err())
		case <-time.After(interval):
		}
	}
}

func Read(ctx context.Context, cluster Cluster, namespace, name string) (*spec.Repository, error) {
	object, err := cluster.Get(ctx, specapi.RepositoryGVR, namespace, name)
	if err != nil {
		return nil, err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		return nil, fmt.Errorf("populate: %s is not a Repository", name)
	}
	return repository, nil
}

func conditionMessage(conditions []metav1.Condition, conditionType string) string {
	if found := condition.Of(conditions, conditionType); found != nil {
		return found.Message
	}
	return "no condition"
}
