package contextpack

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
)

func pack() Pack {
	return Pack{
		Seed:       Seed{Kind: SeedContext, Value: "impl-bundle"},
		Repository: "repository: hydradb-0013\npath: .",
		Task: Task{
			Seed:       Seed{Kind: SeedContext, Value: "impl-bundle"},
			Verify:     []string{"go build ./...", "go test ./impl/bundle"},
			Acceptance: []string{"go test ./test/e2e"},
		},
		Owner: Card{Title: "impl-bundle", Body: "intent: it summarizes a piece of a code base"},
		Neighbors: []Card{
			{Title: "DEPENDS_ON impl-summarize", Body: "intent: it writes the context document"},
		},
		Claims: []Card{
			{Title: "finding open", Body: "the code suggests: intent is empty"},
		},
		ModelZone: "# notes\n\nmy working memory",
		Budget:    8000,
	}
}

func position(t *testing.T, text, title string) int {
	t.Helper()
	index := strings.Index(text, "## "+title)
	if index < 0 {
		t.Fatalf("the pack has no section %q:\n%s", title, text)
	}
	return index
}

func TestTheSectionOrderIsFixed(t *testing.T) {
	text, dropped := Render(pack(), 8000)
	if len(dropped) != 0 {
		t.Fatalf("dropped = %v, want none", dropped)
	}
	order := []string{RepositoryTitle, TaskTitle, OwnerTitle, NeighborsTitle, BlackboardTitle, ScoreboardTitle, ClaimsTitle, BudgetTitle, ModelZoneTitle}
	previous := -1
	for _, title := range order {
		at := position(t, text, title)
		if at < previous {
			t.Fatalf("%q is out of order:\n%s", title, text)
		}
		previous = at
	}
	if !strings.Contains(text, "go build ./...") || !strings.Contains(text, "go test ./test/e2e") {
		t.Errorf("the task card lost the verify or the acceptance commands:\n%s", text)
	}
}

func TestTheBudgetDropsWholeSectionsAndListsThem(t *testing.T) {
	text, dropped := Render(pack(), 60)
	if len(dropped) == 0 {
		t.Fatalf("nothing was dropped under a small budget:\n%s", text)
	}
	for _, title := range dropped {
		if strings.Contains(text, "## "+title) {
			t.Errorf("%q was dropped and is still rendered:\n%s", title, text)
		}
	}
	bodies := map[string]string{
		OwnerTitle:     "intent: it summarizes a piece of a code base",
		NeighborsTitle: "intent: it writes the context document",
		ClaimsTitle:    "the code suggests: intent is empty",
	}
	for _, title := range dropped {
		if body, ok := bodies[title]; ok && strings.Contains(text, body) {
			t.Errorf("%q was dropped and its body is still rendered:\n%s", title, text)
		}
	}
	joined := strings.Join(dropped, ", ")
	if !strings.Contains(text, joined) {
		t.Errorf("the dropped titles are not listed (%v):\n%s", dropped, text)
	}
	if !strings.Contains(text, "## "+ModelZoneTitle) {
		t.Errorf("the model zone is not last and kept:\n%s", text)
	}
}

func TestTheTokenBudgetLine(t *testing.T) {
	text, _ := Render(pack(), 8000)
	if !strings.Contains(text, "of 8.0k tokens") {
		t.Errorf("the pack has no token-budget line:\n%s", text)
	}
	if position(t, text, BudgetTitle) > position(t, text, ModelZoneTitle) {
		t.Errorf("the budget line is after the model zone:\n%s", text)
	}
}

func TestThePackCarriesNeighborCards(t *testing.T) {
	pack := Pack{
		Seed:       Seed{Kind: SeedContext, Value: "impl-bundle"},
		Repository: "repository: hydradb-0013",
		Task:       Task{Seed: Seed{Kind: SeedContext, Value: "impl-bundle"}},
		Owner:      Card{Title: "impl-bundle", Body: "intent: it summarizes a piece of a code base"},
		Neighbors: NeighborCards([]Neighbor{
			{
				Name:         "impl-summarize",
				Edge:         graph.EdgeDependsOn,
				Direction:    "out",
				Label:        graph.LabelContext,
				Intent:       "it writes the context document\nand nothing else",
				Requirements: []string{"r.context-doc: it writes a document per context"},
				Interfaces:   []string{"WriteContextDoc (function) func WriteContextDoc(docDir, repository, context string) (string, error)"},
			},
			{
				Name:      "impl-ingest",
				Edge:      graph.EdgeDependsOn,
				Direction: "in",
				Label:     graph.LabelContext,
			},
		}),
		Claims: []Card{{Title: "finding open", Body: "the code suggests: intent is empty"}},
	}
	text, dropped := Render(pack, 8000)
	if len(dropped) != 0 {
		t.Fatalf("dropped = %v, want none", dropped)
	}
	neighbors := position(t, text, NeighborsTitle)
	if neighbors < position(t, text, OwnerTitle) || neighbors > position(t, text, ClaimsTitle) {
		t.Fatalf("%q is not between the owner card and the claims:\n%s", NeighborsTitle, text)
	}
	for _, want := range []string{
		"out " + graph.EdgeDependsOn + " " + graph.LabelContext + " impl-summarize",
		"intent: it writes the context document\n",
		"MUST r.context-doc: it writes a document per context",
		"interface WriteContextDoc (function) func WriteContextDoc(docDir, repository, context string) (string, error)",
		"in " + graph.EdgeDependsOn + " " + graph.LabelContext + " impl-ingest",
		"intent: " + NoIntentNote,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the neighbor cards lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "and nothing else") {
		t.Errorf("a neighbor card carried more than the first line of the intent:\n%s", text)
	}
}

func TestThePackCarriesTheBlackboardAndTheScoreboard(t *testing.T) {
	pack := pack()
	pack.Blackboard = []BlackboardEntry{
		{Kind: "change", Name: "calc-s2c-bbbb", Context: "calc", Phase: "Running", At: "2026-10-01T00:00:00Z"},
		{Kind: "memory", Name: "a note", Context: "calc", Text: "the seed\nis parsed", At: "2026-09-30T00:00:00Z"},
		{Kind: "change", Name: "abc-contextpack-s2c-cccc", Context: "abc-contextpack", Phase: "Running", Own: true, At: "2026-10-02T00:00:00Z"},
	}
	pack.Scoreboard = []ScoreboardItem{
		{Plan: "0013", Item: "E", Context: "abc-contextpack", Phase: "InProgress", At: "2026-10-02T00:00:00Z"},
		{Plan: "0013", Item: "F", Context: "impl-bundle", Phase: "Done", At: "2026-10-03T00:00:00Z"},
	}
	text, dropped := Render(pack, 8000)
	if len(dropped) != 0 {
		t.Fatalf("dropped = %v, want none", dropped)
	}

	neighbors := position(t, text, NeighborsTitle)
	blackboard := position(t, text, BlackboardTitle)
	scoreboard := position(t, text, ScoreboardTitle)
	claims := position(t, text, ClaimsTitle)
	if blackboard < neighbors || blackboard > claims || scoreboard < blackboard || scoreboard > claims {
		t.Fatalf("the blackboard and the scoreboard are not between the neighbors and the claims:\n%s", text)
	}

	for _, want := range []string{
		"change abc-contextpack-s2c-cccc " + OwnMark + " abc-contextpack Running",
		"change calc-s2c-bbbb calc Running",
		"memory a note calc the seed",
		"0013 F impl-bundle Done",
		"0013 E abc-contextpack InProgress",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the swarm sections lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "the seed\nis parsed") {
		t.Errorf("a blackboard entry carried more than the first line of its text:\n%s", text)
	}
	if strings.Count(text, OwnMark) != 1 {
		t.Errorf("exactly one entry is the agent run's own:\n%s", text)
	}
	if strings.Index(text, "change abc-contextpack-s2c-cccc") > strings.Index(text, "change calc-s2c-bbbb") {
		t.Errorf("the blackboard is not ordered by recency, newest first:\n%s", text)
	}
	if strings.Index(text, "0013 F impl-bundle") > strings.Index(text, "0013 E abc-contextpack") {
		t.Errorf("the scoreboard is not ordered by recency, newest first:\n%s", text)
	}
}

func TestAnEmptyBlackboardRendersItsNote(t *testing.T) {
	empty, _ := Render(pack(), 8000)
	if !strings.Contains(empty, EmptyBlackboardNote) {
		t.Errorf("an empty blackboard rendered no note:\n%s", empty)
	}
	if !strings.Contains(empty, EmptyScoreboardNote) {
		t.Errorf("an empty scoreboard rendered no note:\n%s", empty)
	}
	if body := renderBlackboard([]BlackboardEntry{{Own: true}}); !strings.Contains(body, EmptyBlackboardNote) {
		t.Errorf("a blackboard of blank entries rendered no note: %q", body)
	}
	if body := renderScoreboard(nil); !strings.Contains(body, EmptyScoreboardNote) {
		t.Errorf("an empty scoreboard body = %q, want the note", body)
	}
}

func TestRankWeighsTheHitTheOwnerAndTheDepth(t *testing.T) {
	plan := DefaultPlan()
	ranked := Rank([]Candidate{
		{Name: "deep", Edge: "DEPENDS_ON", Depth: 3},
		{Name: "owner", Owner: true},
		{Name: "hit", Hit: 2},
	}, plan)
	if ranked[0].Name != "hit" || ranked[1].Name != "owner" || ranked[2].Name != "deep" {
		t.Errorf("ranked = %+v", ranked)
	}
}

func TestParseSeed(t *testing.T) {
	if seed := ParseSeed("file:impl/bundle/bundle.go"); seed.Kind != SeedFile || seed.Value != "impl/bundle/bundle.go" {
		t.Errorf("seed = %+v", seed)
	}
	if seed := ParseSeed("neighbor cards"); seed.Kind != SeedQuery {
		t.Errorf("seed = %+v", seed)
	}
}
