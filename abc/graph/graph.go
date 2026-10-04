package graph

import (
	"context"
	"fmt"
	"maps"
	pathpkg "path"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
)

const (
	LabelRepo        = "SpecRepo"
	LabelContext     = "SpecContext"
	LabelRequirement = "SpecRequirement"
	LabelInterface   = "SpecInterface"
	LabelCodeRef     = "CodeRef"
)

var ManagedLabels = []string{LabelRepo, LabelContext, LabelRequirement, LabelInterface, LabelCodeRef}

const (
	LabelChange   = "SpecChange"
	LabelProgress = "SpecProgress"
	LabelPiMemory = "PiMemory"
)

var LiveLabels = []string{LabelChange, LabelProgress}

const (
	EdgeHasContext   = "HAS_CONTEXT"
	EdgeRequires     = "REQUIRES"
	EdgeDeclares     = "DECLARES"
	EdgeReferences   = "REFERENCES"
	EdgeUpstream     = "UPSTREAM"
	EdgeOverlay      = "OVERLAY"
	EdgeOrchestrator = "ORCHESTRATOR"
	EdgeDependsOn    = "DEPENDS_ON"
	EdgeIntroduces   = "INTRODUCES"
	EdgeTouched      = "TOUCHED"
	EdgeOccurred     = "OCCURRED"
	EdgeSpecifies    = "SPECIFIES"
)

var LiveEdgeSpecs = []EdgeSpec{
	{Type: EdgeTouched, FromLabel: LabelChange, ToLabel: LabelCodeRef},
	{Type: EdgeOccurred, FromLabel: LabelChange, ToLabel: LabelProgress},
	{Type: EdgeSpecifies, FromLabel: LabelPiMemory, ToLabel: LabelRequirement},
}

func LiveTable() ([]VertexSet, []EdgeSet) {
	vertices := make([]VertexSet, 0, len(LiveLabels))
	for _, label := range LiveLabels {
		vertices = append(vertices, VertexSet{Label: label})
	}
	edges := make([]EdgeSet, 0, len(LiveEdgeSpecs))
	for _, spec := range LiveEdgeSpecs {
		edges = append(edges, EdgeSet{Type: spec.Type, FromLabel: spec.FromLabel, ToLabel: spec.ToLabel})
	}
	return vertices, edges
}

const ScopeProperty = "scope"

func Key(namespace, key string) int64 {
	if namespace == "" {
		return ids.Stable(key)
	}
	return ids.Stable(namespace + "|" + key)
}

func RepoID(name string) int64 {
	return RepoIDIn("", name)
}

func RepoIDIn(namespace, name string) int64 {
	return Key(namespace, "repo:"+name)
}

func ContextID(name string) int64 {
	return ContextIDIn("", name)
}

func ContextIDIn(namespace, name string) int64 {
	return Key(namespace, "context:"+name)
}

func RequirementID(context, requirement string) int64 {
	return RequirementIDIn("", context, requirement)
}

func RequirementIDIn(namespace, context, requirement string) int64 {
	return Key(namespace, "requirement:"+context+"/"+requirement)
}

func InterfaceID(context, name string) int64 {
	return InterfaceIDIn("", context, name)
}

func InterfaceIDIn(namespace, context, name string) int64 {
	return Key(namespace, "interface:"+context+"/"+name)
}

func CodeRefID(codegraphID string) int64 {
	return CodeRefIDIn("", codegraphID)
}

func CodeRefIDIn(namespace, codegraphID string) int64 {
	return Key(namespace, "coderef:"+codegraphID)
}

func FileCodeRefID(path string) int64 {
	return FileCodeRefIDIn("", path)
}

func FileCodeRefIDIn(namespace, path string) int64 {
	return CodeRefIDIn(namespace, "file:"+path)
}

func ChangeID(name string) int64 {
	return ChangeIDIn("", name)
}

func ChangeIDIn(namespace, name string) int64 {
	return Key(namespace, "change:"+name)
}

func ProgressID(change string, index int) int64 {
	return ProgressIDIn("", change, index)
}

func ProgressIDIn(namespace, change string, index int) int64 {
	return Key(namespace, fmt.Sprintf("progress:%s:%d", change, index))
}

func PiMemoryID(title string) int64 {
	return ids.Stable("pimemory:" + title)
}

func FileCodeRef(path string) Vertex {
	return FileCodeRefIn("", path)
}

func FileCodeRefIn(namespace, path string) Vertex {
	return Vertex{
		ID: FileCodeRefIDIn(namespace, path),
		Props: map[string]any{
			"codegraphId": "file:" + path,
			"kind":        "file",
			"name":        pathpkg.Base(path),
			"filePath":    path,
			ScopeProperty: namespace,
		},
	}
}

func ChangeVertex(change spec.SpecChange) Vertex {
	return ChangeVertexIn("", change)
}

func ChangeVertexIn(namespace string, change spec.SpecChange) Vertex {
	return Vertex{
		ID: ChangeIDIn(namespace, change.Name),
		Props: map[string]any{
			"name":        change.Name,
			"context":     change.Spec.SystemContext,
			"direction":   change.Spec.Direction,
			"phase":       change.Status.Phase,
			ScopeProperty: namespace,
		},
	}
}

func ProgressVertex(change string, index int, record spec.ProgressRecord) Vertex {
	return ProgressVertexIn("", change, index, record)
}

func ProgressVertexIn(namespace, change string, index int, record spec.ProgressRecord) Vertex {
	return Vertex{
		ID: ProgressIDIn(namespace, change, index),
		Props: map[string]any{
			"change":      change,
			"turn":        int64(record.Turn),
			"tool":        record.Tool,
			"note":        record.Note,
			"at":          record.At,
			ScopeProperty: namespace,
		},
	}
}

type Vertex struct {
	ID    int64
	Props map[string]any
}

func (v Vertex) Row() map[string]any {
	row := make(map[string]any, len(v.Props)+1)
	row["id"] = v.ID
	maps.Copy(row, v.Props)
	return row
}

type Edge struct {
	From int64
	To   int64
}

func (e Edge) Row() map[string]any {
	return map[string]any{"src": e.From, "dst": e.To}
}

type VertexSet struct {
	Label string
	Rows  []Vertex
}

type EdgeSet struct {
	Type      string
	FromLabel string
	ToLabel   string
	Rows      []Edge
}

type CodeRef struct {
	CodegraphID string
	Kind        string
	Name        string
	FilePath    string
	Line        int
}

func (c CodeRef) Display() string {
	if c.FilePath == "" {
		return c.Name
	}
	at := c.FilePath
	if c.Line > 0 {
		at = fmt.Sprintf("%s:%d", at, c.Line)
	}
	if c.Name == "" {
		return at
	}
	return c.Name + "@" + at
}

func CodeRefMap(contexts []spec.SystemContext) map[string]CodeRef {
	refs := map[string]CodeRef{}
	add := func(ref CodeRef) {
		if ref.CodegraphID == "" {
			return
		}
		if _, seen := refs[ref.CodegraphID]; seen {
			return
		}
		refs[ref.CodegraphID] = ref
	}
	for _, context := range contexts {
		for _, observed := range context.Status.Observed.Interfaces {
			add(CodeRef{
				CodegraphID: observed.CodegraphID,
				Kind:        observed.Kind,
				Name:        observed.Name,
				FilePath:    observed.File,
				Line:        observed.Line,
			})
		}
	}
	declared := make([]string, 0, len(contexts))
	for _, context := range contexts {
		declared = append(declared, context.Spec.CodeRefs...)
		for _, requirement := range context.Spec.Requirements {
			declared = append(declared, requirement.CodeRefs...)
		}
	}
	sort.Strings(declared)
	for _, reference := range declared {
		if _, seen := refs[reference]; seen {
			continue
		}
		if fileRef, ok := fileCodeRef(reference); ok {
			add(fileRef)
		}
	}
	return refs
}

func fileCodeRef(reference string) (CodeRef, bool) {
	filePath, ok := strings.CutPrefix(reference, "file:")
	if !ok || filePath == "" {
		return CodeRef{}, false
	}
	return CodeRef{
		CodegraphID: reference,
		Kind:        "file",
		Name:        pathpkg.Base(filePath),
		FilePath:    filePath,
	}, true
}

func CodeRefDisplay(refs map[string]CodeRef, reference string) (string, bool) {
	ref, ok := refs[reference]
	if !ok {
		return "", false
	}
	return ref.Display(), true
}

type Snapshot struct {
	Namespace string

	Repository spec.Repository
	Contexts   []spec.SystemContext
	CodeRefs   map[string]CodeRef
}

type Writer interface {
	WriteVertices(ctx context.Context, set VertexSet) error
	WriteEdges(ctx context.Context, set EdgeSet) error
	DeleteVertices(ctx context.Context, ids []int64) error
	SelectIDs(ctx context.Context, label string) ([]int64, error)
	SelectIDsWhere(ctx context.Context, label string, filter map[string]any) ([]int64, error)
	SelectOut(ctx context.Context, edgeType, fromLabel, toLabel string, fromID int64, properties []string) ([]map[string]any, error)
	SelectIn(ctx context.Context, edgeType, fromLabel, toLabel string, toID int64, properties []string) ([]map[string]any, error)
}

type EdgeSpec struct {
	Type      string
	FromLabel string
	ToLabel   string
}

var EdgeSpecs = []EdgeSpec{
	{Type: EdgeHasContext, FromLabel: LabelRepo, ToLabel: LabelContext},
	{Type: EdgeUpstream, FromLabel: LabelContext, ToLabel: LabelContext},
	{Type: EdgeOverlay, FromLabel: LabelContext, ToLabel: LabelContext},
	{Type: EdgeOrchestrator, FromLabel: LabelContext, ToLabel: LabelContext},
	{Type: EdgeRequires, FromLabel: LabelContext, ToLabel: LabelRequirement},
	{Type: EdgeDeclares, FromLabel: LabelContext, ToLabel: LabelInterface},
	{Type: EdgeReferences, FromLabel: LabelContext, ToLabel: LabelCodeRef},
	{Type: EdgeReferences, FromLabel: LabelRequirement, ToLabel: LabelCodeRef},
	{Type: EdgeDependsOn, FromLabel: LabelContext, ToLabel: LabelContext},
	{Type: EdgeIntroduces, FromLabel: LabelContext, ToLabel: LabelContext},
}

var LabelProperties = map[string][]string{
	LabelRepo:        {"name", "path", ScopeProperty},
	LabelContext:     {"name", "repo", "intent", "specHash", ScopeProperty},
	LabelRequirement: {"context", "reqId", "level", "text", ScopeProperty},
	LabelInterface:   {"context", "name", "kind", "signature", ScopeProperty},
	LabelCodeRef:     {"codegraphId", "kind", "name", "filePath", "line", "display", ScopeProperty},
	LabelChange:      {"name", "context", "direction", "phase", ScopeProperty},
	LabelProgress:    {"change", "turn", "tool", "note", "at", ScopeProperty},
	LabelPiMemory:    {"title", "body", "session"},
}

func Build(snapshot Snapshot) ([]VertexSet, []EdgeSet) {
	vertices := make([]VertexSet, 0, len(ManagedLabels))
	for _, label := range ManagedLabels {
		vertices = append(vertices, VertexSet{Label: label})
	}
	edges := make([]EdgeSet, 0, len(EdgeSpecs))
	for _, edgeSpec := range EdgeSpecs {
		edges = append(edges, EdgeSet{Type: edgeSpec.Type, FromLabel: edgeSpec.FromLabel, ToLabel: edgeSpec.ToLabel})
	}

	repoIndex, contextIndex := 0, 1
	requirementIndex, interfaceIndex, codeRefIndex := 2, 3, 4
	hasContextIndex, upstreamIndex, overlayIndex, orchestratorIndex := 0, 1, 2, 3
	requiresIndex, declaresIndex, contextRefsIndex, requirementRefsIndex := 4, 5, 6, 7
	dependsOnIndex, introducesIndex := 8, 9

	namespace := snapshot.Namespace
	repository := snapshot.Repository
	repoID := RepoIDIn(namespace, repository.Name)
	vertices[repoIndex].Rows = append(vertices[repoIndex].Rows, Vertex{
		ID: repoID,
		Props: map[string]any{
			"name":        repository.Name,
			"path":        repository.WorkPath(),
			ScopeProperty: namespace,
		},
	})

	if snapshot.CodeRefs == nil {
		snapshot.CodeRefs = map[string]CodeRef{}
	}
	seenCodeRefs := map[string]bool{}
	addCodeRef := func(reference string) (int64, bool) {
		ref, ok := snapshot.CodeRefs[reference]
		if !ok {
			return 0, false
		}
		id := CodeRefIDIn(namespace, ref.CodegraphID)
		if !seenCodeRefs[ref.CodegraphID] {
			seenCodeRefs[ref.CodegraphID] = true
			vertices[codeRefIndex].Rows = append(vertices[codeRefIndex].Rows, Vertex{
				ID: id,
				Props: map[string]any{
					"codegraphId": ref.CodegraphID,
					"kind":        ref.Kind,
					"name":        ref.Name,
					"filePath":    ref.FilePath,
					"line":        ref.Line,
					"display":     ref.Display(),
					ScopeProperty: namespace,
				},
			})
		}
		return id, true
	}

	for _, context := range snapshot.Contexts {
		contextID := ContextIDIn(namespace, context.Name)
		vertices[contextIndex].Rows = append(vertices[contextIndex].Rows, Vertex{
			ID: contextID,
			Props: map[string]any{
				"name":        context.Name,
				"repo":        repository.Name,
				"intent":      context.Spec.Intent,
				"specHash":    specHash(context),
				ScopeProperty: namespace,
			},
		})
		edges[hasContextIndex].Rows = append(edges[hasContextIndex].Rows, Edge{From: repoID, To: contextID})

		for _, requirement := range context.Spec.Requirements {
			requirementID := RequirementIDIn(namespace, context.Name, requirement.ID)
			vertices[requirementIndex].Rows = append(vertices[requirementIndex].Rows, Vertex{
				ID: requirementID,
				Props: map[string]any{
					"context":     context.Name,
					"reqId":       requirement.ID,
					"level":       string(requirement.Level),
					"text":        requirement.Text,
					ScopeProperty: namespace,
				},
			})
			edges[requiresIndex].Rows = append(edges[requiresIndex].Rows, Edge{From: contextID, To: requirementID})
			for _, codeRef := range requirement.CodeRefs {
				if target, ok := addCodeRef(codeRef); ok {
					edges[requirementRefsIndex].Rows = append(edges[requirementRefsIndex].Rows, Edge{From: requirementID, To: target})
				}
			}
		}

		for _, declared := range context.Spec.Interfaces {
			interfaceID := InterfaceIDIn(namespace, context.Name, declared.Name)
			vertices[interfaceIndex].Rows = append(vertices[interfaceIndex].Rows, Vertex{
				ID: interfaceID,
				Props: map[string]any{
					"context":     context.Name,
					"name":        declared.Name,
					"kind":        declared.Kind,
					"signature":   declared.Signature,
					ScopeProperty: namespace,
				},
			})
			edges[declaresIndex].Rows = append(edges[declaresIndex].Rows, Edge{From: contextID, To: interfaceID})
		}

		for _, codeRef := range context.Spec.CodeRefs {
			if target, ok := addCodeRef(codeRef); ok {
				edges[contextRefsIndex].Rows = append(edges[contextRefsIndex].Rows, Edge{From: contextID, To: target})
			}
		}

		arch := context.Spec.Arch != nil
		edges[upstreamIndex].Rows = appendRef(edges[upstreamIndex].Rows, contextID, context.Spec.Upstream, arch, namespace)
		for _, overlay := range context.Spec.Overlay {
			edges[overlayIndex].Rows = appendRef(edges[overlayIndex].Rows, contextID, overlay, arch, namespace)
		}
		edges[orchestratorIndex].Rows = appendRef(edges[orchestratorIndex].Rows, contextID, context.Spec.Orchestrator, arch, namespace)
		for _, dependency := range context.Spec.DependsOn {
			edges[dependsOnIndex].Rows = appendRef(edges[dependsOnIndex].Rows, contextID, dependency, arch, namespace)
		}
		for _, introduced := range context.Spec.Introduces {
			edges[introducesIndex].Rows = appendRef(edges[introducesIndex].Rows, contextID, introduced, arch, namespace)
		}
	}
	return vertices, edges
}

func appendRef(rows []Edge, from int64, ref string, arch bool, namespace string) []Edge {
	name, ok := RefTarget(ref, arch)
	if !ok {
		return rows
	}
	return append(rows, Edge{From: from, To: ContextIDIn(namespace, name)})
}

func RefTarget(ref string, arch bool) (string, bool) {
	if ref == spec.RefSelf {
		return "", false
	}
	if arch {
		if !spec.IsArchID(ref) {
			return "", false
		}
		return spec.ArchName(ref), true
	}
	return spec.RefName(ref)
}

func specHash(context spec.SystemContext) string {
	result := spec.ValidateSystemContext(&context)
	return result.SpecHash
}

func BuildAll(snapshots []Snapshot) ([]VertexSet, []EdgeSet) {
	vertices := make([]VertexSet, 0, len(ManagedLabels))
	for _, label := range ManagedLabels {
		vertices = append(vertices, VertexSet{Label: label})
	}
	edges := make([]EdgeSet, 0, len(EdgeSpecs))
	for _, edgeSpec := range EdgeSpecs {
		edges = append(edges, EdgeSet{Type: edgeSpec.Type, FromLabel: edgeSpec.FromLabel, ToLabel: edgeSpec.ToLabel})
	}
	index := map[string]int{}
	for position, set := range vertices {
		index[set.Label] = position
	}
	edgeIndex := map[string]int{}
	for position, set := range edges {
		edgeIndex[set.Type+":"+set.FromLabel+":"+set.ToLabel] = position
	}
	for _, snapshot := range snapshots {
		snapshotVertices, snapshotEdges := Build(snapshot)
		for _, set := range snapshotVertices {
			position := index[set.Label]
			vertices[position].Rows = append(vertices[position].Rows, set.Rows...)
		}
		for _, set := range snapshotEdges {
			position := edgeIndex[set.Type+":"+set.FromLabel+":"+set.ToLabel]
			edges[position].Rows = append(edges[position].Rows, set.Rows...)
		}
	}
	return vertices, edges
}

func Rebuild(ctx context.Context, writer Writer, namespace string, snapshots []Snapshot) error {
	for index := range snapshots {
		snapshots[index].Namespace = namespace
	}
	for _, label := range ManagedLabels {
		existing, err := rebuiltIDs(ctx, writer, label, namespace)
		if err != nil {
			return fmt.Errorf("graph: read %s ids: %w", label, err)
		}
		if err := writer.DeleteVertices(ctx, existing); err != nil {
			return fmt.Errorf("graph: delete %s: %w", label, err)
		}
	}
	vertices, edges := BuildAll(snapshots)
	for _, set := range vertices {
		if err := writer.WriteVertices(ctx, set); err != nil {
			return fmt.Errorf("graph: write %s: %w", set.Label, err)
		}
	}
	for _, set := range edges {
		if err := writer.WriteEdges(ctx, set); err != nil {
			return fmt.Errorf("graph: write %s edges: %w", set.Type, err)
		}
	}
	return nil
}

func rebuiltIDs(ctx context.Context, writer Writer, label, namespace string) ([]int64, error) {
	if namespace == "" {
		return writer.SelectIDs(ctx, label)
	}
	return writer.SelectIDsWhere(ctx, label, map[string]any{ScopeProperty: namespace})
}

func VertexUpsert(label string, properties []string) string {
	assignments := make([]string, 0, len(properties)+1)
	assignments = append(assignments, "n:"+label)
	for _, property := range properties {
		assignments = append(assignments, "n."+property+" = row."+property)
	}
	return "UNWIND $rows AS row MERGE (n {id: row.id}) SET " + strings.Join(assignments, ", ")
}

func EdgeCreate(edgeType, fromLabel, toLabel string) string {
	return "UNWIND $rows AS row MATCH (a:" + fromLabel + " {id: row.src}), (b:" + toLabel + " {id: row.dst}) CREATE (a)-[:" + edgeType + "]->(b)"
}

func VertexDelete() string {
	return "UNWIND $rows AS row MATCH (n {id: row.id}) DETACH DELETE n"
}

func VertexIDs(label string) string {
	return "MATCH (n:" + label + ") RETURN n.id AS id"
}

func VertexSelect(label string, properties []string, filter map[string]any) string {
	return "MATCH (n:" + label + filterClause(filter) + ") RETURN " + projection("n", properties)
}

func EdgeSelectOut(edgeType, fromLabel, toLabel string, fromID int64, properties []string) string {
	return "MATCH (a:" + fromLabel + " {id: " + ids.CypherLiteral(fromID) + "})-[:" + edgeType + "]->(b:" + toLabel + ") RETURN " + projection("b", properties)
}

func EdgeSelectIn(edgeType, fromLabel, toLabel string, toID int64, properties []string) string {
	return "MATCH (a:" + fromLabel + ")-[:" + edgeType + "]->(b:" + toLabel + " {id: " + ids.CypherLiteral(toID) + "}) RETURN " + projection("a", properties)
}

func filterClause(filter map[string]any) string {
	if len(filter) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filter))
	for _, key := range sortedKeys(filter) {
		parts = append(parts, key+": "+ids.CypherLiteral(filter[key]))
	}
	return " {" + strings.Join(parts, ", ") + "}"
}

func projection(binding string, properties []string) string {
	parts := make([]string, 0, len(properties))
	for _, property := range properties {
		parts = append(parts, binding+"."+property+" AS "+property)
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(filter map[string]any) []string {
	keys := make([]string, 0, len(filter))
	for key := range filter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
