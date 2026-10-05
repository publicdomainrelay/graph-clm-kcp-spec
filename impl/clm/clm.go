package clm

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
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

	GraphNamespace string

	ManagedBudget int

	AllowRemove []string

	Summary io.Writer

	Now func() time.Time
}

func (o Options) graphNamespace() string {
	if o.GraphNamespace != "" {
		return o.GraphNamespace
	}
	return graphns.FromEnv()
}

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

type ApplyResult struct {
	Context string

	Delta spec.Delta

	Applied bool

	SpecHash string

	Queued string

	Message string
}

func Apply(ctx context.Context, options Options, modelZone string) (ApplyResult, error) {
	systemContext, err := readContext(ctx, options)
	if err != nil {
		return ApplyResult{}, err
	}
	parsed, err := clm.ParseDocument(modelZone)
	if err != nil {
		return ApplyResult{}, err
	}
	merged := clm.MergeDeclared(systemContext.Spec, parsed.Declared)
	change := delta.Diff(systemContext.Spec, merged)
	result := ApplyResult{Context: systemContext.Name, Delta: change}
	if change.Empty() {
		result.Message = "the model zone says what the spec already says"
		return result, nil
	}
	writeDetails(options.Summary, change)
	if err := refuseImplicitRemoval(systemContext.Name, change, parsed.Removed, options.AllowRemove); err != nil {
		return result, err
	}

	queued, err := unsettledRunningChange(ctx, options, systemContext.Name)
	if err != nil {
		return result, err
	}
	result.Queued = queued

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
	delete(candidate.Annotations, specapi.OriginHashAnnotation)

	stamped, err := kcpclient.Unstructured(candidate)
	if err != nil {
		return result, err
	}
	if _, err := options.Cluster.Apply(ctx, stamped); err != nil {
		return result, err
	}
	result.Applied = true

	return result, nil
}

func writeDetails(writer io.Writer, change spec.Delta) {
	if writer == nil {
		return
	}
	for _, line := range delta.Details(change) {
		fmt.Fprintln(writer, line)
	}
}

// refuseImplicitRemoval stops an apply that would delete requirements the
// document does not name. A model zone that lost a requirement because an
// operator sliced the document is indistinguishable from one that removed it on
// purpose; the operator has to say which, by listing the id in the document's
// `removed:` marker or passing it to --allow-remove.
func refuseImplicitRemoval(systemContext string, change spec.Delta, declared, allowed []string) error {
	removedRequirements := delta.RemovedRequirementIDs(change)
	removedInteractions := delta.RemovedInteractionIDs(change)
	if len(removedRequirements) == 0 && len(removedInteractions) == 0 {
		return nil
	}
	permitted := map[string]bool{}
	for _, id := range declared {
		permitted[id] = true
	}
	for _, id := range allowed {
		permitted[strings.TrimSpace(id)] = true
	}
	unlisted := []string{}
	for _, id := range removedRequirements {
		if !permitted[id] {
			unlisted = append(unlisted, id)
		}
	}
	for _, id := range removedInteractions {
		if !permitted[id] {
			unlisted = append(unlisted, id)
		}
	}
	if len(unlisted) == 0 {
		return nil
	}
	sort.Strings(unlisted)
	return fmt.Errorf("clm: the model zone of %s removes requirement(s) or interaction(s) %s; list each id under `removed:` in the spec block or pass --allow-remove to remove them on purpose",
		systemContext, strings.Join(unlisted, ", "))
}

// unsettledRunningChange names the SpecToCode change the context's own edit
// queues behind: a change already realizing the context. The apply still writes
// the spec; specd raises the edit as its own Pending change, and the
// per-repository serialization admits it once the running change settles. The
// edit is never folded into the running change, so its delta is not lost.
func unsettledRunningChange(ctx context.Context, options Options, systemContext string) (string, error) {
	running, err := runningChange(ctx, options, systemContext)
	if err != nil {
		return "", err
	}
	if running == nil {
		return "", nil
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
		if err := writeLive(ctx, options.Writer, options.graphNamespace(), *change, record, result.Index); err != nil {
			return result, err
		}
	}
	return result, nil
}

func writeLive(ctx context.Context, writer graph.Writer, namespace string, change spec.SpecChange, record spec.ProgressRecord, index int) error {
	vertices, edges := graph.LiveTable()
	changeIndex, progressIndex := 0, 1
	touchedIndex, occurredIndex := 0, 1

	changeID := graph.ChangeIDIn(namespace, change.Name)
	vertices[changeIndex].Rows = append(vertices[changeIndex].Rows, graph.ChangeVertexIn(namespace, change))
	for _, file := range record.Files {
		vertex := graph.FileCodeRefIn(namespace, file)
		vertices = append(vertices, graph.VertexSet{Label: graph.LabelCodeRef, Rows: []graph.Vertex{vertex}})
		edges[touchedIndex].Rows = append(edges[touchedIndex].Rows,
			graph.Edge{From: changeID, To: vertex.ID})
	}
	if record.Tool != "" || record.Note != "" || record.Turn > 0 {
		vertices[progressIndex].Rows = append(vertices[progressIndex].Rows,
			graph.ProgressVertexIn(namespace, change.Name, index, record))
		edges[occurredIndex].Rows = append(edges[occurredIndex].Rows,
			graph.Edge{From: changeID, To: graph.ProgressIDIn(namespace, change.Name, index)})
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

func resolvedRefs(systemContext *spec.SystemContext) []agent.ResolvedRef {
	return agent.ContextRefs(systemContext.Spec, systemContext.Status.Observed)
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
