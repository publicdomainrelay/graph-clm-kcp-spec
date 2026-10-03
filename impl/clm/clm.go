// Package clm is the state bridge between a context language model host and
// kcp. A host has no kube client and no Bolt driver of its own, so it shells
// out to `specctl clm render|apply|report`; this package is what those three
// verbs run. kcp access, the delta authority and the graph writes therefore
// have one implementation (Go) that every host and every language reaches the
// same way.
package clm

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/bundle"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type Cluster interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)

	Apply(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	PatchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, status map[string]any) (*unstructured.Unstructured, error)
}

type Options struct {
	Cluster Cluster

	Namespace string

	Context string

	Writer graph.Writer

	ManagedBudget int

	Now func() time.Time
}

// Document is what one context renders to: the model zone built from the spec
// kcp holds, above the managed zone built from the observed facts. It is the
// same document the summarize path writes, so the file the model reads is the
// file the controller maintains.
type Document struct {
	Context string

	Document string

	ModelZone string

	Spec spec.SystemContextSpec

	Observed spec.ObservedFacts

	Refs []string
}

func (o Options) namespace() string {
	if o.Namespace != "" {
		return o.Namespace
	}
	return specapi.DefaultNamespace
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Render reads one SystemContext and composes its document.
func Render(ctx context.Context, options Options) (Document, error) {
	systemContext, err := readContext(ctx, options)
	if err != nil {
		return Document{}, err
	}
	modelZone, err := clm.RenderModelZone(systemContext.Name, systemContext.Spec.Repository, systemContext.Spec)
	if err != nil {
		return Document{}, err
	}
	refs := resolvedRefs(systemContext)
	document := clm.Document(modelZone, refs, options.ManagedBudget)
	return Document{
		Context:   systemContext.Name,
		Document:  document,
		ModelZone: modelZone,
		Spec:      systemContext.Spec,
		Observed:  systemContext.Status.Observed,
		Refs:      systemContext.Spec.CodeRefs,
	}, nil
}

// ApplyResult is what one apply did: the delta it asked kcp for, whether the
// write happened, and the running change the edit was folded into, if any.
type ApplyResult struct {
	Context string

	Delta spec.Delta

	Applied bool

	SpecHash string

	// Folded names the running SpecToCode change the edit was recorded on. A
	// model editing the spec while its own change runs must not spawn a second
	// change for itself, so the edit is a progress record on the change instead.
	Folded string

	Message string
}

// Apply reads a model zone, diffs it against the spec kcp holds, and writes the
// result with the `origin: clm` annotation. The delta is computed here, in Go,
// and printed by the caller: the host never guesses what changed.
func Apply(ctx context.Context, options Options, modelZone string) (ApplyResult, error) {
	systemContext, err := readContext(ctx, options)
	if err != nil {
		return ApplyResult{}, err
	}
	parsed, err := clm.ParseModelZone(modelZone)
	if err != nil {
		return ApplyResult{}, err
	}
	merged := clm.MergeDeclared(systemContext.Spec, parsed)
	change := delta.Diff(systemContext.Spec, merged)
	result := ApplyResult{Context: systemContext.Name, Delta: change}
	if change.Empty() {
		result.Message = "the model zone says what the spec already says"
		return result, nil
	}

	candidate := &spec.SystemContext{
		ObjectMeta: *systemContext.ObjectMeta.DeepCopy(),
		Spec:       merged,
		Status:     systemContext.Status,
	}
	candidate.SetDefaults()
	if validation := spec.ValidateSystemContext(candidate); !validation.OK() {
		return result, fmt.Errorf("clm: the model zone of %s does not validate: %w", systemContext.Name, validation.Err())
	}
	specHash, err := spec.HashSystemContextSpec(merged)
	if err != nil {
		return result, err
	}
	result.SpecHash = specHash

	if candidate.Annotations == nil {
		candidate.Annotations = map[string]string{}
	}
	candidate.Annotations[specapi.OriginAnnotation] = specapi.OriginCLM
	// origin-hash is deliberately left unset: it marks a write the tool made for
	// itself, and a model's edit is a real edit that must raise a SpecToCode
	// change, not be absorbed.
	delete(candidate.Annotations, specapi.OriginHashAnnotation)

	stamped, err := kcpclient.Unstructured(candidate)
	if err != nil {
		return result, err
	}
	if _, err := options.Cluster.Apply(ctx, stamped); err != nil {
		return result, err
	}
	result.Applied = true

	folded, err := foldIntoRunningChange(ctx, options, systemContext.Name, change, result.SpecHash)
	if err != nil {
		return result, err
	}
	result.Folded = folded
	if folded != "" {
		result.Message = "folded into the running change " + folded
	}
	return result, nil
}

// foldIntoRunningChange records a model's spec edit on the change that is
// already running for the context. It is what keeps a realizing agent from
// raising a change for itself: the controller would otherwise see a spec edit
// and a Running change of the same direction.
func foldIntoRunningChange(ctx context.Context, options Options, systemContext string, change spec.Delta, specHash string) (string, error) {
	running, err := runningChange(ctx, options, systemContext)
	if err != nil {
		return "", err
	}
	if running == nil {
		return "", nil
	}
	event := Event{
		Note: fmt.Sprintf("clm edited the spec (%s); the delta is folded into this change",
			delta.Summary(change)),
		At: options.now(),
	}
	if specHash != "" {
		event.Note += fmt.Sprintf(" [%s]", specHash[:8])
	}
	if _, err := Report(ctx, options, ReportOptions{Change: running.Name, Event: event}); err != nil {
		return "", err
	}
	return running.Name, nil
}

func runningChange(ctx context.Context, options Options, systemContext string) (*spec.SpecChange, error) {
	listed, err := options.Cluster.List(ctx, specapi.SpecChangeGVR, options.namespace())
	if err != nil {
		return nil, err
	}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		candidate, ok := typed.(*spec.SpecChange)
		if !ok {
			continue
		}
		if candidate.Spec.SystemContext != systemContext {
			continue
		}
		if candidate.Status.Phase != specapi.PhaseRunning {
			continue
		}
		if candidate.Spec.Direction != specapi.DirectionSpecToCode {
			continue
		}
		return candidate, nil
	}
	return nil, nil
}

// Event is one thing a host observed. Turn and Tool are the agent loop's;
// Files are the files the tool touched, relative to the managed tree; Note is
// what the host wants a human to read.
type Event struct {
	Turn int `json:"turn,omitempty"`

	Tool string `json:"tool,omitempty"`

	Files []string `json:"files,omitempty"`

	Note string `json:"note,omitempty"`

	At time.Time `json:"-"`
}

type ReportOptions struct {
	Change string

	Event Event
}

type ReportResult struct {
	Change string

	Recorded bool

	Index int

	Progress int
}

// Report appends one progress record to a SpecChange and writes the matching
// graph edges. The status subresource is the durable record; the graph is the
// derived index a reader queries while the change runs.
func Report(ctx context.Context, options Options, request ReportOptions) (ReportResult, error) {
	result := ReportResult{Change: request.Change}
	if request.Change == "" {
		return result, fmt.Errorf("clm: report needs a change name")
	}
	object, err := options.Cluster.Get(ctx, specapi.SpecChangeGVR, options.namespace(), request.Change)
	if err != nil {
		return result, fmt.Errorf("clm: read specchange %s: %w", request.Change, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return result, err
	}
	change, ok := typed.(*spec.SpecChange)
	if !ok {
		return result, fmt.Errorf("clm: %s is not a SpecChange", request.Change)
	}

	at := request.Event.At
	if at.IsZero() {
		// The bridge stamps the record, not the host: a host that runs `report`
		// through a shell has no clock it can trust and no need to pass one.
		at = options.now()
	}
	record := spec.ProgressRecord{
		Turn:  request.Event.Turn,
		Tool:  request.Event.Tool,
		Files: request.Event.Files,
		Note:  request.Event.Note,
		At:    at.UTC().Format(time.RFC3339),
	}
	if change.Status.AppendProgress(record) {
		if _, err := options.Cluster.PatchStatus(ctx, specapi.SpecChangeGVR, options.namespace(), request.Change,
			map[string]any{"progress": change.Status.Progress}); err != nil {
			return result, err
		}
		result.Recorded = true
	}
	result.Index = len(change.Status.Progress)
	result.Progress = len(change.Status.Progress)

	if options.Writer != nil {
		if err := writeLive(ctx, options.Writer, *change, record, result.Index); err != nil {
			return result, err
		}
	}
	return result, nil
}

// writeLive is the graph half of a report: the change, the files it touched,
// and one progress vertex per record. The TOUCHED edges land on the same
// CodeRef vertex an ingest writes for the file, so `specctl graph neighbors`
// joins the work the agent did to the file the index saw.
func writeLive(ctx context.Context, writer graph.Writer, change spec.SpecChange, record spec.ProgressRecord, index int) error {
	vertices, edges := graph.LiveTable()
	changeIndex, progressIndex := 0, 1
	touchedIndex, occurredIndex := 0, 1

	vertices[changeIndex].Rows = append(vertices[changeIndex].Rows, graph.ChangeVertex(change))
	for _, file := range record.Files {
		vertex := graph.FileCodeRef(file)
		vertices = append(vertices, graph.VertexSet{Label: graph.LabelCodeRef, Rows: []graph.Vertex{vertex}})
		edges[touchedIndex].Rows = append(edges[touchedIndex].Rows,
			graph.Edge{From: graph.ChangeID(change.Name), To: vertex.ID})
	}
	if record.Tool != "" || record.Note != "" || record.Turn > 0 {
		vertices[progressIndex].Rows = append(vertices[progressIndex].Rows,
			graph.ProgressVertex(change.Name, index, record))
		edges[occurredIndex].Rows = append(edges[occurredIndex].Rows,
			graph.Edge{From: graph.ChangeID(change.Name), To: graph.ProgressID(change.Name, index)})
	}

	for _, set := range vertices {
		if len(set.Rows) == 0 {
			continue
		}
		if err := writer.WriteVertices(ctx, set); err != nil {
			return fmt.Errorf("clm: write %s: %w", set.Label, err)
		}
	}
	for _, set := range edges {
		if len(set.Rows) == 0 {
			continue
		}
		if err := writer.WriteEdges(ctx, set); err != nil {
			return fmt.Errorf("clm: write %s edges: %w", set.Type, err)
		}
	}
	return nil
}

// resolvedRefs is what the managed zone lists: the interfaces the index
// observed, and the context's own `file:` refs, which are already CodeGraph ids
// and need no lookup.
func resolvedRefs(systemContext *spec.SystemContext) []agent.ResolvedRef {
	refs := bundle.ResolvedRefs(systemContext.Status.Observed)
	seen := map[string]bool{}
	for _, ref := range refs {
		seen[ref.CodegraphID] = true
	}
	for _, codeRef := range systemContext.Spec.CodeRefs {
		fileRef, ok := agent.FileRef(codeRef)
		if !ok || seen[fileRef.CodegraphID] {
			continue
		}
		seen[fileRef.CodegraphID] = true
		refs = append(refs, fileRef)
	}
	return refs
}

func readContext(ctx context.Context, options Options) (*spec.SystemContext, error) {
	if options.Context == "" {
		return nil, fmt.Errorf("clm: a context name is required")
	}
	object, err := options.Cluster.Get(ctx, specapi.SystemContextGVR, options.namespace(), options.Context)
	if err != nil {
		return nil, fmt.Errorf("clm: read systemcontext %s: %w", options.Context, err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		return nil, fmt.Errorf("clm: %s is not a SystemContext", options.Context)
	}
	return systemContext, nil
}
