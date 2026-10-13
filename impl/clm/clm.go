package clm

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/finding"
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

	Reader Reader

	RepoPath string

	Turn int

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
	if unit, line, directive := DirectiveUnit(parsed.Declared); directive {
		if err := openDirectiveFinding(ctx, options, systemContext, unit, line); err != nil {
			fmt.Fprintf(options.Summary, "clm: could not record the quarantined directive of %s: %v\n", systemContext.Name, err)
		}
		return result, fmt.Errorf("clm: the model zone of %s carries an instruction addressed to an agent in %s (%q); the apply is refused and the text is quarantined as a finding, so the spec is unchanged",
			systemContext.Name, unit, line)
	}
	if err := writeNotes(ctx, options, systemContext.Name, modelZone); err != nil {
		fmt.Fprintf(options.Summary, "clm: could not record the notes of %s: %v\n", systemContext.Name, err)
	}
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

// directivePrefixes are the words a line may open with without losing its
// imperative shape: a list marker, a politeness and an auxiliary.
var directivePrefixes = []string{
	"-", "*", "+", ">", "#", "1.", "2.", "3.",
	"please ", "you must ", "you should ", "you need to ", "you have to ",
	"must ", "always ", "never ", "do not ", "don't ", "now ",
}

// directivePatterns are the imperative shapes of an instruction addressed to
// an agent: leave your instructions or your rules, run a command or a tool, or
// read a credential path or a secret file. Every pattern is anchored at the
// start of the line, so prose that only mentions an instruction or a command
// does not match.
var directivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(ignore|disregard|forget|override|bypass|skip)\b.{0,48}\b(instruction|rule|prompt|guideline|constraint|directive|guardrail)s?\b`),
	regexp.MustCompile("(?i)^(run|execute|invoke|call|launch|spawn|install|download)\\b.{0,48}(`|\\$\\()?\\s*(rm|curl|wget|bash|sh|zsh|sudo|chmod|chown|docker|kubectl|git|python|python3|node|npm|deno|go|make|cat|echo|ssh|scp|nc)\\b"),
	regexp.MustCompile(`(?i)^(read|open|cat|print|show|send|upload|copy|post|reveal|exfiltrate|steal|dump|fetch|list)\b.{0,64}(credential|secret|password|passwd|token|api[-_ ]?key|private[-_ ]?key|\.ssh|id_rsa|\.env\b|/etc/shadow|kubeconfig|\.aws|\.netrc|\.npmrc)`),
}

// Directive is the first line of text that tells an agent what to do, and
// whether text carries one. A line is a directive only in the imperative
// shape, so an ordinary sentence that happens to carry the words instruction
// or command is not one.
func Directive(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if isDirectiveLine(line) {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

func isDirectiveLine(line string) bool {
	candidate := strings.TrimSpace(line)
	for {
		trimmed := candidate
		for _, prefix := range directivePrefixes {
			if strings.HasPrefix(strings.ToLower(trimmed), prefix) {
				trimmed = strings.TrimSpace(trimmed[len(prefix):])
			}
		}
		if trimmed == candidate {
			break
		}
		candidate = trimmed
	}
	if candidate == "" {
		return false
	}
	for _, pattern := range directivePatterns {
		if pattern.MatchString(candidate) {
			return true
		}
	}
	return false
}

// DirectiveUnit scans the model zone of a context document and names the first
// unit whose text carries a directive, so the apply can refuse it and tell the
// finding which unit the text tried to change.
func DirectiveUnit(declared spec.SystemContextSpec) (string, string, bool) {
	type written struct {
		unit string
		text string
	}
	units := []written{{unit: authority.UnitIntent, text: declared.Intent}}
	for _, requirement := range declared.Requirements {
		units = append(units, written{unit: authority.RequirementUnit(requirement.ID), text: requirement.Text})
	}
	for _, declaredInterface := range declared.Interfaces {
		units = append(units, written{
			unit: authority.InterfaceUnit(declaredInterface.Name),
			text: declaredInterface.Name + "\n" + declaredInterface.Signature + "\n" + declaredInterface.File,
		})
	}
	for _, interaction := range declared.Interactions {
		units = append(units, written{
			unit: authority.InteractionUnit(interaction.ID),
			text: interaction.Peer + "\n" + interaction.Initiator + "\n" + interaction.Channel + "\n" + interaction.Purpose,
		})
	}
	for _, entry := range units {
		if line, found := Directive(entry.text); found {
			return entry.unit, line, true
		}
	}
	return "", "", false
}

// openDirectiveFinding records the quarantine: the directive's first line as
// the summary and the unit it tried to change. The finding is Open, because a
// person has to decide what the text meant before it can come near the spec.
func openDirectiveFinding(ctx context.Context, options Options, systemContext *spec.SystemContext, unit, line string) error {
	summary := line
	if len(summary) > findingSummaryLimit {
		summary = summary[:findingSummaryLimit]
	}
	wanted := spec.Finding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      finding.Name(systemContext.Name, unit),
			Namespace: systemContext.Namespace,
		},
		Spec: spec.FindingSpec{
			Repository: systemContext.Spec.Repository,
			Source:     finding.SourceCLM,
			Context:    systemContext.Name,
			Unit:       unit,
			Severity:   finding.SeverityInfo,
			Summary:    summary,
		},
		Status: spec.FindingStatus{State: finding.Open},
	}
	spec.SetDefaults(&wanted)
	object, err := kcpclient.Unstructured(&wanted)
	if err != nil {
		return err
	}
	if _, err := options.Cluster.Apply(ctx, object); err != nil {
		return fmt.Errorf("clm: record the quarantined directive of %s: %w", unit, err)
	}
	return nil
}

// findingSummaryLimit bounds the quarantine summary the way a proposal finding
// is bounded.
const findingSummaryLimit = 500

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
