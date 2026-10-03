package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltgraph"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/cmd/internal/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
)

type neighbor struct {
	edge      string
	direction string
	label     string
	key       string
}

func runGraph(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "specctl graph: neighbors or rebuild is required")
		return exitUsage
	}
	switch args[0] {
	case "neighbors":
		return runGraphNeighbors(args[1:], stdout, stderr)
	case "rebuild":
		return runGraphRebuild(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl graph: unknown subcommand %q\n", args[0])
	return exitUsage
}

func runGraphNeighbors(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl graph neighbors", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bolt := boltflags.Add(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl graph neighbors: %v\n", err)
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "specctl graph neighbors: one context name is required")
		return exitUsage
	}
	if bolt.URL == "" {
		fmt.Fprintln(stderr, "specctl graph neighbors: --bolt-url or SPECD_BOLT_URL is required")
		return exitUsage
	}
	ctx := context.Background()
	client, err := bolt.Connect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph neighbors: %v\n", err)
		return exitError
	}
	defer client.Close(ctx)

	name := resolveContextName(positional[0])
	contextID := graph.ContextID(name)
	found, err := client.Select(ctx, graph.LabelContext, []string{"name", "repo"}, map[string]any{"id": contextID})
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph neighbors: %v\n", err)
		return exitError
	}
	if len(found) == 0 {
		fmt.Fprintf(stderr, "specctl graph neighbors: no context %q in the graph\n", name)
		return exitError
	}

	neighbors, err := oneHop(ctx, client, graph.LabelContext, contextID)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph neighbors: %v\n", err)
		return exitError
	}
	printNeighbors(stdout, name, neighbors)
	return exitOK
}

func oneHop(ctx context.Context, client *boltgraph.Client, label string, id int64) ([]neighbor, error) {
	neighbors := []neighbor{}
	for _, edgeSpec := range append(append([]graph.EdgeSpec{}, graph.EdgeSpecs...), graph.LiveEdgeSpecs...) {
		if edgeSpec.FromLabel == label {
			rows, err := client.SelectOut(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id, graph.LabelProperties[edgeSpec.ToLabel])
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				neighbors = append(neighbors, neighbor{
					edge:      edgeSpec.Type,
					direction: "out",
					label:     edgeSpec.ToLabel,
					key:       displayKey(row, edgeSpec.ToLabel),
				})
			}
		}
		if edgeSpec.ToLabel == label {
			rows, err := client.SelectIn(ctx, edgeSpec.Type, edgeSpec.FromLabel, edgeSpec.ToLabel, id, graph.LabelProperties[edgeSpec.FromLabel])
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				neighbors = append(neighbors, neighbor{
					edge:      edgeSpec.Type,
					direction: "in",
					label:     edgeSpec.FromLabel,
					key:       displayKey(row, edgeSpec.FromLabel),
				})
			}
		}
	}
	sort.Slice(neighbors, func(left, right int) bool {
		if neighbors[left].edge != neighbors[right].edge {
			return neighbors[left].edge < neighbors[right].edge
		}
		if neighbors[left].direction != neighbors[right].direction {
			return neighbors[left].direction < neighbors[right].direction
		}
		return neighbors[left].key < neighbors[right].key
	})
	return neighbors, nil
}

func printNeighbors(out io.Writer, name string, neighbors []neighbor) {
	fmt.Fprintf(out, "context\t%s\n", name)
	if len(neighbors) == 0 {
		fmt.Fprintln(out, "no neighbors")
		return
	}
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "EDGE\tDIR\tLABEL\tKEY")
	for _, entry := range neighbors {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", entry.edge, entry.direction, entry.label, entry.key)
	}
	table.Flush()
}

func keyProperty(label string) string {
	switch label {
	case graph.LabelCodeRef:
		return "codegraphId"
	case graph.LabelRequirement:
		return "reqId"
	case graph.LabelInterface, graph.LabelContext, graph.LabelRepo, graph.LabelChange:
		return "name"
	case graph.LabelProgress:
		return "note"
	case graph.LabelPiMemory:
		return "title"
	}
	return ""
}

func displayKey(row map[string]any, label string) string {
	if property := keyProperty(label); property != "" {
		if value, ok := row[property]; ok && value != nil {
			return oneLine(fmt.Sprint(value), 80)
		}
	}
	parts := []string{}
	for _, property := range graph.LabelProperties[label] {
		if value, ok := row[property]; ok && value != nil {
			parts = append(parts, fmt.Sprint(value))
		}
	}
	return oneLine(strings.Join(parts, " "), 80)
}

func oneLine(value string, limit int) string {
	return truncate(strings.Join(strings.Fields(value), " "), limit)
}

func runGraphRebuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl graph rebuild", flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl graph rebuild: %v\n", err)
		return exitUsage
	}
	if bolt.URL == "" {
		fmt.Fprintln(stderr, "specctl graph rebuild: --bolt-url or SPECD_BOLT_URL is required")
		return exitUsage
	}
	ctx := context.Background()
	cluster, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph rebuild: %v\n", err)
		return exitError
	}
	writer, err := bolt.Connect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph rebuild: %v\n", err)
		return exitError
	}
	defer writer.Close(ctx)

	if err := ingest.RebuildGraph(ctx, cluster, writer, ingest.Options{Namespace: options.namespace}); err != nil {
		fmt.Fprintf(stderr, "specctl graph rebuild: %v\n", err)
		return exitError
	}

	counts := []string{}
	for _, label := range graph.ManagedLabels {
		ids, err := writer.SelectIDs(ctx, label)
		if err != nil {
			fmt.Fprintf(stderr, "specctl graph rebuild: %v\n", err)
			return exitError
		}
		counts = append(counts, fmt.Sprintf("%s=%d", label, len(ids)))
	}
	fmt.Fprintf(stdout, "graph rebuilt: %s\n", strings.Join(counts, " "))
	return exitOK
}
