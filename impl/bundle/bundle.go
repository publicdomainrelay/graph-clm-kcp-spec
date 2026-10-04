// Package bundle builds what the model reads about one context: the spec, the
// observed facts, the context document, one hop of the spec graph and the
// codegraph excerpts behind the code refs, cut to a token budget.
package bundle

import (
	"context"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const (
	DefaultBudget = 8000

	DefaultNodeLimit = 3

	DefaultTaskNames = 8
)

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
}

// Codegraph is the two codegraph reads a bundle needs. It is an interface so a
// test can answer without the command, and so the builder works when the
// caller has no codegraph at all.
type Codegraph interface {
	Context(ctx context.Context, task string, maxNodes int) (string, error)

	Node(ctx context.Context, name string) (string, error)
}

type Options struct {
	Cluster Cluster

	Namespace string

	Context string

	Repository *spec.Repository

	Writer graph.Writer

	Codegraph Codegraph

	Budget int

	NodeLimit int

	ManagedBudget int
}

// Build reads one context and everything one hop from it. The graph and
// codegraph are optional: without them the bundle is the spec, the facts and
// the context document, which is still a usable ask.
func Build(ctx context.Context, options Options) (agent.ContextBundle, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	budget := options.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	nodeLimit := options.NodeLimit
	if nodeLimit <= 0 {
		nodeLimit = DefaultNodeLimit
	}

	object, err := options.Cluster.Get(ctx, specapi.SystemContextGVR, namespace, options.Context)
	if err != nil {
		return agent.ContextBundle{}, fmt.Errorf("bundle: read systemcontext %s: %w", options.Context, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return agent.ContextBundle{}, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return agent.ContextBundle{}, fmt.Errorf("bundle: %s is not a SystemContext", options.Context)
	}

	repository := options.Repository
	if repository == nil && systemContext.Spec.Repository != "" {
		repository, err = readRepository(ctx, options.Cluster, namespace, systemContext.Spec.Repository)
		if err != nil {
			return agent.ContextBundle{}, err
		}
	}
	repositoryName := systemContext.Spec.Repository
	repoPath := ""
	if repository != nil {
		repoPath = repository.WorkPath()
	}

	bundle := agent.ContextBundle{
		Context:    options.Context,
		Repository: repositoryName,
		Spec:       systemContext.Spec,
		Observed:   systemContext.Status.Observed,
		Budget:     budget,
	}

	if repoPath != "" {
		document, err := ReadContextDoc(repoPath, options.Context)
		if err != nil {
			return agent.ContextBundle{}, err
		}
		bundle.ContextDoc = document
	} else if repository == nil {
		return agent.ContextBundle{}, fmt.Errorf("bundle: repository %q of %s was not found", repositoryName, options.Context)
	}

	if options.Writer != nil {
		neighbors, err := Neighbors(ctx, options.Writer, options.Context)
		if err != nil {
			return agent.ContextBundle{}, err
		}
		bundle.Neighbors = neighbors
	}
	// Without an index there is nothing for the CLI to read, and spawning it
	// twice per context to be told so is waste.
	if options.Codegraph != nil && repoPath != "" {
		if _, err := os.Stat(codegraphsqlite.DatabasePath(repoPath)); err == nil {
			bundle.CodeExcerpts = excerpts(ctx, options.Codegraph, bundle, nodeLimit)
		}
	}
	return bundle, nil
}

func readRepository(ctx context.Context, cluster Cluster, namespace, name string) (*spec.Repository, error) {
	object, err := cluster.Get(ctx, specapi.RepositoryGVR, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("bundle: read repository %s: %w", name, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	repository, ok := typed.(*spec.Repository)
	if !ok {
		return nil, fmt.Errorf("bundle: %s is not a Repository", name)
	}
	return repository, nil
}

// Neighbors is one hop in and out of a context in the spec graph. The graph is
// an index of kcp plus CodeGraph, so the read uses the same edge table the
// writer uses and the two cannot disagree about a direction.
func Neighbors(ctx context.Context, writer graph.Writer, contextName string) ([]agent.Neighbor, error) {
	id := graph.ContextID(contextName)
	out := []agent.Neighbor{}
	for _, edgeSpec := range graph.EdgeSpecs {
		if edgeSpec.FromLabel == graph.LabelContext {
			rows, err := writer.SelectOut(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id,
				graph.LabelProperties[edgeSpec.ToLabel])
			if err != nil {
				return nil, fmt.Errorf("bundle: read %s out of %s: %w", edgeSpec.Type, contextName, err)
			}
			for _, row := range rows {
				out = append(out, neighborOf(edgeSpec.Type, agent.DirectionOut, edgeSpec.ToLabel, row))
			}
		}
		if edgeSpec.ToLabel == graph.LabelContext {
			rows, err := writer.SelectIn(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id,
				graph.LabelProperties[edgeSpec.FromLabel])
			if err != nil {
				return nil, fmt.Errorf("bundle: read %s into %s: %w", edgeSpec.Type, contextName, err)
			}
			for _, row := range rows {
				out = append(out, neighborOf(edgeSpec.Type, agent.DirectionIn, edgeSpec.FromLabel, row))
			}
		}
	}
	return out, nil
}

func neighborOf(edge, direction, label string, row map[string]any) agent.Neighbor {
	props := make(map[string]string, len(row))
	for key, value := range row {
		props[key] = fmt.Sprint(value)
	}
	return agent.Neighbor{
		Edge:      edge,
		Direction: direction,
		Label:     label,
		Name:      neighborName(row),
		Props:     props,
	}
}

func neighborName(row map[string]any) string {
	for _, key := range []string{"name", "reqId", "codegraphId"} {
		if value, ok := row[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// excerpts asks codegraph for the source behind the observed interfaces, then
// for the context as a whole. A symbol the index cannot read is skipped: a
// model that is missing one excerpt still gets the spec and the facts.
func excerpts(ctx context.Context, codegraph Codegraph, bundle agent.ContextBundle, nodeLimit int) []agent.CodeExcerpt {
	out := []agent.CodeExcerpt{}
	for index, observedInterface := range bundle.Observed.Interfaces {
		if index >= nodeLimit {
			break
		}
		text, err := codegraph.Node(ctx, observedInterface.Name)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, agent.CodeExcerpt{Source: "codegraph node " + observedInterface.Name, Text: text})
	}

	task := taskWords(bundle)
	if task == "" {
		return out
	}
	text, err := codegraph.Context(ctx, task, DefaultTaskNames)
	if err != nil || strings.TrimSpace(text) == "" {
		return out
	}
	return append(out, agent.CodeExcerpt{Source: "codegraph context", Text: text})
}

func taskWords(bundle agent.ContextBundle) string {
	words := []string{bundle.Context}
	for index, observedInterface := range bundle.Observed.Interfaces {
		if index >= DefaultTaskNames {
			break
		}
		words = append(words, observedInterface.Name)
	}
	return strings.Join(words, " ")
}

// ResolvedRefs is the managed zone of the context document: every observed
// interface, as the id the graph and the spec cite it by. It is derived from
// the facts, never from the model, so the zone is the same on every run.
func ResolvedRefs(observed spec.ObservedFacts) []agent.ResolvedRef {
	return agent.ObservedRefs(observed)
}

// SpecFromCluster is a small helper for callers that already hold an
// unstructured object and want the typed spec.
func SpecFromCluster(object *unstructured.Unstructured) (*spec.SystemContext, error) {
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return nil, fmt.Errorf("bundle: %s is not a SystemContext", object.GetName())
	}
	return systemContext, nil
}
