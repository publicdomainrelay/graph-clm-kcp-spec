package clm

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
)

const memoryNamespace = "run-abc"

func TestMemoryQueriesCarryTheNamespace(t *testing.T) {
	namespaced := SpecifiesQuery(memoryNamespace, "file:calc/calc.go")
	want := ids.CypherLiteral(graph.CodeRefIDIn(memoryNamespace, "file:calc/calc.go"))
	if !strings.Contains(namespaced, want) {
		t.Fatalf("the SPECIFIES query matches an id without the namespace: %s", namespaced)
	}
	if bare := ids.CypherLiteral(graph.CodeRefID("file:calc/calc.go")); strings.Contains(namespaced, bare+" ") ||
		strings.HasSuffix(namespaced, bare) {
		t.Fatalf("the SPECIFIES query carries the unnamespaced id: %s", namespaced)
	}

	memoryID := MemoryID(memoryNamespace, "calc", "the removal guard")
	if memoryID == MemoryID("", "calc", "the removal guard") {
		t.Fatal("the memory id ignores the namespace")
	}
	references := ReferencesQuery(memoryNamespace, memoryID)
	if !strings.Contains(references, ids.CypherLiteral(memoryID)) {
		t.Fatalf("the REFERENCES query does not carry the namespaced memory id: %s", references)
	}
}

func TestANoteBecomesOneMemoryVertex(t *testing.T) {
	document := "# Context: calc\n\nintent\n\n<!-- SPECD_MANAGED_BEGIN -->\npack\n<!-- SPECD_MANAGED_END -->\n\n" +
		"## notes\n\n- the removal guard: it holds `file:calc/calc.go` and `r.add`\n  because two managers disagree\n"
	regions := RegionsOf(document)
	if !strings.Contains(regions.Pack, "pack") {
		t.Fatalf("the pack region is empty: %+v", regions)
	}
	notes := NotesIn(regions.ModelZone, "calc", 3)
	if len(notes) != 1 {
		t.Fatalf("notes = %+v", notes)
	}
	note := notes[0]
	if note.Title != "the removal guard" || note.Turn != 3 || note.Context != "calc" {
		t.Fatalf("note = %+v", note)
	}
	if !strings.Contains(note.Body, "two managers disagree") {
		t.Errorf("the continuation line is not part of the body: %q", note.Body)
	}

	codegraphIDs, units := NoteTargets(note)
	if len(codegraphIDs) != 1 || codegraphIDs[0] != "file:calc/calc.go" {
		t.Errorf("code units = %v", codegraphIDs)
	}
	if len(units) != 1 || units[0] != "r.add" {
		t.Errorf("claim units = %v", units)
	}

	vertices, edges := MemoryTable(memoryNamespace, note, codegraphIDs, units)
	vertex := vertices[0].Rows[0]
	if vertex.ID != graph.MemoryID(memoryNamespace, "calc", "the removal guard") {
		t.Fatalf("the memory vertex id = %d", vertex.ID)
	}
	if vertex.Props["title"] != "the removal guard" || vertex.Props["turn"] != int64(3) {
		t.Errorf("the memory vertex props = %v", vertex.Props)
	}
	if vertices[1].Label != graph.LabelAgentContext {
		t.Fatalf("the agent context vertex is missing: %+v", vertices)
	}
	if len(edges[1].Rows) != 1 || edges[1].Rows[0].To != graph.CodeRefIDIn(memoryNamespace, "file:calc/calc.go") {
		t.Errorf("the REFERENCES edge = %+v", edges[1].Rows)
	}
	if len(edges[2].Rows) != 1 || edges[2].Rows[0].To != graph.RequirementIDIn(memoryNamespace, "calc", "r.add") {
		t.Errorf("the SPECIFIES edge = %+v", edges[2].Rows)
	}
	if edges[0].Type != graph.EdgeRemembered {
		t.Errorf("the note is not REMEMBERED by its agent context: %+v", edges[0])
	}
}

func TestTheSameNoteWrittenTwiceIsOneID(t *testing.T) {
	first := MemoryID(memoryNamespace, "calc", "the removal guard")
	second := MemoryID(memoryNamespace, "calc", "the removal guard")
	if first != second {
		t.Fatalf("the same note built two ids: %d and %d", first, second)
	}
}

func TestTheModelZoneIsRenderedWithANotesRegion(t *testing.T) {
	document, err := RenderModelZoneWithProse("calc", "calc", "intent", sample())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(document, NotesHeading) {
		t.Fatalf("the model zone has no notes region:\n%s", document)
	}
	parsed, err := ParseModelZone(document)
	if err != nil {
		t.Fatalf("the rendered model zone does not parse: %v", err)
	}
	if parsed.Intent != "intent" {
		t.Errorf("the notes region leaked into the intent: %q", parsed.Intent)
	}
	if notes := NotesIn(document, "calc", 1); len(notes) != 0 {
		t.Errorf("an empty notes region holds a note: %+v", notes)
	}
}
