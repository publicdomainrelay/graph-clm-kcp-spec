package contextpack

import (
	"context"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

// fakeReader answers the three graph reads the pack makes: the seed context,
// the one hop out of a context and the facts of a neighbour. The hop is keyed
// by the context the read starts from, the way one hop on a real graph is.
type fakeReader struct {
	context map[string]any

	out func(edgeType string, fromID int64) []map[string]any
}

func (f fakeReader) Select(ctx context.Context, label string, properties []string, filter map[string]any) ([]map[string]any, error) {
	if f.context == nil {
		return nil, nil
	}
	return []map[string]any{f.context}, nil
}

func (f fakeReader) SelectOut(ctx context.Context, edgeType, fromLabel, toLabel string, fromID int64, properties []string) ([]map[string]any, error) {
	if f.out == nil {
		return nil, nil
	}
	return f.out(edgeType, fromID), nil
}

func (f fakeReader) SelectIn(ctx context.Context, edgeType, fromLabel, toLabel string, toID int64, properties []string) ([]map[string]any, error) {
	return nil, nil
}

func mustRequirements(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for index := range count {
		rows = append(rows, map[string]any{
			"reqId": "r.fact-" + string(rune('a'+index)),
			"level": string(spec.LevelMust),
			"text":  "The requirement the neighbour states.",
		})
	}
	return rows
}

func declaredInterfaces(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for index := range count {
		rows = append(rows, map[string]any{
			"name":      "Declared" + string(rune('a'+index)),
			"kind":      "function",
			"signature": "func Declared() error",
		})
	}
	return rows
}

const (
	seedContext     = "cmd-calc"
	neighborContext = "calc"
)

func neighborFactsOf(requirements, interfaces int) func(string, int64) []map[string]any {
	return func(edgeType string, fromID int64) []map[string]any {
		if fromID != graph.ContextIDIn("", neighborContext) {
			return nil
		}
		switch edgeType {
		case graph.EdgeRequires:
			return mustRequirements(requirements)
		case graph.EdgeDeclares:
			return declaredInterfaces(interfaces)
		}
		return nil
	}
}

func TestTheNeighborCardsNameTheNeighbors(t *testing.T) {
	reader := fakeReader{
		context: map[string]any{"name": seedContext, "intent": "A command line front end."},
		out: func(edgeType string, fromID int64) []map[string]any {
			if fromID == graph.ContextIDIn("", seedContext) && edgeType == graph.EdgeDependsOn {
				return []map[string]any{{"name": neighborContext, "repo": "calc", "intent": "Arithmetic on two integers."}}
			}
			return neighborFactsOf(1, 1)(edgeType, fromID)
		},
	}
	pack, err := ForContext(context.Background(), seedContext, Options{Reader: reader})
	if err != nil {
		t.Fatalf("ForContext: %v", err)
	}
	if len(pack.Neighbors) != 1 {
		t.Fatalf("neighbors = %+v", pack.Neighbors)
	}
	card := pack.Neighbors[0]
	if !strings.Contains(card.Title, neighborContext) || !strings.Contains(card.Title, graph.EdgeDependsOn) {
		t.Errorf("the card title does not name the neighbor: %q", card.Title)
	}
	if !strings.Contains(card.Body, "Arithmetic on two integers") {
		t.Errorf("the card body carries no intent: %q", card.Body)
	}
}

func TestANeighborCardStopsAtTheFactLimit(t *testing.T) {
	sent := NeighborFactLimit + 4
	reader := fakeReader{
		context: map[string]any{"name": seedContext},
		out: func(edgeType string, fromID int64) []map[string]any {
			if fromID == graph.ContextIDIn("", seedContext) && edgeType == graph.EdgeDependsOn {
				return []map[string]any{{"name": neighborContext, "intent": "Arithmetic."}}
			}
			return neighborFactsOf(sent, sent)(edgeType, fromID)
		},
	}
	pack, err := ForContext(context.Background(), seedContext, Options{Reader: reader})
	if err != nil {
		t.Fatalf("ForContext: %v", err)
	}
	if len(pack.Neighbors) != 1 {
		t.Fatalf("neighbors = %+v", pack.Neighbors)
	}
	body := pack.Neighbors[0].Body
	if got := strings.Count(body, "MUST "); got != NeighborFactLimit {
		t.Errorf("the card states %d requirements, want %d of %d", got, NeighborFactLimit, sent)
	}
	if got := strings.Count(body, "interface "); got != NeighborFactLimit {
		t.Errorf("the card states %d interfaces, want %d of %d", got, NeighborFactLimit, sent)
	}
}

func TestTheNeighborListStopsAtTheLimit(t *testing.T) {
	rows := []map[string]any{}
	for index := range DefaultLimit + 4 {
		rows = append(rows, map[string]any{"name": "neighbor" + string(rune('a'+index))})
	}
	reader := fakeReader{
		context: map[string]any{"name": seedContext},
		out: func(edgeType string, fromID int64) []map[string]any {
			if fromID == graph.ContextIDIn("", seedContext) && edgeType == graph.EdgeDependsOn {
				return rows
			}
			return nil
		},
	}
	pack, err := ForContext(context.Background(), seedContext, Options{Reader: reader})
	if err != nil {
		t.Fatalf("ForContext: %v", err)
	}
	if len(pack.Neighbors) != DefaultLimit {
		t.Errorf("neighbors = %d, want the limit %d", len(pack.Neighbors), DefaultLimit)
	}
}
