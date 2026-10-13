package contextpack

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/contextpack"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/finding"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/search"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
)

const DefaultLimit = 8

// Reader is the graph as this package reads it: HydraDB answers a vertex
// select and a single hop, and refuses anything longer, so a neighborhood is
// one hop out of the seed and nothing more.
type Reader interface {
	Select(ctx context.Context, label string, properties []string, filter map[string]any) ([]map[string]any, error)

	SelectOut(ctx context.Context, edgeType, fromLabel, toLabel string, fromID int64, properties []string) ([]map[string]any, error)

	SelectIn(ctx context.Context, edgeType, fromLabel, toLabel string, toID int64, properties []string) ([]map[string]any, error)
}

type Cluster interface {
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)
}

type Options struct {
	Reader Reader

	Cluster Cluster

	Index search.Index

	Namespace string

	GraphNamespace string

	Repository *spec.Repository

	DocDir string

	ModelZone string

	Budget int

	Limit int
}

func (o Options) graphNamespace() string {
	if o.GraphNamespace != "" {
		return o.GraphNamespace
	}
	return graphns.FromEnv()
}

func (o Options) namespace() string {
	if o.Namespace != "" {
		return o.Namespace
	}
	return specapi.DefaultNamespace
}

func ForContext(ctx context.Context, name string, options Options) (contextpack.Pack, error) {
	pack := contextpack.Pack{
		Seed:       contextpack.Seed{Kind: contextpack.SeedContext, Value: name},
		Repository: repositoryCard(options.Repository),
		Task: contextpack.Task{
			Seed:       contextpack.Seed{Kind: contextpack.SeedContext, Value: name},
			Verify:     taskCommands(options),
			Acceptance: acceptanceCommands(options),
		},
		ModelZone: options.ModelZone,
		Budget:    options.Budget,
	}
	card, found, err := contextCard(ctx, options, name)
	if err != nil {
		return pack, err
	}
	if found {
		pack.Owner = card
	}
	neighbors, err := neighborCards(ctx, options, name)
	if err != nil {
		return pack, err
	}
	pack.Neighbors = neighbors
	claims, err := claimCards(ctx, options, []string{name})
	if err != nil {
		return pack, err
	}
	pack.Claims = claims
	return pack, nil
}

func ForFile(ctx context.Context, path string, options Options) (contextpack.Pack, error) {
	owner, err := ownerOfFile(ctx, options, path)
	if err != nil {
		return contextpack.Pack{}, err
	}
	if owner == "" {
		return contextpack.Pack{
			Seed:       contextpack.Seed{Kind: contextpack.SeedFile, Value: path},
			Repository: repositoryCard(options.Repository),
			Task: contextpack.Task{
				Seed:       contextpack.Seed{Kind: contextpack.SeedFile, Value: path},
				Verify:     taskCommands(options),
				Acceptance: acceptanceCommands(options),
			},
			ModelZone: options.ModelZone,
			Budget:    options.Budget,
		}, nil
	}
	pack, err := ForContext(ctx, owner, options)
	if err != nil {
		return pack, err
	}
	pack.Seed = contextpack.Seed{Kind: contextpack.SeedFile, Value: path}
	pack.Task.Seed = pack.Seed
	return pack, nil
}

func ForQuery(ctx context.Context, terms string, options Options) (contextpack.Pack, error) {
	seed := contextpack.Seed{Kind: contextpack.SeedQuery, Value: terms}
	pack := contextpack.Pack{
		Seed:       seed,
		Repository: repositoryCard(options.Repository),
		Task: contextpack.Task{
			Seed:       seed,
			Verify:     taskCommands(options),
			Acceptance: acceptanceCommands(options),
		},
		ModelZone: options.ModelZone,
		Budget:    options.Budget,
	}
	if options.Index == nil {
		return pack, nil
	}
	limit := options.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	hits, err := options.Index.Search(ctx, terms, limit)
	if err != nil {
		return pack, err
	}
	candidates := []contextpack.Candidate{}
	cards := []contextpack.Card{}
	contexts := []string{}
	seen := map[string]bool{}
	for _, hit := range hits {
		candidates = append(candidates, contextpack.Candidate{
			ID: hit.ID, Label: hit.Label, Name: hit.Label, Context: hit.Context, Hit: hit.Score, Depth: 1,
		})
		if hit.Context == "" || seen[hit.Context] {
			continue
		}
		seen[hit.Context] = true
		contexts = append(contexts, hit.Context)
		card, found, err := contextCard(ctx, options, hit.Context)
		if err != nil {
			return pack, err
		}
		if found {
			cards = append(cards, card)
		}
	}
	ranked := contextpack.Rank(candidates, contextpack.DefaultPlan())
	ordered := []contextpack.Card{}
	byName := map[string]contextpack.Card{}
	for _, card := range cards {
		byName[card.Title] = card
	}
	for _, candidate := range ranked {
		card, ok := byName[candidate.Context]
		if !ok {
			continue
		}
		ordered = append(ordered, card)
		delete(byName, candidate.Context)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ordered = append(ordered, byName[name])
	}
	pack.Neighbors = ordered
	claims, err := claimCards(ctx, options, contexts)
	if err != nil {
		return pack, err
	}
	pack.Claims = claims
	return pack, nil
}

func taskCommands(options Options) []string {
	if options.Repository != nil && len(options.Repository.Spec.Verify) > 0 {
		return options.Repository.Spec.Verify
	}
	return nil
}

func acceptanceCommands(options Options) []string {
	if options.Repository == nil {
		return nil
	}
	commands := []string{}
	for _, step := range options.Repository.Spec.Acceptance {
		commands = append(commands, strings.Join(append([]string{step.Name + ":"}, step.Command...), " "))
	}
	return commands
}

func repositoryCard(repository *spec.Repository) string {
	if repository == nil {
		return ""
	}
	builder := strings.Builder{}
	fmt.Fprintf(&builder, "repository: %s\n", repository.Name)
	if path := repository.WorkPath(); path != "" {
		fmt.Fprintf(&builder, "path: %s\n", path)
	}
	return builder.String()
}

func contextCard(ctx context.Context, options Options, name string) (contextpack.Card, bool, error) {
	rows, err := options.Reader.Select(ctx, graph.LabelContext,
		[]string{"name", "repo", "intent"}, map[string]any{"id": graph.ContextIDIn(options.graphNamespace(), name)})
	if err != nil {
		return contextpack.Card{}, false, fmt.Errorf("contextpack: read context %s: %w", name, err)
	}
	if len(rows) == 0 {
		return contextpack.Card{}, false, nil
	}
	row := rows[0]
	builder := strings.Builder{}
	if intent := value(row, "intent"); intent != "" {
		fmt.Fprintf(&builder, "intent: %s\n", firstLine(intent))
	}
	return contextpack.Card{Title: value(row, "name"), Body: builder.String()}, true, nil
}

func neighborCards(ctx context.Context, options Options, name string) ([]contextpack.Card, error) {
	id := graph.ContextIDIn(options.graphNamespace(), name)
	plan := contextpack.DefaultPlan()
	candidates := []contextpack.Candidate{}
	cards := map[string]contextpack.Card{}
	for _, edgeSpec := range graph.EdgeSpecs {
		if edgeSpec.FromLabel == graph.LabelContext {
			rows, err := options.Reader.SelectOut(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id,
				graph.LabelProperties[edgeSpec.ToLabel])
			if err != nil {
				return nil, fmt.Errorf("contextpack: read %s out of %s: %w", edgeSpec.Type, name, err)
			}
			for _, row := range rows {
				collect(ctx, options, &candidates, cards, agent.DirectionOut, edgeSpec, row)
			}
		}
		if edgeSpec.ToLabel == graph.LabelContext {
			rows, err := options.Reader.SelectIn(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id,
				graph.LabelProperties[edgeSpec.FromLabel])
			if err != nil {
				return nil, fmt.Errorf("contextpack: read %s into %s: %w", edgeSpec.Type, name, err)
			}
			for _, row := range rows {
				collect(ctx, options, &candidates, cards, agent.DirectionIn, edgeSpec, row)
			}
		}
	}
	ordered := []contextpack.Card{}
	for _, candidate := range contextpack.Rank(candidates, plan) {
		card, ok := cards[candidate.Name]
		if !ok {
			continue
		}
		ordered = append(ordered, card)
		delete(cards, candidate.Name)
	}
	rest := make([]string, 0, len(cards))
	for title := range cards {
		rest = append(rest, title)
	}
	sort.Strings(rest)
	for _, title := range rest {
		ordered = append(ordered, cards[title])
	}
	return ordered, nil
}

func collect(
	ctx context.Context,
	options Options,
	candidates *[]contextpack.Candidate,
	cards map[string]contextpack.Card,
	direction string,
	edgeSpec graph.EdgeSpec,
	row map[string]any,
) {
	label := edgeSpec.ToLabel
	if direction == agent.DirectionIn {
		label = edgeSpec.FromLabel
	}
	name := neighborName(row)
	neighbor := contextpack.Neighbor{
		Name:      name,
		Edge:      edgeSpec.Type,
		Direction: direction,
		Label:     label,
		Intent:    value(row, "intent"),
	}
	if label == graph.LabelContext && name != "" {
		neighbor.Requirements, neighbor.Interfaces = contextFacts(ctx, options, name)
	}
	card := contextpack.NeighborCard(neighbor)
	body := strings.Builder{}
	for _, key := range graph.LabelProperties[label] {
		if key == "name" || key == "intent" {
			continue
		}
		if value := value(row, key); value != "" {
			fmt.Fprintf(&body, "%s: %s\n", key, oneLine(value))
		}
	}
	body.WriteString(card.Body)
	title := card.Title
	if title == "" {
		title = direction + " " + edgeSpec.Type + " " + label + " " + name
	}
	cards[title] = contextpack.Card{Title: title, Body: body.String()}
	*candidates = append(*candidates, contextpack.Candidate{
		Label: label, Name: title, Context: name, Edge: edgeSpec.Type, Depth: 1,
		Owner: direction == agent.DirectionOut && edgeSpec.Type == graph.EdgeOrchestrator,
	})
}

func contextFacts(ctx context.Context, options Options, name string) ([]string, []string) {
	id := graph.ContextIDIn(options.graphNamespace(), name)
	requirements := []string{}
	rows, err := options.Reader.SelectOut(ctx, graph.EdgeRequires, graph.LabelContext, graph.LabelRequirement, id,
		graph.LabelProperties[graph.LabelRequirement])
	if err == nil {
		for _, row := range rows {
			if !strings.EqualFold(value(row, "level"), string(spec.LevelMust)) {
				continue
			}
			requirements = append(requirements, value(row, "reqId")+": "+oneLine(value(row, "text")))
		}
	}
	interfaces := []string{}
	rows, err = options.Reader.SelectOut(ctx, graph.EdgeDeclares, graph.LabelContext, graph.LabelInterface, id,
		graph.LabelProperties[graph.LabelInterface])
	if err == nil {
		for _, row := range rows {
			interfaces = append(interfaces, value(row, "name")+" ("+value(row, "kind")+") "+value(row, "signature"))
		}
	}
	return requirements, interfaces
}

func ownerOfFile(ctx context.Context, options Options, path string) (string, error) {
	rows, err := options.Reader.Select(ctx, graph.LabelCodeRef, []string{"codegraphId", "filePath"},
		map[string]any{"filePath": path})
	if err != nil {
		return "", fmt.Errorf("contextpack: read code ref %s: %w", path, err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	id := int64Value(rows[0]["id"])
	owners, err := options.Reader.SelectIn(ctx, graph.EdgeReferences, graph.LabelContext, graph.LabelCodeRef, id,
		graph.LabelProperties[graph.LabelContext])
	if err != nil {
		return "", fmt.Errorf("contextpack: read the owner of %s: %w", path, err)
	}
	if len(owners) == 0 {
		return "", nil
	}
	return value(owners[0], "name"), nil
}

func claimCards(ctx context.Context, options Options, contexts []string) ([]contextpack.Card, error) {
	if options.Cluster == nil || len(contexts) == 0 {
		return nil, nil
	}
	wanted := map[string]bool{}
	for _, name := range contexts {
		wanted[name] = true
	}
	cards := []contextpack.Card{}
	findings, err := options.Cluster.List(ctx, specapi.FindingGVR, options.namespace())
	if err != nil {
		return nil, fmt.Errorf("contextpack: read findings: %w", err)
	}
	for index := range findings.Items {
		item := &findings.Items[index]
		contextName, _, _ := unstructured.NestedString(item.Object, "spec", "context")
		if !wanted[contextName] {
			continue
		}
		state, _, _ := unstructured.NestedString(item.Object, "status", "state")
		if state == finding.Resolved || state == finding.Dismissed {
			continue
		}
		summary, _, _ := unstructured.NestedString(item.Object, "spec", "summary")
		cards = append(cards, contextpack.Card{
			Title: "finding " + item.GetName(),
			Body:  oneLine(contextName + ": " + summary),
		})
	}
	plans, err := options.Cluster.List(ctx, specapi.PlanGVR, options.namespace())
	if err != nil {
		return cards, nil
	}
	for index := range plans.Items {
		item := &plans.Items[index]
		items, _, _ := unstructured.NestedSlice(item.Object, "spec", "items")
		for _, entry := range items {
			planItem, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			contextName, _ := planItem["context"].(string)
			if !wanted[contextName] {
				continue
			}
			phase, _ := planItem["status"].(map[string]any)
			if valueOf(phase, "phase") == specapi.PhaseSucceeded {
				continue
			}
			cards = append(cards, contextpack.Card{
				Title: "plan item " + item.GetName(),
				Body:  oneLine(contextName + ": " + valueOf(planItem, "id") + " " + valueOf(planItem, "summary")),
			})
		}
	}
	return cards, nil
}

func neighborName(row map[string]any) string {
	for _, key := range []string{"name", "reqId", "codegraphId", "title"} {
		if found := value(row, key); found != "" {
			return found
		}
	}
	return ""
}

func value(row map[string]any, key string) string {
	if row == nil {
		return ""
	}
	found, ok := row[key]
	if !ok || found == nil {
		return ""
	}
	return fmt.Sprint(found)
}

func valueOf(row map[string]any, key string) string {
	return value(row, key)
}

func int64Value(found any) int64 {
	switch typed := found.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	}
	return 0
}

func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.Index(trimmed, "\n"); index >= 0 {
		trimmed = trimmed[:index]
	}
	return trimmed
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
