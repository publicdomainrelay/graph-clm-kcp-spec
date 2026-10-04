package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/archkcp"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/kcp-libs/common/condition"
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

	Partition string

	Include []string

	Exclude []string

	Adopt *spec.SystemContextSpec

	AdoptAll map[string]*spec.SystemContextSpec

	RootContext bool

	ArchPath string

	Writer graph.Writer

	GraphNamespace string
}

type ContextResult struct {
	Name        string
	Created     bool
	SpecChanged bool
	StatusWrote bool
	Fingerprint string
	Observed    spec.ObservedFacts
	Conditions  []metav1.Condition
	Skipped     bool
	Reason      string
}

type Result struct {
	Repository string

	Commit string

	SpecPath string

	Contexts []ContextResult

	Seeded []string
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
	treeFiles, err := gitrepo.TrackedFiles(ctx, repoPath)
	if err != nil {
		treeFiles = nil
	}
	resolver := specsync.ModuleResolver{ModulePath: GoModulePath(repoPath), ImportMap: DenoImportMap(repoPath)}
	partitionOptions := specsync.PartitionOptions{
		Mode:           options.Partition,
		Include:        options.Include,
		Exclude:        options.Exclude,
		RepositoryName: repositoryName,
		TreeFiles:      treeFiles,
		ModulePath:     resolver.ModulePath,
		ImportMap:      resolver.ImportMap,
		Generated:      GeneratedDirectories(repoPath, facts.Files),
		RootContext:    options.RootContext,
	}
	if options.Partition == spec.PartitionPackage {
		roots, err := PackageRoots(repoPath)
		if err != nil {
			return Result{}, err
		}
		partitionOptions.Roots = withoutGenerated(roots, partitionOptions.Generated)
	}
	partitions := specsync.PartitionFactsWith(facts, partitionOptions)
	dependencies := specsync.PartitionDependencies(partitions, facts.Imports, resolver)
	for index := range partitions {
		partitions[index].DependsOn = dependencies[partitions[index].Name]
	}

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
		contextResult, err := ingestPartition(ctx, cluster, repositoryName, namespace, commit, partition, options)
		if err != nil {
			return Result{}, err
		}
		result.Contexts = append(result.Contexts, contextResult)
	}

	seeded, err := seedFromArch(ctx, cluster, repoPath, repositoryName, namespace, partitions, options)
	if err != nil {
		return Result{}, err
	}
	result.Seeded = seeded

	if options.Writer != nil {
		if err := RebuildGraph(ctx, cluster, options.Writer, options); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func seedFromArch(
	ctx context.Context,
	cluster Cluster,
	repoPath, repositoryName, namespace string,
	partitions []specsync.Partition,
	options Options,
) ([]string, error) {
	path := options.ArchPath
	explicit := path != ""
	if path == "" {
		path = spec.DefaultArchPath
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoPath, filepath.FromSlash(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if explicit || !os.IsNotExist(err) {
			return nil, fmt.Errorf("ingest: read %s: %w", path, err)
		}
		return nil, nil
	}
	seeded, err := archkcp.Seed(ctx, cluster, archkcp.SeedOptions{
		Namespace:   namespace,
		Repository:  repositoryName,
		Data:        data,
		Partitions:  partitions,
		RootContext: options.RootContext,
	})
	if err != nil {
		return nil, err
	}
	return seeded.Seeded, nil
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

	conditions := condition.Copy(repository.Status.Conditions)
	condition.SetTrue(&conditions, repository.GetGeneration(), specapi.ConditionIndexed,
		specapi.ReasonIndexed, "the codegraph index is current")
	status := map[string]any{
		"headCommit":         commit,
		"indexedCommit":      commit,
		"observedGeneration": repository.GetGeneration(),
		"conditions":         conditions,
	}
	if specapi.StatusMatches(repository.Status, status) {
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
	options Options,
) (ContextResult, error) {
	adopt := options.Adopt
	if specific, ok := options.AdoptAll[partition.Name]; ok {
		adopt = specific
	}
	observed := specsync.Observed(partition)
	result := ContextResult{Name: partition.Name, Fingerprint: observed.Fingerprint, Observed: observed}

	var existingContext *spec.SystemContext
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
	case kcpclient.IsNotFound(err):
		existingContext = &spec.SystemContext{ObjectMeta: metav1.ObjectMeta{Name: partition.Name, Namespace: namespace}}
		result.Created = true
	default:
		return result, err
	}

	if owner := existingContext.Spec.Repository; owner != "" && owner != repositoryName {
		result.Skipped = true
		result.Reason = "the context belongs to repository " + owner
		return result, nil
	}

	previousSpec := existingContext.Spec
	previousHash, previousErr := spec.HashSystemContextSpec(previousSpec)
	adopted := adopt != nil && spec.SameDeclaredState(previousSpec, *adopt)

	syncedFingerprint := existingContext.Status.SyncedFingerprint
	syncedCommit := existingContext.Status.SyncedCommit
	syncedObserved := existingContext.Status.SyncedObserved
	if syncedFingerprint == "" || adopted {
		syncedFingerprint = observed.Fingerprint
		syncedCommit = commit
		syncedObserved = observed
	}

	merged := previousSpec
	if merged.Repository == "" {
		merged.Repository = repositoryName
	}
	if merged.Upstream == "" {
		merged.Upstream = spec.RefSelf
	}
	if len(merged.DependsOn) == 0 {
		merged.DependsOn = specsync.DependencyRefs(partition.DependsOn)
	}
	merged.CodeRefs = mergeCodeRefs(merged.CodeRefs, observed.Files)
	merged = specsync.ReanchorRefs(merged, existingContext.Status.SyncedObserved, observed)
	merged = specsync.ReanchorRefs(merged, existingContext.Status.Observed, observed)
	merged = specsync.MigrateDeclared(merged, observed)

	specChanged := !reflect.DeepEqual(merged, previousSpec)
	generation := existingContext.GetGeneration()

	realizedSpecHash := existingContext.Status.RealizedSpecHash
	mergedHash, mergedErr := spec.HashSystemContextSpec(merged)
	absorbed := previousErr == nil && mergedErr == nil && (realizedSpecHash == "" || realizedSpecHash == previousHash)
	if adopted {
		absorbed = mergedErr == nil
	}
	realizedSpec := existingContext.Status.RealizedSpec
	if absorbed {
		realizedSpecHash = mergedHash
		snapshot := merged
		realizedSpec = &snapshot
	}

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
		delete(updated.Annotations, specapi.OriginHashAnnotation)
		if absorbed {
			if mergedHash, err := spec.HashSystemContextSpec(merged); err == nil {
				updated.Annotations[specapi.OriginHashAnnotation] = mergedHash
			}
		}
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

	_, conditions := specsync.Conditions(specsync.Input{
		Name:              partition.Name,
		Generation:        generation,
		Spec:              merged,
		Observed:          observed,
		SyncedFingerprint: syncedFingerprint,
	}, existingContext.Status.Conditions)
	result.Conditions = conditions

	status := map[string]any{
		"observedGeneration": generation,
		"observedCommit":     commit,
		"observed":           observedObject(observed),
		"syncedCommit":       syncedCommit,
		"syncedFingerprint":  syncedFingerprint,
		"syncedObserved":     observedObject(syncedObserved),
		"realizedSpecHash":   realizedSpecHash,
		"conditions":         conditions,
	}
	if realizedSpec != nil {
		status["realizedSpec"] = realizedSpec
	}

	if specapi.StatusMatches(existingContext.Status, status) {
		return result, nil
	}
	if _, err := cluster.PatchStatus(ctx, specapi.SystemContextGVR, namespace, partition.Name, status); err != nil {
		return result, err
	}
	result.StatusWrote = true
	return result, nil
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

func SanitizeName(value string) string {
	return sanitizeName(value)
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
