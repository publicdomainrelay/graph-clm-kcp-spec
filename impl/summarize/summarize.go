package summarize

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"

	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
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

	Update(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

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

	Warnings []string

	SpecHash string

	Applied bool

	ContextDoc string

	Proposals []spec.Proposal

	Declared []string
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
	result.Warnings = draft.Warnings

	write, err := guardedWrite(ctx, options, namespace, systemContext, draft)
	if err != nil {
		return result, err
	}
	systemContext = write.context
	merged := write.plan.Spec
	realized := write.plan.Realized
	result.Proposals = write.plan.Proposals
	result.Declared = write.plan.Declared.Sorted()
	mergedHash, err := spec.HashSystemContextSpec(merged)
	if err != nil {
		return result, err
	}
	realizedHash, err := spec.HashSystemContextSpec(realized)
	if err != nil {
		return result, err
	}
	result.SpecHash = mergedHash
	generation := write.generation
	result.Applied = write.applied

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
		"realizedSpecHash":   realizedHash,
		"realizedSpec":       &realized,
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

const writeAttempts = 5

type written struct {
	context *spec.SystemContext

	plan authority.Write

	generation int64

	applied bool
}

// guardedWrite stores a draft against the spec as it is when the agent is
// done, not as it was when the agent started: a person can edit the spec while
// the model works, and the update carries the resource version it read, so an
// edit that lands in between is merged again instead of overwritten.
func guardedWrite(ctx context.Context, options Options, namespace string, started *spec.SystemContext, draft agent.SpecDraft) (written, error) {
	current := started
	for attempt := 1; ; attempt++ {
		recorded, err := authority.Parse(current.Annotations[specapi.DeclaredAnnotation])
		if err != nil {
			return written{}, fmt.Errorf("summarize: %s: %w", options.Context, err)
		}
		recorded = recorded.Union(authority.Changed(started.Spec, current.Spec))
		drafted, validation := agent.ValidateDraft(options.Context, current.Spec, current.Status.Observed, draft)
		if !validation.OK() {
			return written{}, fmt.Errorf("summarize: the draft of %s does not validate: %w", options.Context, validation.Err())
		}
		plan := authority.Merge(recorded, current.Spec, current.Status.RealizedSpec, drafted)
		out := written{context: current, plan: plan, generation: current.GetGeneration()}

		mergedHash, err := spec.HashSystemContextSpec(plan.Spec)
		if err != nil {
			return written{}, err
		}
		realizedHash, err := spec.HashSystemContextSpec(plan.Realized)
		if err != nil {
			return written{}, err
		}
		previousHash, previousErr := spec.HashSystemContextSpec(current.Spec)
		if previousErr == nil && previousHash == mergedHash {
			return out, nil
		}

		updated := &spec.SystemContext{
			ObjectMeta: *current.ObjectMeta.DeepCopy(),
			Spec:       plan.Spec,
			Status:     current.Status,
		}
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}
		updated.Annotations[specapi.OriginAnnotation] = specapi.OriginIngest
		if mergedHash == realizedHash {
			updated.Annotations[specapi.OriginHashAnnotation] = mergedHash
		} else {
			delete(updated.Annotations, specapi.OriginHashAnnotation)
		}
		updated.SetDefaults()
		stamped, err := kcpclient.Unstructured(updated)
		if err != nil {
			return written{}, err
		}
		applied, err := options.Cluster.Update(ctx, stamped)
		if err == nil {
			out.generation = applied.GetGeneration()
			out.applied = true
			return out, nil
		}
		if !kcpclient.IsConflict(err) || attempt >= writeAttempts {
			return written{}, err
		}
		object, err := options.Cluster.Get(ctx, specapi.SystemContextGVR, namespace, options.Context)
		if err != nil {
			return written{}, fmt.Errorf("summarize: re-read systemcontext %s: %w", options.Context, err)
		}
		typed, err := kcpclient.Typed(object)
		if err != nil {
			return written{}, err
		}
		fresh, ok := typed.(*spec.SystemContext)
		if !ok {
			return written{}, fmt.Errorf("summarize: %s is not a SystemContext", options.Context)
		}
		current = fresh
	}
}
