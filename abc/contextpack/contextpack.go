package contextpack

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
)

const DefaultBudget = 8000

const DepthDecay = 0.5

const (
	HitWeight = 1.0

	OwnerWeight = 0.5
)

type SeedKind string

const (
	SeedContext SeedKind = "context"

	SeedFile SeedKind = "file"

	SeedQuery SeedKind = "query"
)

type Seed struct {
	Kind SeedKind

	Value string
}

func ParseSeed(value string) Seed {
	if kind, rest, ok := strings.Cut(value, ":"); ok {
		switch SeedKind(kind) {
		case SeedContext:
			return Seed{Kind: SeedContext, Value: rest}
		case SeedFile:
			return Seed{Kind: SeedFile, Value: rest}
		case SeedQuery:
			return Seed{Kind: SeedQuery, Value: rest}
		}
	}
	return Seed{Kind: SeedQuery, Value: value}
}

type Hop struct {
	Edge string

	From string

	To string

	Weight float64

	Depth int
}

type Plan struct {
	Hops []Hop
}

func DefaultPlan() Plan {
	return Plan{Hops: []Hop{
		{Edge: graph.EdgeDependsOn, From: graph.LabelContext, To: graph.LabelContext, Weight: 1.0, Depth: 1},
		{Edge: graph.EdgeUpstream, From: graph.LabelContext, To: graph.LabelContext, Weight: 0.8, Depth: 1},
		{Edge: graph.EdgeOverlay, From: graph.LabelContext, To: graph.LabelContext, Weight: 0.7, Depth: 1},
		{Edge: graph.EdgeOrchestrator, From: graph.LabelContext, To: graph.LabelContext, Weight: 0.6, Depth: 1},
		{Edge: graph.EdgeTestImports, From: graph.LabelContext, To: graph.LabelContext, Weight: 0.3, Depth: 1},
	}}
}

func (p Plan) Weight(edge string) float64 {
	for _, hop := range p.Hops {
		if hop.Edge == edge {
			return hop.Weight
		}
	}
	return 0
}

func (p Plan) Depth() int {
	depth := 0
	for _, hop := range p.Hops {
		if hop.Depth > depth {
			depth = hop.Depth
		}
	}
	return depth
}

type Candidate struct {
	ID int64

	Label string

	Name string

	Context string

	Edge string

	Depth int

	Hit float64

	Owner bool
}

func Weight(candidate Candidate, plan Plan) float64 {
	weight := candidate.Hit * HitWeight
	if candidate.Owner {
		weight += OwnerWeight
	}
	edge := plan.Weight(candidate.Edge)
	if edge == 0 && candidate.Edge == "" {
		edge = 1
	}
	weight += edge * math.Pow(DepthDecay, float64(candidate.Depth))
	return weight
}

func Rank(candidates []Candidate, plan Plan) []Candidate {
	ordered := append([]Candidate{}, candidates...)
	sort.SliceStable(ordered, func(left, right int) bool {
		leftWeight := Weight(ordered[left], plan)
		rightWeight := Weight(ordered[right], plan)
		if leftWeight != rightWeight {
			return leftWeight > rightWeight
		}
		if ordered[left].Context != ordered[right].Context {
			return ordered[left].Context < ordered[right].Context
		}
		return ordered[left].Name < ordered[right].Name
	})
	return ordered
}

type Card struct {
	Title string

	Body string
}

type Neighbor struct {
	Name string

	Edge string

	Direction string

	Label string

	Intent string

	Requirements []string

	Interfaces []string
}

const NoIntentNote = "no intent yet"

// NoOwnerNote is what a pack seeded by a file says when no context observes
// the file: a file outside every context is not an error, it is a plain fact
// the task card states.
const NoOwnerNote = "no context owns this file"

func NeighborCard(neighbor Neighbor) Card {
	builder := strings.Builder{}
	intent := firstLine(neighbor.Intent)
	switch {
	case intent != "":
		fmt.Fprintf(&builder, "intent: %s\n", intent)
	case neighbor.Label == graph.LabelContext:
		fmt.Fprintf(&builder, "intent: %s\n", NoIntentNote)
	}
	for _, requirement := range neighbor.Requirements {
		fmt.Fprintf(&builder, "MUST %s\n", requirement)
	}
	for _, declared := range neighbor.Interfaces {
		fmt.Fprintf(&builder, "interface %s\n", declared)
	}
	title := strings.TrimSpace(strings.Join([]string{neighbor.Direction, neighbor.Edge, neighbor.Label, neighbor.Name}, " "))
	return Card{Title: title, Body: builder.String()}
}

func NeighborCards(neighbors []Neighbor) []Card {
	cards := make([]Card, 0, len(neighbors))
	for _, neighbor := range neighbors {
		cards = append(cards, NeighborCard(neighbor))
	}
	return cards
}

func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.Index(trimmed, "\n"); index >= 0 {
		trimmed = trimmed[:index]
	}
	return trimmed
}

// BlackboardEntry is one thing another agent run already did, decided or
// proposed for a context of this pack: a change it is Running, with the context
// and the phase, a memory note another agent context wrote, or an open proposal
// or finding. At is the recency key the section orders by, newest first. Own
// marks the entry the agent run reading the pack wrote itself, so the model is
// not told its own work as news.
type BlackboardEntry struct {
	Kind string

	Name string

	Context string

	Phase string

	Text string

	Own bool

	At string
}

// ScoreboardItem is one plan item that touches a context of this pack, with the
// phase it reached. At is the recency key the section orders by, newest first.
type ScoreboardItem struct {
	Plan string

	Item string

	Context string

	Phase string

	Summary string

	At string
}

// OwnMark labels the entry an agent run wrote itself: the run that reads the
// pack sees its own change stated as its own, not as another run's news.
const OwnMark = "(own)"

// EmptyBlackboardNote and EmptyScoreboardNote are what the two swarm sections
// say when they hold nothing: an empty section still renders, because "the
// swarm has done nothing here" is a fact the model has to read, not a gap it
// cannot tell from a pack that was trimmed.
const (
	EmptyBlackboardNote = "nothing on the blackboard yet"

	EmptyScoreboardNote = "nothing on the scoreboard yet"
)

type Task struct {
	Seed Seed

	// Owner names the context a file seed belongs to, or states plainly that
	// no context observes it. A context seed has no owner note: the seed is
	// the context.
	Owner string

	Verify []string

	Acceptance []string
}

type Pack struct {
	Seed Seed

	Repository string

	Task Task

	Owner Card

	Neighbors []Card

	// Blackboard and Scoreboard are what the swarm already knows about the
	// pack's contexts: the entries sit between the neighbor cards and the
	// claims, and Sections renders them in that order.
	Blackboard []BlackboardEntry

	Scoreboard []ScoreboardItem

	Claims []Card

	ModelZone string

	Budget int
}

const (
	RepositoryTitle = "repository card"

	TaskTitle = "task card"

	OwnerTitle = "owner context card"

	NeighborsTitle = "ranked neighbor cards"

	BlackboardTitle = "blackboard"

	ScoreboardTitle = "scoreboard"

	ClaimsTitle = "findings, proposals and plan items"

	BudgetTitle = "token budget"

	ModelZoneTitle = "model zone"
)

type Section struct {
	Title string

	Body string

	Priority int
}

func Sections(pack Pack) []Section {
	sections := []Section{
		{Title: RepositoryTitle, Body: pack.Repository, Priority: 0},
		{Title: TaskTitle, Body: renderTask(pack.Task), Priority: 0},
		{Title: OwnerTitle, Body: renderCard(pack.Owner), Priority: 0},
		{Title: NeighborsTitle, Body: renderCards(pack.Neighbors), Priority: 3},
		{Title: BlackboardTitle, Body: renderBlackboard(pack.Blackboard), Priority: 3},
		{Title: ScoreboardTitle, Body: renderScoreboard(pack.Scoreboard), Priority: 3},
		{Title: ClaimsTitle, Body: renderCards(pack.Claims), Priority: 4},
	}
	out := make([]Section, 0, len(sections))
	for _, section := range sections {
		if strings.TrimSpace(section.Body) == "" {
			continue
		}
		out = append(out, section)
	}
	return out
}

func renderTask(task Task) string {
	builder := strings.Builder{}
	fmt.Fprintf(&builder, "seed: %s\n", string(task.Seed.Kind)+":"+task.Seed.Value)
	if owner := strings.TrimSpace(task.Owner); owner != "" {
		fmt.Fprintf(&builder, "owner: %s\n", owner)
	}
	if len(task.Verify) > 0 {
		fmt.Fprintf(&builder, "verify: `%s`\n", strings.Join(task.Verify, " "))
	}
	if len(task.Acceptance) > 0 {
		fmt.Fprintf(&builder, "acceptance: `%s`\n", strings.Join(task.Acceptance, " "))
	}
	return builder.String()
}

func renderCard(card Card) string {
	if strings.TrimSpace(card.Body) == "" {
		return ""
	}
	if card.Title == "" {
		return card.Body
	}
	return card.Title + "\n" + card.Body
}

func renderCards(cards []Card) string {
	parts := []string{}
	for _, card := range cards {
		if rendered := renderCard(card); rendered != "" {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, "\n")
}

// renderBlackboard prints what other agent runs touched, decided or proposed
// for the pack's contexts, newest first, with the run's own work marked as its
// own. It never returns an empty body: a blackboard with nothing on it says so.
func renderBlackboard(entries []BlackboardEntry) string {
	if len(entries) == 0 {
		return EmptyBlackboardNote + "\n"
	}
	ordered := append([]BlackboardEntry{}, entries...)
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].At > ordered[right].At
	})
	builder := strings.Builder{}
	for _, entry := range ordered {
		if entry.Kind == "" && entry.Name == "" {
			continue
		}
		mark := ""
		if entry.Own {
			mark = OwnMark
		}
		line := joinFields(entry.Kind, entry.Name, mark, entry.Context, entry.Phase, firstLine(entry.Text))
		if line == "" {
			continue
		}
		fmt.Fprintf(&builder, "%s\n", line)
	}
	if builder.Len() == 0 {
		return EmptyBlackboardNote + "\n"
	}
	return builder.String()
}

// renderScoreboard prints the plan items that touch the pack's contexts, newest
// first, and says so when no plan item touches them.
func renderScoreboard(items []ScoreboardItem) string {
	if len(items) == 0 {
		return EmptyScoreboardNote + "\n"
	}
	ordered := append([]ScoreboardItem{}, items...)
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].At > ordered[right].At
	})
	builder := strings.Builder{}
	for _, item := range ordered {
		if item.Plan == "" && item.Item == "" {
			continue
		}
		line := joinFields(item.Plan, item.Item, item.Context, item.Phase, firstLine(item.Summary))
		if line == "" {
			continue
		}
		fmt.Fprintf(&builder, "%s\n", line)
	}
	if builder.Len() == 0 {
		return EmptyScoreboardNote + "\n"
	}
	return builder.String()
}

func joinFields(fields ...string) string {
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, " ")
}

func EstimateTokens(text string) int {
	return (len(text) + 3) / 4
}

func BudgetLine(tokens, budget int) string {
	return fmt.Sprintf("pack %s of %s tokens", tokenText(tokens), tokenText(budget))
}

func tokenText(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func Render(pack Pack, budgetTokens int) (string, []string) {
	if budgetTokens <= 0 {
		budgetTokens = pack.Budget
	}
	if budgetTokens <= 0 {
		budgetTokens = DefaultBudget
	}
	sections := Sections(pack)
	order := make([]int, len(sections))
	for index := range sections {
		order[index] = index
	}
	sort.SliceStable(order, func(left, right int) bool {
		return sections[order[left]].Priority < sections[order[right]].Priority
	})

	kept := map[int]bool{}
	dropped := []string{}
	spent := EstimateTokens(sectionText(ModelZoneTitle, pack.ModelZone))
	for _, index := range order {
		cost := EstimateTokens(sectionText(sections[index].Title, sections[index].Body))
		if spent+cost > budgetTokens && len(kept) > 0 {
			dropped = append(dropped, sections[index].Title)
			continue
		}
		kept[index] = true
		spent += cost
	}

	builder := strings.Builder{}
	for index, section := range sections {
		if !kept[index] {
			continue
		}
		builder.WriteString(sectionText(section.Title, section.Body))
	}
	builder.WriteString(sectionText(BudgetTitle, BudgetLine(spent, budgetTokens)))
	if len(dropped) > 0 {
		builder.WriteString(sectionText("dropped", "These sections did not fit the token budget: "+
			strings.Join(dropped, ", ")))
	}
	if strings.TrimSpace(pack.ModelZone) != "" {
		builder.WriteString(sectionText(ModelZoneTitle, pack.ModelZone))
	}
	return builder.String(), dropped
}

func sectionText(title, body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return "## " + title + "\n\n" + body + "\n\n"
}
