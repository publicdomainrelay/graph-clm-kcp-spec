package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltgraph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type clmFlags struct {
	kube *globals
	bolt *boltflags.Options
	repo string

	positional []string
}

func newCLMFlags(args []string, stderr io.Writer, name string, extra func(*flag.FlagSet)) (*clmFlags, error) {
	fs := flag.NewFlagSet("specctl clm "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	kube := addGlobalsExcept(fs, "context")
	bolt := boltflags.Add(fs)
	repo := fs.String("repo", ".", "managed tree the repository skill of .specs/context-skill.md is read from")
	if extra != nil {
		extra(fs)
	}
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return nil, err
	}
	if err := bolt.Resolve(fs); err != nil {
		return nil, err
	}
	return &clmFlags{kube: kube, bolt: bolt, repo: *repo, positional: positional}, nil
}

// clmOptions is the shared option set of the graph-backed verbs; the reader and
// the writer are the same client.
func (f *clmFlags) clmOptions(writer graph.Writer, reader clm.Reader) clm.Options {
	return clm.Options{
		Namespace: f.kube.namespace,
		Writer:    writer,
		Reader:    reader,
		RepoPath:  f.repo,
	}
}

func (f *clmFlags) cluster() (*kcpclient.Client, error) {
	return f.kube.client()
}

func (f *clmFlags) graphWriter(ctx context.Context, stderr io.Writer, prefix string) (*boltgraph.Client, func()) {
	if f.bolt.URL == "" {
		return nil, nil
	}
	client, err := f.bolt.Connect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v (continuing without the graph)\n", prefix, err)
		return nil, nil
	}
	return client, func() { client.Close(ctx) }
}

func runCLM(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "specctl clm: render, apply, report, remember, recall or neighbors is required")
		return exitUsage
	}
	switch args[0] {
	case "render":
		return runCLMRender(args[1:], stdout, stderr)
	case "apply":
		return runCLMApply(args[1:], stdout, stderr)
	case "report":
		return runCLMReport(args[1:], stdout, stderr)
	case "remember":
		return runCLMRemember(args[1:], stdout, stderr)
	case "recall":
		return runCLMRecall(args[1:], stdout, stderr)
	case "neighbors":
		return runCLMNeighbors(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl clm: unknown subcommand %q\n", args[0])
	return exitUsage
}

// runCLMRemember writes one memory note of a context into the graph, in the
// instance's namespace. It is the explicit form of the note the apply path
// picks up from a model zone.
func runCLMRemember(args []string, stdout, stderr io.Writer) int {
	contextName := ""
	title := ""
	body := ""
	turn := 0
	refs := stringsFlag{}
	options, err := newCLMFlags(args, stderr, "remember", func(fs *flag.FlagSet) {
		fs.StringVar(&contextName, "context", "", "SystemContext the note was written in")
		fs.StringVar(&title, "title", "", "one-line title of the note")
		fs.StringVar(&body, "body", "", "body of the note; `- title: body` may carry more notes")
		fs.IntVar(&turn, "turn", 0, "agent turn the note came from")
		fs.Var(&refs, "ref", "code or claim unit the note names; repeatable")
	})
	if err != nil {
		return exitUsage
	}
	if contextName == "" || title == "" {
		fmt.Fprintln(stderr, "specctl clm remember: --context and --title are required")
		return exitUsage
	}
	ctx := context.Background()
	writer, closer := options.graphWriter(ctx, stderr, "specctl clm remember")
	if closer != nil {
		defer closer()
	}
	if writer == nil {
		fmt.Fprintln(stderr, "specctl clm remember: --bolt-url or SPECD_BOLT_URL is required")
		return exitUsage
	}
	linked, err := clm.Remember(ctx, options.clmOptions(writer, writer), contextName, title, body, turn, commaList(refs))
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm remember: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s\t%s\tlinked=%d\n", contextName, title, linked)
	return exitOK
}

func runCLMRecall(args []string, stdout, stderr io.Writer) int {
	limit := 20
	options, err := newCLMFlags(args, stderr, "recall", func(fs *flag.FlagSet) {
		fs.IntVar(&limit, "limit", 20, "how many notes to print")
	})
	if err != nil {
		return exitUsage
	}
	terms := options.positional
	if len(terms) == 0 {
		fmt.Fprintln(stderr, "specctl clm recall: one or more terms are required")
		return exitUsage
	}
	ctx := context.Background()
	writer, closer := options.graphWriter(ctx, stderr, "specctl clm recall")
	if closer != nil {
		defer closer()
	}
	if writer == nil {
		fmt.Fprintln(stderr, "specctl clm recall: --bolt-url or SPECD_BOLT_URL is required")
		return exitUsage
	}
	memories, err := clm.Recall(ctx, options.clmOptions(writer, writer), terms, limit)
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm recall: %v\n", err)
		return exitError
	}
	if len(memories) == 0 {
		fmt.Fprintf(stdout, "no memory matches %q\n", strings.Join(terms, " "))
		return exitOK
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "CONTEXT\tTURN\tTITLE\tBODY")
	for _, memory := range memories {
		fmt.Fprintf(table, "%s\t%d\t%s\t%s\n", memory.Context, memory.Turn, memory.Title, oneLine(memory.Body, maxDisplayKeyChars))
	}
	table.Flush()
	return exitOK
}

func runCLMNeighbors(args []string, stdout, stderr io.Writer) int {
	options, err := newCLMFlags(args, stderr, "neighbors", nil)
	if err != nil {
		return exitUsage
	}
	if len(options.positional) != 1 {
		fmt.Fprintln(stderr, "specctl clm neighbors: one context name is required")
		return exitUsage
	}
	ctx := context.Background()
	writer, closer := options.graphWriter(ctx, stderr, "specctl clm neighbors")
	if closer != nil {
		defer closer()
	}
	if writer == nil {
		fmt.Fprintln(stderr, "specctl clm neighbors: --bolt-url or SPECD_BOLT_URL is required")
		return exitUsage
	}
	name := resolveContextName(options.positional[0])
	neighbors, err := clm.Neighbors(ctx, options.clmOptions(writer, writer), name)
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm neighbors: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "context\t%s\n", name)
	if len(neighbors) == 0 {
		fmt.Fprintln(stdout, "no neighbors")
		return exitOK
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "EDGE\tDIR\tLABEL\tKEY")
	for _, entry := range neighbors {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", entry.Edge, entry.Direction, entry.Label, entry.Key)
	}
	table.Flush()
	return exitOK
}

func runCLMRender(args []string, stdout, stderr io.Writer) int {
	contextName := ""
	budget := 0
	options, err := newCLMFlags(args, stderr, "render", func(fs *flag.FlagSet) {
		fs.StringVar(&contextName, "context", "", "SystemContext to render")
		fs.IntVar(&budget, "context-doc-budget", 0, "token budget of the managed zone")
	})
	if err != nil {
		return exitUsage
	}
	if contextName == "" {
		fmt.Fprintln(stderr, "specctl clm render: --context is required")
		return exitUsage
	}
	ctx := context.Background()
	cluster, err := options.cluster()
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm render: %v\n", err)
		return exitError
	}
	rendered, err := clm.Render(ctx, clm.Options{
		Cluster:       cluster,
		Namespace:     options.kube.namespace,
		Context:       contextName,
		ManagedBudget: budget,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm render: %v\n", err)
		return exitError
	}
	fmt.Fprint(stdout, rendered.Document)
	return exitOK
}

func runCLMApply(args []string, stdout, stderr io.Writer) int {
	contextName := ""
	file := ""
	turn := 0
	allowRemove := stringsFlag{}
	options, err := newCLMFlags(args, stderr, "apply", func(fs *flag.FlagSet) {
		fs.StringVar(&contextName, "context", "", "SystemContext the model zone belongs to")
		fs.StringVar(&file, "file", "", "model zone to read; - or empty reads standard input")
		fs.IntVar(&turn, "turn", 0, "agent turn the model zone came from; the notes it holds carry it")
		fs.Var(&allowRemove, "allow-remove", "requirement id the document may delete without listing it under `removed:`; repeatable")
	})
	if err != nil {
		return exitUsage
	}
	if contextName == "" {
		fmt.Fprintln(stderr, "specctl clm apply: --context is required")
		return exitUsage
	}
	modelZone, err := readModelZone(file)
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm apply: %v\n", err)
		return exitError
	}
	ctx := context.Background()
	cluster, err := options.cluster()
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm apply: %v\n", err)
		return exitError
	}
	writer, closer := options.graphWriter(ctx, stderr, "specctl clm apply")
	if closer != nil {
		defer closer()
	}
	result, err := clm.Apply(ctx, clm.Options{
		Cluster:     cluster,
		Namespace:   options.kube.namespace,
		Context:     contextName,
		Writer:      writer,
		RepoPath:    options.repo,
		Turn:        turn,
		AllowRemove: commaList(allowRemove),
		Summary:     stderr,
	}, modelZone)
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm apply: %v\n", err)
		return exitError
	}
	encoded, err := json.MarshalIndent(result.Delta, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm apply: %v\n", err)
		return exitError
	}
	fmt.Fprintln(stdout, string(encoded))
	if !result.Applied {
		fmt.Fprintf(stderr, "specctl clm apply: no change: %s\n", result.Message)
		return exitOK
	}
	fmt.Fprintf(stderr, "specctl clm apply: %s applied (%s)%s\n",
		result.Context, delta.Summary(result.Delta), queuedSuffix(result.Queued))
	return exitOK
}

func runCLMReport(args []string, stdout, stderr io.Writer) int {
	change := ""
	eventJSON := ""
	event := clm.Event{}
	files := stringsFlag{}
	options, err := newCLMFlags(args, stderr, "report", func(fs *flag.FlagSet) {
		fs.StringVar(&change, "change", "", "SpecChange the event belongs to")
		fs.StringVar(&eventJSON, "event", "", "the event as JSON")
		fs.IntVar(&event.Turn, "turn", 0, "agent turn the event happened in")
		fs.StringVar(&event.Tool, "tool", "", "tool that ran")
		fs.Var(&files, "file", "file the tool touched, relative to the managed tree; repeatable")
		fs.StringVar(&event.Note, "note", "", "what the host wants a human to read")
	})
	if err != nil {
		return exitUsage
	}
	if change == "" {
		fmt.Fprintln(stderr, "specctl clm report: --change is required")
		return exitUsage
	}
	if eventJSON != "" {
		if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
			fmt.Fprintf(stderr, "specctl clm report: --event is not JSON: %v\n", err)
			return exitUsage
		}
	}
	event.Files = append(event.Files, files...)
	ctx := context.Background()
	cluster, err := options.cluster()
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm report: %v\n", err)
		return exitError
	}
	writer, closer := options.graphWriter(ctx, stderr, "specctl clm report")
	if closer != nil {
		defer closer()
	}
	result, err := clm.Report(ctx, clm.Options{
		Cluster:   cluster,
		Namespace: options.kube.namespace,
		Writer:    writer,
	}, clm.ReportOptions{Change: change, Event: event})
	if err != nil {
		fmt.Fprintf(stderr, "specctl clm report: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s progress=%d recorded=%t\n", result.Change, result.Progress, result.Recorded)
	return exitOK
}

func queuedSuffix(queued string) string {
	if queued == "" {
		return ""
	}
	return ", queued behind " + queued
}

func commaList(values stringsFlag) []string {
	out := []string{}
	for _, value := range values {
		for _, entry := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(entry); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func readModelZone(path string) (string, error) {
	if strings.TrimSpace(path) == "" || path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := readFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
