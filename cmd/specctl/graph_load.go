package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphproc"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
)

// runGraphServe starts a graph-node that belongs to no kcp instance, for
// reading a saved graph by hand. It keeps running until graph stop.
func runGraphServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl graph serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "directory the graph-node keeps its data, token and endpoint in")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *root == "" {
		fmt.Fprintln(stderr, "specctl graph serve: --root is required")
		return exitUsage
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph serve: %v\n", err)
		return exitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	instance, err := graphproc.Start(ctx, graphproc.Options{Root: absolute})
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph serve: %v\n", err)
		return exitError
	}
	printGraphEnv(stdout, instance)
	return exitOK
}

func runGraphStop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl graph stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "the --root graph serve was given")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	absolute, err := filepath.Abs(*root)
	if err != nil || *root == "" {
		fmt.Fprintln(stderr, "specctl graph stop: --root is required")
		return exitUsage
	}
	instance, ok := graphproc.Probe(absolute)
	if !ok {
		fmt.Fprintf(stdout, "no graph serves %s\n", absolute)
		return exitOK
	}
	graphproc.Stop(instance)
	fmt.Fprintf(stdout, "graph %s stopped\n", instance.BoltURL)
	return exitOK
}

func printGraphEnv(stdout io.Writer, instance graphproc.Instance) {
	for _, pair := range [][2]string{
		{"SPECD_BOLT_BACKEND", instance.Backend},
		{"SPECD_BOLT_URL", instance.BoltURL},
		{"SPECD_BOLT_USER", instance.User},
		{"SPECD_BOLT_PASSWORD_FILE", instance.TokenFile},
	} {
		fmt.Fprintf(stdout, "export %s=%q\n", pair[0], pair[1])
	}
}

// runGraphLoad writes a saved graph into a graph: the vertices.jsonl and
// edges.jsonl an open-architecture branch carries, read from a git ref or a
// directory. Vertices already in the graph with the same ids are replaced.
func runGraphLoad(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl graph load", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "git repository the --ref is read from")
	ref := fs.String("ref", "", "branch or commit holding graph/vertices.jsonl and graph/edges.jsonl, such as open-architecture/<repository>")
	dir := fs.String("dir", "", "directory holding graph/vertices.jsonl and graph/edges.jsonl instead of a ref")
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl graph load: %v\n", err)
		return exitUsage
	}
	if !bolt.Enabled() {
		fmt.Fprintln(stderr, "specctl graph load: no graph: run it in a checkout specctl up started, export the lines specctl graph serve prints, or pass --bolt-url")
		return exitUsage
	}
	if (*ref == "") == (*dir == "") {
		fmt.Fprintln(stderr, "specctl graph load: exactly one of --ref and --dir is required")
		return exitUsage
	}
	ctx := context.Background()
	vertexData, edgeData, err := savedGraph(ctx, *repo, *ref, *dir)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph load: %v\n", err)
		return exitError
	}
	vertices, err := parseVertices(vertexData)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph load: %v\n", err)
		return exitError
	}
	edges, err := parseEdges(edgeData)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph load: %v\n", err)
		return exitError
	}
	edges = graph.WithEndpoints(vertices, edges)

	client, err := bolt.Connect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph load: %v\n", err)
		return exitError
	}
	defer client.Close(ctx)
	ids := []int64{}
	vertexCount, edgeCount := 0, 0
	for _, set := range vertices {
		for _, vertex := range set.Rows {
			ids = append(ids, vertex.ID)
		}
	}
	if err := client.DeleteVertices(ctx, ids); err != nil {
		fmt.Fprintf(stderr, "specctl graph load: %v\n", err)
		return exitError
	}
	for _, set := range vertices {
		if err := client.WriteVertices(ctx, set); err != nil {
			fmt.Fprintf(stderr, "specctl graph load: write %s: %v\n", set.Label, err)
			return exitError
		}
		vertexCount += len(set.Rows)
	}
	for _, set := range edges {
		if err := client.WriteEdges(ctx, set); err != nil {
			fmt.Fprintf(stderr, "specctl graph load: write %s edges: %v\n", set.Type, err)
			return exitError
		}
		edgeCount += len(set.Rows)
	}
	fmt.Fprintf(stdout, "loaded %d vertices and %d edges into %s\n", vertexCount, edgeCount, bolt.URL)
	return exitOK
}

func savedGraph(ctx context.Context, repo, ref, dir string) ([]byte, []byte, error) {
	if dir != "" {
		vertices, err := os.ReadFile(filepath.Join(dir, oabranch.GraphVerticesPath))
		if err != nil {
			return nil, nil, err
		}
		edges, err := os.ReadFile(filepath.Join(dir, oabranch.GraphEdgesPath))
		if err != nil {
			return nil, nil, err
		}
		return vertices, edges, nil
	}
	top, err := session.TopLevel(repo)
	if err != nil {
		return nil, nil, err
	}
	store := oagit.Store{Repo: top}
	commit, err := store.Tip(ctx, ref)
	if err != nil || commit == "" {
		commit, err = store.Tip(ctx, "refs/heads/"+ref)
	}
	if err != nil {
		return nil, nil, err
	}
	if commit == "" {
		return nil, nil, fmt.Errorf("no branch or commit %q in %s", ref, top)
	}
	files, err := store.ReadFilesAt(ctx, commit, []string{oabranch.GraphVerticesPath, oabranch.GraphEdgesPath})
	if err != nil {
		return nil, nil, err
	}
	vertices, ok := files[oabranch.GraphVerticesPath]
	if !ok {
		return nil, nil, fmt.Errorf("%s at %s has no %s", ref, commit, oabranch.GraphVerticesPath)
	}
	return vertices, files[oabranch.GraphEdgesPath], nil
}

func jsonLines(data []byte, each func(record map[string]any) error) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		record := map[string]any{}
		if err := decoder.Decode(&record); err != nil {
			return fmt.Errorf("parse %q: %w", line, err)
		}
		if err := each(record); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func int64Of(value any) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%v is not a number", value)
	}
	return number.Int64()
}

func parseVertices(data []byte) ([]graph.VertexSet, error) {
	byLabel := map[string]*graph.VertexSet{}
	err := jsonLines(data, func(record map[string]any) error {
		label, _ := record["label"].(string)
		id, err := int64Of(record["id"])
		if err != nil || label == "" {
			return fmt.Errorf("a vertex without a label or an id: %v", record)
		}
		props := map[string]any{}
		for key, value := range record {
			if key == "label" || key == "id" {
				continue
			}
			if number, ok := value.(json.Number); ok {
				if whole, err := number.Int64(); err == nil {
					value = whole
				} else if real, err := number.Float64(); err == nil {
					value = real
				}
			}
			props[key] = value
		}
		set, ok := byLabel[label]
		if !ok {
			set = &graph.VertexSet{Label: label}
			byLabel[label] = set
		}
		set.Rows = append(set.Rows, graph.Vertex{ID: id, Props: props})
		return nil
	})
	return sortedSets(byLabel), err
}

func sortedSets(byLabel map[string]*graph.VertexSet) []graph.VertexSet {
	labels := make([]string, 0, len(byLabel))
	for label := range byLabel {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	out := make([]graph.VertexSet, 0, len(labels))
	for _, label := range labels {
		out = append(out, *byLabel[label])
	}
	return out
}

func parseEdges(data []byte) ([]graph.EdgeSet, error) {
	byKey := map[string]*graph.EdgeSet{}
	keys := []string{}
	err := jsonLines(data, func(record map[string]any) error {
		kind, _ := record["type"].(string)
		fromLabel, _ := record["fromLabel"].(string)
		toLabel, _ := record["toLabel"].(string)
		from, fromErr := int64Of(record["from"])
		to, toErr := int64Of(record["to"])
		if kind == "" || fromErr != nil || toErr != nil {
			return fmt.Errorf("an edge without a type or ends: %v", record)
		}
		key := kind + ":" + fromLabel + ":" + toLabel
		set, ok := byKey[key]
		if !ok {
			set = &graph.EdgeSet{Type: kind, FromLabel: fromLabel, ToLabel: toLabel}
			byKey[key] = set
			keys = append(keys, key)
		}
		set.Rows = append(set.Rows, graph.Edge{From: from, To: to})
		return nil
	})
	sort.Strings(keys)
	out := make([]graph.EdgeSet, 0, len(keys))
	for _, key := range keys {
		out = append(out, *byKey[key])
	}
	return out, err
}

// runGraphQuery runs one Cypher query against the graph and prints the rows.
func runGraphQuery(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl graph query", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "table", "table or json")
	bolt := boltflags.Add(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl graph query: %v\n", err)
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "specctl graph query: one Cypher query is required")
		return exitUsage
	}
	if !bolt.Enabled() {
		fmt.Fprintln(stderr, "specctl graph query: no graph: run it in a checkout specctl up started, export the lines specctl graph serve prints, or pass --bolt-url")
		return exitUsage
	}
	ctx := context.Background()
	client, err := bolt.Connect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph query: %v\n", err)
		return exitError
	}
	defer client.Close(ctx)
	rows, err := client.Run(ctx, positional[0], nil)
	if err != nil {
		fmt.Fprintf(stderr, "specctl graph query: %v\n", err)
		return exitError
	}
	if *output == "json" {
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl graph query: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(data))
		return exitOK
	}
	columns := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		for key := range row {
			if !seen[key] {
				seen[key] = true
				columns = append(columns, key)
			}
		}
	}
	sort.Strings(columns)
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, strings.Join(columns, "\t"))
	for _, row := range rows {
		cells := make([]string, 0, len(columns))
		for _, column := range columns {
			cells = append(cells, fmt.Sprint(row[column]))
		}
		fmt.Fprintln(table, strings.Join(cells, "\t"))
	}
	table.Flush()
	fmt.Fprintf(stderr, "%d row(s)\n", len(rows))
	return exitOK
}
