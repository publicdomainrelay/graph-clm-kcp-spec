package clm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
)

// Reader is the read side of the graph the memory queries need: the vertices of
// one label, filtered. The write side is graph.Writer, which the report path
// already uses.
type Reader interface {
	Select(ctx context.Context, label string, properties []string, filter map[string]any) ([]map[string]any, error)
}

// Skill is the context-management skill a host injects: the shared text of
// clm/core, or the repository's own override.
type Skill struct {
	Version string

	Text string

	Source string
}

// SkillFor reads the repository's override of the context-management skill, if
// it has one: .specs/context-skill.md, whose first line may declare
// "version: <v>". A repository without the file gets the shared skill of
// clm/core, and its version is what the AgentContext vertex records.
func SkillFor(repoPath string) Skill {
	shared := Skill{Version: clm.SkillVersion, Source: "core"}
	if strings.TrimSpace(repoPath) == "" {
		return shared
	}
	path := filepath.Join(repoPath, clm.SkillPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return shared
	}
	text := string(data)
	version := ""
	if first, rest, ok := strings.Cut(text, "\n"); ok {
		if declared, found := strings.CutPrefix(strings.TrimSpace(first), "version:"); found {
			version = strings.TrimSpace(declared)
			text = strings.TrimSpace(rest)
		}
	}
	if version == "" {
		version = fmt.Sprintf("repo-%x", ids.Stable(text))
	}
	return Skill{Version: version, Text: strings.TrimSpace(text), Source: path}
}

// Remember writes one memory note: its Memory vertex, the AgentContext that
// remembered it, a REFERENCES edge to every code unit and a SPECIFIES edge to
// every claim unit it names. The same note written twice is one vertex, so the
// second write updates it.
func Remember(ctx context.Context, options Options, contextName, title, body string, turn int, refs []string) (int, error) {
	if options.Writer == nil {
		return 0, fmt.Errorf("clm: remember needs a graph to write to")
	}
	if contextName == "" || title == "" {
		return 0, fmt.Errorf("clm: a note needs a context and a title")
	}
	note := clm.MemoryNote{
		Context:    contextName,
		Title:      title,
		Body:       body,
		Turn:       turn,
		RefTargets: refs,
	}
	codegraphIDs, units := clm.NoteTargets(note)
	vertices, edges := clm.MemoryTable(options.graphNamespace(), note, codegraphIDs, units)
	vertices[1].Rows[0].Props["skillVersion"] = SkillFor(options.RepoPath).Version
	for _, set := range vertices {
		if err := options.Writer.WriteVertices(ctx, set); err != nil {
			return 0, fmt.Errorf("clm: write %s: %w", set.Label, err)
		}
	}
	for _, set := range edges {
		if len(set.Rows) == 0 {
			continue
		}
		if err := options.Writer.WriteEdges(ctx, set); err != nil {
			return 0, fmt.Errorf("clm: write %s edges: %w", set.Type, err)
		}
	}
	return len(codegraphIDs) + len(units), nil
}

// writeNotes turns the free-form part of a model zone into graph memory: one
// Memory vertex per note, on every turn end. A note that says nothing new
// merges onto the vertex the previous turn wrote.
func writeNotes(ctx context.Context, options Options, contextName, document string) error {
	if options.Writer == nil {
		return nil
	}
	notes := clm.MemoryNotes(document, contextName, options.Turn)
	if len(notes) == 0 {
		return nil
	}
	skill := SkillFor(options.RepoPath)
	for _, note := range notes {
		codegraphIDs, units := clm.NoteTargets(note)
		vertices, edges := clm.MemoryTable(options.graphNamespace(), note, codegraphIDs, units)
		vertices[1].Rows[0].Props["skillVersion"] = skill.Version
		for _, set := range vertices {
			if err := options.Writer.WriteVertices(ctx, set); err != nil {
				return fmt.Errorf("clm: write %s: %w", set.Label, err)
			}
		}
		for _, set := range edges {
			if len(set.Rows) == 0 {
				continue
			}
			if err := options.Writer.WriteEdges(ctx, set); err != nil {
				return fmt.Errorf("clm: write %s edges: %w", set.Type, err)
			}
		}
	}
	return nil
}

// Memory is one note read back.
type Memory struct {
	Title string

	Body string

	Context string

	Turn int
}

// Recall reads the notes of the instance's namespace whose title or body holds
// every term, newest turn first.
func Recall(ctx context.Context, options Options, terms []string, limit int) ([]Memory, error) {
	if options.Reader == nil {
		return nil, fmt.Errorf("clm: recall needs a graph to read from")
	}
	filter := map[string]any{}
	if namespace := options.graphNamespace(); namespace != "" {
		filter[graph.ScopeProperty] = namespace
	}
	rows, err := options.Reader.Select(ctx, graph.LabelMemory,
		[]string{"title", "body", "context", "turn"}, filter)
	if err != nil {
		return nil, fmt.Errorf("clm: read %s: %w", graph.LabelMemory, err)
	}
	memories := []Memory{}
	for _, row := range rows {
		memory := Memory{
			Title:   text(row["title"]),
			Body:    text(row["body"]),
			Context: text(row["context"]),
			Turn:    number(row["turn"]),
		}
		if !matches(memory, terms) {
			continue
		}
		memories = append(memories, memory)
	}
	sort.Slice(memories, func(left, right int) bool {
		if memories[left].Turn != memories[right].Turn {
			return memories[left].Turn > memories[right].Turn
		}
		return memories[left].Title < memories[right].Title
	})
	if limit > 0 && len(memories) > limit {
		memories = memories[:limit]
	}
	return memories, nil
}

func matches(memory Memory, terms []string) bool {
	haystack := strings.ToLower(memory.Title + "\n" + memory.Body)
	for _, term := range terms {
		needle := strings.ToLower(strings.TrimSpace(term))
		if needle == "" {
			continue
		}
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

// Neighbor is one hop from a context: the edge, its direction, the label it
// reached and the key a reader names it by.
type Neighbor struct {
	Edge string

	Direction string

	Label string

	Key string
}

// Neighbors walks one hop from a context, its Memory vertices included: the
// agent context file of the context remembers every note written in it.
func Neighbors(ctx context.Context, options Options, contextName string) ([]Neighbor, error) {
	if options.Writer == nil {
		return nil, fmt.Errorf("clm: neighbors needs a graph to read from")
	}
	namespace := options.graphNamespace()
	contextID := graph.ContextIDIn(namespace, contextName)
	neighbors := []Neighbor{}
	for _, edgeSpec := range append(append([]graph.EdgeSpec{}, graph.EdgeSpecs...), graph.LiveEdgeSpecs...) {
		if edgeSpec.FromLabel == graph.LabelContext {
			rows, err := options.Writer.SelectOut(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, contextID,
				graph.LabelProperties[edgeSpec.ToLabel])
			if err != nil {
				return nil, fmt.Errorf("clm: read %s: %w", edgeSpec.Type, err)
			}
			for _, row := range rows {
				neighbors = append(neighbors, Neighbor{
					Edge:      edgeSpec.Type,
					Direction: "out",
					Label:     edgeSpec.ToLabel,
					Key:       displayKey(row, edgeSpec.ToLabel),
				})
			}
		}
	}
	rows, err := options.Writer.SelectIn(ctx, graph.EdgeRemembered, graph.LabelAgentContext, graph.LabelMemory,
		graph.AgentContextID(namespace, contextName), graph.LabelProperties[graph.LabelMemory])
	if err != nil {
		return nil, fmt.Errorf("clm: read %s: %w", graph.EdgeRemembered, err)
	}
	for _, row := range rows {
		neighbors = append(neighbors, Neighbor{
			Edge:      graph.EdgeRemembered,
			Direction: "out",
			Label:     graph.LabelMemory,
			Key:       displayKey(row, graph.LabelMemory),
		})
	}
	sort.Slice(neighbors, func(left, right int) bool {
		if neighbors[left].Edge != neighbors[right].Edge {
			return neighbors[left].Edge < neighbors[right].Edge
		}
		if neighbors[left].Direction != neighbors[right].Direction {
			return neighbors[left].Direction < neighbors[right].Direction
		}
		return neighbors[left].Key < neighbors[right].Key
	})
	return neighbors, nil
}

func keyProperty(label string) string {
	switch label {
	case graph.LabelCodeRef:
		return "codegraphId"
	case graph.LabelRequirement:
		return "reqId"
	case graph.LabelInterface, graph.LabelContext, graph.LabelRepo, graph.LabelChange, graph.LabelAgentContext:
		return "name"
	case graph.LabelProgress:
		return "note"
	case graph.LabelPiMemory, graph.LabelMemory:
		return "title"
	}
	return ""
}

func displayKey(row map[string]any, label string) string {
	if property := keyProperty(label); property != "" {
		if value := text(row[property]); value != "" {
			return value
		}
	}
	parts := []string{}
	for _, property := range graph.LabelProperties[label] {
		if value := text(row[property]); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

func text(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func number(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	}
	return 0
}
