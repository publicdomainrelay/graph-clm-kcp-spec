package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)
	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)
	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type Options struct {
	RepoPath string

	RepositoryName string

	Namespace string

	SpecPath string

	Tool string

	Commit string

	Writer graph.Writer
}

type ContextResult struct {
	Name        string
	Created     bool
	SpecChanged bool
	StatusWrote bool
	Fingerprint string
	Observed    spec.ObservedFacts
	Conditions  []metav1.Condition
}

type Result struct {
	Repository string

	Commit string

	SpecPath string

	Contexts []ContextResult
}

func Run(ctx context.Context, cluster Cluster, options Options) (Result, error) {
	repoPath, err := filepath.Abs(options.RepoPath)
	if err != nil {
		return Result{}, fmt.Errorf("ingest: resolve %s: %w", options.RepoPath, err)
	}
	repositoryName := options.RepositoryName
	if repositoryName == "" {
		repositoryName = sanitizeName(path.Base(repoPath))
	}
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	specPath := options.SpecPath
	if specPath == "" {
		specPath = options.RepoPath
	}

	dbPath, err := codegraphsqlite.Ensure(ctx, repoPath, options.Tool)
	if err != nil {
		return Result{}, err
	}
	database, err := codegraphsqlite.Open(dbPath)
	if err != nil {
		return Result{}, err
	}
	defer database.Close()

	commit := options.Commit
	if commit == "" {
		commit, err = gitrepo.Head(ctx, repoPath)
		if err != nil {
			commit = ""
		}
	}
	facts, err := database.Facts(ctx, commit)
	if err != nil {
		return Result{}, err
	}
	partitions := specsync.PartitionFacts(facts, repositoryName)

	result := Result{Repository: repositoryName, Commit: commit, SpecPath: specPath}

	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: repositoryName, Namespace: namespace},
		Spec:       spec.RepositorySpec{Path: specPath},
	}
	repository.SetDefaults()
	if err := upsertRepository(ctx, cluster, repository, commit); err != nil {
		return Result{}, err
	}

	for _, partition := range partitions {
		contextResult, err := ingestPartition(ctx, cluster, repositoryName, namespace, commit, partition)
		if err != nil {
			return Result{}, err
		}
		result.Contexts = append(result.Contexts, contextResult)
	}

	if options.Writer != nil {
		if err := RebuildGraph(ctx, cluster, options.Writer, options); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func upsertRepository(ctx context.Context, cluster Cluster, repository *spec.Repository, commit string) error {
	existing, err := cluster.Get(ctx, specapi.RepositoryGVR, repository.Namespace, repository.Name)
	switch {
	case err == nil:
		typed, err := kcpclient.Typed(existing)
		if err != nil {
			return err
		}
		current, ok := typed.(*spec.Repository)
		if !ok {
			return fmt.Errorf("ingest: %s is not a Repository", repository.Name)
		}
		repository.ObjectMeta = current.ObjectMeta
		repository.Spec = current.Spec
		repository.Status = current.Status
	case kcpclient.IsNotFound(err):
		object, err := kcpclient.Unstructured(repository)
		if err != nil {
			return err
		}
		applied, err := cluster.Apply(ctx, object)
		if err != nil {
			return err
		}
		created, err := kcpclient.Typed(applied)
		if err != nil {
			return err
		}
		if typed, ok := created.(*spec.Repository); ok {
			repository.ObjectMeta = typed.ObjectMeta
			repository.Status = typed.Status
		}
	default:
		return err
	}

	conditions := conditionsFor(repository.Status.Conditions, []metav1.Condition{{
		Type:    specapi.ConditionIndexed,
		Status:  metav1.ConditionTrue,
		Reason:  specapi.ReasonIndexed,
		Message: "the codegraph index is current",
	}})
	status := map[string]any{
		"headCommit":         commit,
		"indexedCommit":      commit,
		"observedGeneration": repository.GetGeneration(),
		"conditions":         conditions,
	}
	if statusMatches(repository.Status, status) {
		return nil
	}
	_, err = cluster.PatchStatus(ctx, specapi.RepositoryGVR, repository.Namespace, repository.Name, status)
	return err
}

func ingestPartition(
	ctx context.Context,
	cluster Cluster,
	repositoryName, namespace, commit string,
	partition specsync.Partition,
) (ContextResult, error) {
	observed := specsync.Observed(partition)
	result := ContextResult{Name: partition.Name, Fingerprint: observed.Fingerprint, Observed: observed}

	var existingContext *spec.SystemContext
	var previousFingerprint string
	found, err := cluster.Get(ctx, specapi.SystemContextGVR, namespace, partition.Name)
	switch {
	case err == nil:
		typed, err := kcpclient.Typed(found)
		if err != nil {
			return result, err
		}
		current, ok := typed.(*spec.SystemContext)
		if !ok {
			return result, fmt.Errorf("ingest: %s is not a SystemContext", partition.Name)
		}
		existingContext = current
		previousFingerprint = current.Status.Observed.Fingerprint
	case kcpclient.IsNotFound(err):
		existingContext = &spec.SystemContext{ObjectMeta: metav1.ObjectMeta{Name: partition.Name, Namespace: namespace}}
		result.Created = true
	default:
		return result, err
	}

	merged := existingContext.Spec
	if merged.Repository == "" {
		merged.Repository = repositoryName
	}
	// A context ingest creates is its own upstream; the CRD default only
	// applies on the server, and the local validator runs before that.
	if merged.Upstream == "" {
		merged.Upstream = spec.RefSelf
	}
	merged.CodeRefs = mergeCodeRefs(merged.CodeRefs, observed.Files)

	specChanged := !reflect.DeepEqual(merged, existingContext.Spec)
	generation := existingContext.GetGeneration()

	var applied *unstructured.Unstructured
	if specChanged {
		updated := &spec.SystemContext{
			ObjectMeta: *existingContext.ObjectMeta.DeepCopy(),
			Spec:       merged,
			Status:     existingContext.Status,
		}
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}
		updated.Annotations[specapi.OriginAnnotation] = specapi.OriginIngest
		updated.SetDefaults()
		object, err := kcpclient.Unstructured(updated)
		if err != nil {
			return result, err
		}
		applied, err = cluster.Apply(ctx, object)
		if err != nil {
			return result, err
		}
		generation = applied.GetGeneration()
		result.SpecChanged = true
	}

	decision := specsync.Decide(merged.Interfaces, observed, previousFingerprint)
	conditions := conditionsFor(existingContext.Status.Conditions, desiredConditions(partition.Name, merged, decision))
	result.Conditions = conditions

	status := map[string]any{
		"observedGeneration": generation,
		"observedCommit":     commit,
		"observed":           observedObject(observed),
		"conditions":         conditions,
	}
	if specChanged {
		hash, err := specHash(&merged)
		if err != nil {
			return result, err
		}
		status["realizedSpecHash"] = hash
	}

	if statusMatches(existingContext.Status, status) {
		return result, nil
	}
	if _, err := cluster.PatchStatus(ctx, specapi.SystemContextGVR, namespace, partition.Name, status); err != nil {
		return result, err
	}
	result.StatusWrote = true
	return result, nil
}

func desiredConditions(name string, merged spec.SystemContextSpec, decision specsync.Decision) []metav1.Condition {
	conditions := []metav1.Condition{{
		Type:    specapi.ConditionSpecValid,
		Status:  metav1.ConditionTrue,
		Reason:  specapi.ReasonValidatorPassed,
		Message: "the spec passes the validator",
	}}
	candidate := spec.SystemContext{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: merged}
	if result := spec.ValidateSystemContext(&candidate); !result.OK() {
		conditions[0].Status = metav1.ConditionFalse
		conditions[0].Reason = specapi.ReasonValidatorFailed
		conditions[0].Message = result.Err().Error()
	}
	synced := metav1.Condition{
		Type:    specapi.ConditionCodeSynced,
		Status:  metav1.ConditionTrue,
		Reason:  specapi.ReasonInterfacesObserved,
		Message: "every declared interface is observed and nothing undeclared is exported",
	}
	if !decision.CodeSynced {
		synced.Status = metav1.ConditionFalse
		synced.Reason = specapi.ReasonInterfacesMissing
		synced.Message = syncMessage(decision)
	}
	conditions = append(conditions, synced)

	drifted := metav1.Condition{
		Type:    specapi.ConditionDrifted,
		Status:  metav1.ConditionFalse,
		Reason:  specapi.ReasonFingerprintEqual,
		Message: "observed facts match the last ingest",
	}
	if decision.Drifted {
		drifted.Status = metav1.ConditionTrue
		drifted.Reason = specapi.ReasonFingerprintChanged
		drifted.Message = "observed facts changed since the last ingest"
	}
	return append(conditions, drifted)
}

func syncMessage(decision specsync.Decision) string {
	parts := []string{}
	if len(decision.Missing) > 0 {
		parts = append(parts, "missing: "+strings.Join(decision.Missing, ", "))
	}
	if len(decision.Undeclared) > 0 {
		parts = append(parts, "undeclared: "+strings.Join(decision.Undeclared, ", "))
	}
	return strings.Join(parts, "; ")
}

// conditionsFor keeps the transition time of a condition whose status did not
// change, which is the Kubernetes convention and makes a repeat ingest a
// no-op write.
func conditionsFor(existing []metav1.Condition, desired []metav1.Condition) []metav1.Condition {
	previous := map[string]metav1.Condition{}
	for _, condition := range existing {
		previous[condition.Type] = condition
	}
	now := metav1.NewTime(time.Now().UTC())
	out := make([]metav1.Condition, 0, len(desired))
	for _, condition := range desired {
		if before, ok := previous[condition.Type]; ok && before.Status == condition.Status {
			condition.LastTransitionTime = before.LastTransitionTime
		} else {
			condition.LastTransitionTime = now
		}
		out = append(out, condition)
	}
	return out
}

func observedObject(observed spec.ObservedFacts) map[string]any {
	encoded, err := json.Marshal(observed)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func specHash(specification *spec.SystemContextSpec) (string, error) {
	hash, err := specapi.HashJSON(*specification)
	if err != nil {
		return "", fmt.Errorf("ingest: hash the spec: %w", err)
	}
	return hash, nil
}

func mergeCodeRefs(existing []string, files []string) []string {
	merged := map[string]bool{}
	for _, ref := range existing {
		if !strings.HasPrefix(ref, spec.CodeRefPrefixFile) {
			merged[ref] = true
		}
	}
	for _, file := range files {
		merged[spec.CodeRefPrefixFile+file] = true
	}
	out := make([]string, 0, len(merged))
	for ref := range merged {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func statusMatches(existing any, desired map[string]any) bool {
	encoded, err := json.Marshal(existing)
	if err != nil {
		return false
	}
	current := map[string]any{}
	if err := json.Unmarshal(encoded, &current); err != nil {
		return false
	}
	normalizedDesired := normalize(desired).(map[string]any)
	for key, value := range normalizedDesired {
		if !reflect.DeepEqual(current[key], value) {
			return false
		}
	}
	return true
}

func normalize(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return value
	}
	return out
}

func sanitizeName(value string) string {
	builder := strings.Builder{}
	lastDash := false
	for _, char := range strings.ToLower(value) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		case (char == '-' || char == '_' || char == '.') && !lastDash && builder.Len() > 0:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		return "repository"
	}
	return name
}
