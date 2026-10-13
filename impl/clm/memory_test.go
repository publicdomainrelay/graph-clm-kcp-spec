package clm

import (
	"context"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
)

const memoryNamespace = "run-impl"

// The free-form part of a model zone becomes graph memory on turn end: one
// Memory vertex per note, linked by REFERENCES to the code it names and by
// SPECIFIES to the claim units, and REMEMBERED by the context's agent context.
func TestANoteInTheModelZoneBecomesAMemoryVertex(t *testing.T) {
	cluster := fixture(t)
	rendered, err := Render(context.Background(), Options{Cluster: cluster, Context: "calc"})
	if err != nil {
		t.Fatal(err)
	}
	zone := rendered.ModelZone + "\n- the removal guard: it holds `file:calc/calc.go` and `r.add`\n"
	writer := &fakeWriter{}
	_, err = Apply(context.Background(), Options{
		Cluster:        cluster,
		Context:        "calc",
		Writer:         writer,
		GraphNamespace: memoryNamespace,
		RepoPath:       t.TempDir(),
		Turn:           2,
	}, zone)
	if err != nil {
		t.Fatal(err)
	}

	memories := writer.labels[graph.LabelMemory]
	if len(memories) != 1 {
		t.Fatalf("memory vertices = %+v", memories)
	}
	memory := memories[0]
	if memory.ID != graph.MemoryID(memoryNamespace, "calc", "the removal guard") {
		t.Fatalf("the memory id = %d", memory.ID)
	}
	if memory.Props["body"] != "it holds `file:calc/calc.go` and `r.add`" || memory.Props["turn"] != int64(2) {
		t.Errorf("the memory vertex = %v", memory.Props)
	}
	if memory.Props[graph.ScopeProperty] != memoryNamespace {
		t.Errorf("the memory vertex is not in the instance namespace: %v", memory.Props)
	}

	agentContexts := writer.labels[graph.LabelAgentContext]
	if len(agentContexts) != 1 || agentContexts[0].ID != graph.AgentContextID(memoryNamespace, "calc") {
		t.Fatalf("agent context vertices = %+v", agentContexts)
	}
	if agentContexts[0].Props["skillVersion"] != clm.SkillVersion {
		t.Errorf("the agent context records no skill version: %v", agentContexts[0].Props)
	}

	remembered := writer.edges[graph.EdgeRemembered]
	if len(remembered) != 1 || remembered[0].From != graph.AgentContextID(memoryNamespace, "calc") || remembered[0].To != memory.ID {
		t.Fatalf("the REMEMBERED edge = %+v", remembered)
	}
	references := writer.edges[graph.EdgeReferences]
	if len(references) != 1 || references[0].To != graph.CodeRefIDIn(memoryNamespace, "file:calc/calc.go") {
		t.Fatalf("the REFERENCES edge = %+v", references)
	}
	specifies := writer.edges[graph.EdgeSpecifies]
	if len(specifies) != 1 || specifies[0].To != graph.RequirementIDIn(memoryNamespace, "calc", "r.add") {
		t.Fatalf("the SPECIFIES edge = %+v", specifies)
	}
}

// The same note written twice is one vertex, so the second write updates it
// rather than adding a second row.
func TestTheSameNoteWrittenTwiceUpdates(t *testing.T) {
	cluster := fixture(t)
	note := "- the removal guard: it holds `file:calc/calc.go`\n"
	writer := &fakeWriter{}
	options := Options{
		Cluster:        cluster,
		Context:        "calc",
		Writer:         writer,
		GraphNamespace: memoryNamespace,
		RepoPath:       t.TempDir(),
		Turn:           1,
	}
	for _, turn := range []int{1, 2} {
		rendered, err := Render(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
		options.Turn = turn
		if _, err := Apply(context.Background(), options, rendered.ModelZone+"\n"+note); err != nil {
			t.Fatal(err)
		}
	}
	memories := writer.labels[graph.LabelMemory]
	if len(memories) != 2 {
		t.Fatalf("memory writes = %+v", memories)
	}
	if memories[0].ID != memories[1].ID {
		t.Fatalf("the same note built two ids: %d and %d", memories[0].ID, memories[1].ID)
	}
	if memories[1].Props["turn"] != int64(2) {
		t.Errorf("the second write did not update the note: %v", memories[1].Props)
	}
}
