package clm

import (
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
)

const (
	NotesHeading = "## notes"

	NotesHint = "_One note per line: `- title: body`. A note becomes a Memory vertex, " +
		"linked to the code and the units it names in backticks._"

	// SkillVersion is the version of the context-management skill clm/core
	// holds. A repository that overrides the skill records its own version in
	// the AgentContext vertex instead.
	SkillVersion = "1"

	// SkillPath is the repository file that overrides the shared
	// context-management skill.
	SkillPath = ".specs/context-skill.md"
)

// MemoryNote is one free-form note of a model zone: what the model wrote about
// the context, apart from the spec it declares. RefTargets are the code and
// claim units the note names, as they are written.
type MemoryNote struct {
	Context string

	Title string

	Body string

	Turn int

	RefTargets []string
}

// Regions is a context document read apart into the two regions of the format:
// the pack the tool owns and regenerates, read-only for the model, and the
// model zone the model writes.
type Regions struct {
	Pack string

	ModelZone string
}

func RegionsOf(document string) Regions {
	modelZone, pack := SplitDocument(document)
	return Regions{Pack: pack, ModelZone: modelZone}
}

// MemoryID is the vertex one note becomes. It goes through graph.Key with the
// graph namespace, so a query of a namespaced graph finds the note.
func MemoryID(namespace, context, title string) int64 {
	return graph.MemoryID(namespace, context, title)
}

// AgentContextID names the agent context file of a context.
func AgentContextID(namespace, context string) int64 {
	return graph.AgentContextID(namespace, context)
}

// CodeRefID names the code reference a note names, in the graph namespace. The
// pi host resolved this id without a namespace, so its SPECIFIES match found no
// row in a namespaced graph.
func CodeRefID(namespace, codegraphID string) int64 {
	return graph.CodeRefIDIn(namespace, codegraphID)
}

// RequirementID names a requirement of a context, in the graph namespace.
func RequirementID(namespace, context, requirement string) int64 {
	return graph.RequirementIDIn(namespace, context, requirement)
}

// MemoryVertex is the vertex of one note: its title, its body, the context it
// was written in and the turn it came from.
func MemoryVertex(namespace string, note MemoryNote) graph.Vertex {
	return graph.Vertex{
		ID: MemoryID(namespace, note.Context, note.Title),
		Props: map[string]any{
			"title":             note.Title,
			"body":              note.Body,
			"context":           note.Context,
			"turn":              int64(note.Turn),
			"agentContext":      note.Context,
			graph.ScopeProperty: namespace,
		},
	}
}

// AgentContextVertex is the one agent context file of a context. It carries the
// version of the context-management skill the host injected, so a reader can
// tell the shared skill from a repository override.
func AgentContextVertex(namespace, context, skillVersion string) graph.Vertex {
	return graph.Vertex{
		ID: AgentContextID(namespace, context),
		Props: map[string]any{
			"name":              context,
			"context":           context,
			"skillVersion":      skillVersion,
			graph.ScopeProperty: namespace,
		},
	}
}

// SpecifiesQuery asks which requirements answer to a code reference. The id it
// matches is built with the graph namespace, so the match finds the row a
// namespaced rebuild wrote.
func SpecifiesQuery(namespace, codegraphID string) string {
	return "MATCH (r:" + graph.LabelRequirement + ")-[:" + graph.EdgeReferences + "]->(c:" + graph.LabelCodeRef +
		" {id: " + ids.CypherLiteral(CodeRefID(namespace, codegraphID)) + "}) RETURN r.id AS id"
}

// ReferencesQuery is the one hop from a memory note to the code it names. The
// note id is a namespaced id, built by MemoryID.
func ReferencesQuery(namespace string, memoryID int64) string {
	return graph.EdgeSelectOut(graph.EdgeReferences, graph.LabelMemory, graph.LabelCodeRef, memoryID,
		[]string{"codegraphId", "kind", "name", "filePath"})
}

// MemoryTable is the rows one note becomes: the Memory vertex, the
// AgentContext that remembered it, a REFERENCES edge to every code unit and a
// SPECIFIES edge to every claim unit the note names.
func MemoryTable(namespace string, note MemoryNote, codegraphIDs, units []string) ([]graph.VertexSet, []graph.EdgeSet) {
	memoryID := MemoryID(namespace, note.Context, note.Title)
	agentContextID := AgentContextID(namespace, note.Context)
	vertices := []graph.VertexSet{
		{Label: graph.LabelMemory, Rows: []graph.Vertex{MemoryVertex(namespace, note)}},
		{
			Label: graph.LabelAgentContext,
			Rows:  []graph.Vertex{AgentContextVertex(namespace, note.Context, SkillVersion)},
		},
	}
	edges := []graph.EdgeSet{
		{Type: graph.EdgeRemembered, FromLabel: graph.LabelAgentContext, ToLabel: graph.LabelMemory,
			Rows: []graph.Edge{{From: agentContextID, To: memoryID}}},
		{Type: graph.EdgeReferences, FromLabel: graph.LabelMemory, ToLabel: graph.LabelCodeRef},
		{Type: graph.EdgeSpecifies, FromLabel: graph.LabelMemory, ToLabel: graph.LabelRequirement},
	}
	// A note names code and claim units, and a graph that refuses an edge whose
	// endpoint is missing (HydraDB) needs those endpoints to exist: the vertex
	// is written with the edge, and merges onto the one an ingest wrote.
	codeRefs := graph.VertexSet{Label: graph.LabelCodeRef}
	for _, codegraphID := range unique(codegraphIDs) {
		codeRefs.Rows = append(codeRefs.Rows, graph.Vertex{
			ID:    CodeRefID(namespace, codegraphID),
			Props: map[string]any{"codegraphId": codegraphID, graph.ScopeProperty: namespace},
		})
		edges[1].Rows = append(edges[1].Rows, graph.Edge{From: memoryID, To: CodeRefID(namespace, codegraphID)})
	}
	if len(codeRefs.Rows) > 0 {
		vertices = append(vertices, codeRefs)
	}
	requirements := graph.VertexSet{Label: graph.LabelRequirement}
	for _, unit := range unique(units) {
		requirements.Rows = append(requirements.Rows, graph.Vertex{
			ID:    RequirementID(namespace, note.Context, unit),
			Props: map[string]any{"context": note.Context, "reqId": unit, graph.ScopeProperty: namespace},
		})
		edges[2].Rows = append(edges[2].Rows, graph.Edge{From: memoryID, To: RequirementID(namespace, note.Context, unit)})
	}
	if len(requirements.Rows) > 0 {
		vertices = append(vertices, requirements)
	}
	return vertices, edges
}

// MemoryNotes reads the free-form region of a model zone: every list item under
// the notes heading, "- title: body", with the indented lines under it as the
// body. The region after the spec block belongs to the model, so a note is
// never mistaken for a spec edit.
func MemoryNotes(document string, context string, turn int) []MemoryNote {
	regions := RegionsOf(document)
	return NotesIn(regions.ModelZone, context, turn)
}

// NotesIn reads the notes of a model zone that is already split from the pack.
func NotesIn(modelZone, context string, turn int) []MemoryNote {
	lines := strings.Split(modelZone, "\n")
	start := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == NotesHeading {
			start = index + 1
			break
		}
	}
	if start == -1 {
		return nil
	}
	notes := []MemoryNote{}
	current := -1
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- "):
			title, body, _ := strings.Cut(strings.TrimSpace(trimmed[2:]), ":")
			notes = append(notes, MemoryNote{
				Context: context,
				Title:   strings.TrimSpace(title),
				Body:    strings.TrimSpace(body),
				Turn:    turn,
			})
			current = len(notes) - 1
		case trimmed == "":
			current = -1
		case current >= 0 && isIndented(line):
			joined := strings.TrimSpace(notes[current].Body + " " + trimmed)
			notes[current].Body = joined
		default:
			current = -1
		}
	}
	kept := notes[:0]
	for _, note := range notes {
		if note.Title == "" {
			continue
		}
		kept = append(kept, note)
	}
	return kept
}

func isIndented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

// NoteTargets reads the backticked targets a note names: a codegraph id or a
// repository path is a code unit, anything else is a claim unit.
func NoteTargets(note MemoryNote) (codegraphIDs, units []string) {
	targets := append(append([]string{}, note.RefTargets...), backticked(note.Title+"\n"+note.Body)...)
	for _, target := range targets {
		if kind, _, ok := strings.Cut(target, ":"); ok && isRefKind(kind) {
			codegraphIDs = append(codegraphIDs, target)
			continue
		}
		if strings.Contains(target, "/") {
			codegraphIDs = append(codegraphIDs, "file:"+target)
			continue
		}
		units = append(units, target)
	}
	return unique(codegraphIDs), unique(units)
}

func backticked(text string) []string {
	found := []string{}
	for rest := text; ; {
		open := strings.Index(rest, "`")
		if open == -1 {
			return found
		}
		rest = rest[open+1:]
		closeAt := strings.Index(rest, "`")
		if closeAt == -1 {
			return found
		}
		candidate := strings.TrimSpace(rest[:closeAt])
		if candidate != "" {
			found = append(found, candidate)
		}
		rest = rest[closeAt+1:]
	}
}

func isRefKind(kind string) bool {
	switch kind {
	case "file", "function", "method", "class", "struct", "interface", "constant", "type_alias", "variable", "import":
		return true
	}
	return false
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
