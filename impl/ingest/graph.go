package ingest

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// rebuildMu serializes the whole-graph rewrite. A rebuild deletes every
// managed vertex and writes them again, so two of them at once would interleave
// and one would wipe the other's vertices; a controller with several workers
// can have two ingests finish together.
var rebuildMu sync.Mutex

func RebuildGraph(ctx context.Context, cluster Cluster, writer graph.Writer, options Options) error {
	rebuildMu.Lock()
	defer rebuildMu.Unlock()

	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	snapshots, err := snapshotsFor(ctx, cluster, namespace)
	if err != nil {
		return err
	}
	return graph.Rebuild(ctx, writer, snapshots)
}

func snapshotsFor(ctx context.Context, cluster Cluster, namespace string) ([]graph.Snapshot, error) {
	listed, err := cluster.List(ctx, specapi.RepositoryGVR, namespace)
	if err != nil {
		return nil, err
	}
	snapshots := []graph.Snapshot{}
	for _, item := range listed.Items {
		typed, err := kcpclient.Typed(&item)
		if err != nil {
			return nil, err
		}
		repository, ok := typed.(*spec.Repository)
		if !ok {
			continue
		}
		contexts, err := contextsOf(ctx, cluster, namespace, repository.Name)
		if err != nil {
			return nil, err
		}
		codeRefs, err := codeRefsOf(ctx, repository, contexts)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, graph.Snapshot{
			Repository: *repository,
			Contexts:   contexts,
			CodeRefs:   codeRefs,
		})
	}
	return snapshots, nil
}

func contextsOf(ctx context.Context, cluster Cluster, namespace, repositoryName string) ([]spec.SystemContext, error) {
	listed, err := cluster.List(ctx, specapi.SystemContextGVR, namespace)
	if err != nil {
		return nil, err
	}
	contexts := []spec.SystemContext{}
	for _, item := range listed.Items {
		if repositoryOf(&item) != repositoryName {
			continue
		}
		typed, err := kcpclient.Typed(&item)
		if err != nil {
			return nil, err
		}
		context, ok := typed.(*spec.SystemContext)
		if !ok {
			continue
		}
		contexts = append(contexts, *context)
	}
	return contexts, nil
}

func repositoryOf(item *unstructured.Unstructured) string {
	value, _, err := unstructured.NestedString(item.Object, "spec", "repository")
	if err != nil {
		return ""
	}
	return value
}

func codeRefsOf(ctx context.Context, repository *spec.Repository, contexts []spec.SystemContext) (map[string]graph.CodeRef, error) {
	refs := []string{}
	for _, context := range contexts {
		refs = append(refs, context.Spec.CodeRefs...)
		for _, requirement := range context.Spec.Requirements {
			refs = append(refs, requirement.CodeRefs...)
		}
	}
	if len(refs) == 0 {
		return map[string]graph.CodeRef{}, nil
	}
	repoPath := repository.Spec.Path
	if !filepath.IsAbs(repoPath) {
		absolute, err := filepath.Abs(repoPath)
		if err != nil {
			return map[string]graph.CodeRef{}, nil
		}
		repoPath = absolute
	}
	database, found, err := codegraphsqlite.OpenRepo(repoPath)
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]graph.CodeRef{}, nil
	}
	defer database.Close()
	return resolveRefs(ctx, database, refs)
}

func resolveRefs(ctx context.Context, database *codegraphsqlite.DB, refs []string) (map[string]graph.CodeRef, error) {
	resolved := map[string]graph.CodeRef{}
	for _, ref := range refs {
		if _, done := resolved[ref]; done {
			continue
		}
		node, ok, err := resolveRef(ctx, database, ref)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		resolved[ref] = node
	}
	return resolved, nil
}

func resolveRef(ctx context.Context, database *codegraphsqlite.DB, ref string) (graph.CodeRef, bool, error) {
	payload := ref
	for _, prefix := range spec.CodeRefPrefixes {
		if trimmed, found := strings.CutPrefix(ref, prefix); found {
			payload = trimmed
			break
		}
	}
	if payload == "" {
		return graph.CodeRef{}, false, nil
	}
	nodes, err := database.Resolve(ctx, payload)
	if err != nil {
		return graph.CodeRef{}, false, err
	}
	if len(nodes) != 1 {
		return graph.CodeRef{}, false, nil
	}
	node := nodes[0]
	return graph.CodeRef{
		CodegraphID: node.ID,
		Kind:        node.Kind,
		Name:        firstNonEmpty(node.QualifiedName, node.Name),
		FilePath:    node.FilePath,
	}, true, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
