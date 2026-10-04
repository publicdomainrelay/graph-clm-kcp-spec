package summarize

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"

	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/bundle"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type Options struct {
	Cluster Cluster

	Namespace string

	Context string

	Repository *spec.Repository

	Agent agent.Agent

	Writer graph.Writer

	Codegraph bundle.Codegraph

	Budget int

	NodeLimit int

	ManagedBudget int

	DocDir string
}

type Result struct {
	Context string

	Draft agent.SpecDraft

	Dropped []agent.DroppedRef

	SpecHash string

	Applied bool

	ContextDoc string
}

func Run(ctx context.Context, options Options) (Result, error) {
	namespace := options.Namespace
	if namespace == "" {
		namespace = specapi.DefaultNamespace
	}
	result := Result{Context: options.Context}

	object, err := options.Cluster.Get(ctx, specapi.SystemContextGVR, namespace, options.Context)
	if err != nil {
		return result, fmt.Errorf("summarize: read systemcontext %s: %w", options.Context, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return result, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return result, fmt.Errorf("summarize: %s is not a SystemContext", options.Context)
	}

	built, err := bundle.Build(ctx, bundle.Options{
		Cluster:       options.Cluster,
		Namespace:     namespace,
		Context:       options.Context,
		Repository:    options.Repository,
		Writer:        options.Writer,
		Codegraph:     options.Codegraph,
		Budget:        options.Budget,
		NodeLimit:     options.NodeLimit,
		ManagedBudget: options.ManagedBudget,
	})
	if err != nil {
		return result, err
	}

	draft, err := options.Agent.Summarize(ctx, built)
	if err != nil {
		return result, err
	}
	result.Draft = draft
	result.Dropped = draft.Dropped

	merged, validation := agent.ValidateDraft(options.Context, systemContext.Spec, systemContext.Status.Observed, draft)
	if !validation.OK() {
		return result, fmt.Errorf("summarize: the draft of %s does not validate: %w", options.Context, validation.Err())
	}
	mergedHash, err := spec.HashSystemContextSpec(merged)
	if err != nil {
		return result, err
	}
	result.SpecHash = mergedHash

	generation := systemContext.GetGeneration()
	previousHash, previousErr := spec.HashSystemContextSpec(systemContext.Spec)
	if previousErr != nil || previousHash != mergedHash {
		updated := &spec.SystemContext{
			ObjectMeta: *systemContext.ObjectMeta.DeepCopy(),
			Spec:       merged,
			Status:     systemContext.Status,
		}
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}
		updated.Annotations[specapi.OriginAnnotation] = specapi.OriginIngest
		updated.Annotations[specapi.OriginHashAnnotation] = mergedHash
		updated.SetDefaults()
		stamped, err := kcpclient.Unstructured(updated)
		if err != nil {
			return result, err
		}
		applied, err := options.Cluster.Apply(ctx, stamped)
		if err != nil {
			return result, err
		}
		generation = applied.GetGeneration()
		result.Applied = true
	}

	observed := systemContext.Status.Observed
	_, conditions := specsync.Conditions(specsync.Input{
		Name:              options.Context,
		Generation:        generation,
		Spec:              merged,
		Observed:          observed,
		SyncedFingerprint: observed.Fingerprint,
	}, systemContext.Status.Conditions)

	status := map[string]any{
		"observedGeneration": generation,
		"realizedSpecHash":   mergedHash,
		"realizedSpec":       &merged,
		"syncedCommit":       systemContext.Status.ObservedCommit,
		"syncedFingerprint":  observed.Fingerprint,
		"syncedObserved":     observed,
		"conditions":         conditions,
	}
	if !specapi.StatusMatches(systemContext.Status, status) {
		if _, err := options.Cluster.PatchStatus(ctx, specapi.SystemContextGVR, namespace, options.Context, status); err != nil {
			return result, err
		}
	}

	if options.Repository != nil && options.Repository.WorkPath() != "" {
		modelZone, err := clm.RenderModelZoneWithProse(options.Context, options.Repository.Name, draft.Summary, merged)
		if err != nil {
			return result, err
		}
		docDir := options.DocDir
		if docDir == "" {
			docDir = statedir.ClmDocDir()
		}
		path, err := bundle.WriteContextDoc(docDir, options.Repository.Name, options.Context, modelZone,
			bundle.ResolvedRefs(observed), options.ManagedBudget)
		if err != nil {
			return result, err
		}
		result.ContextDoc = path
	}
	return result, nil
}
