package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/cmd/internal/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/clm"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// clmFlags is what every `specctl clm` verb needs: the workspace to read and
// the optional graph to write. A host runs these as child processes, so the
// environment carries what a caller would otherwise pass as flags.
type clmFlags struct {
	kube *globals
	bolt *boltflags.Options
}

func newCLMFlags(args []string, stderr io.Writer, name string, extra func(*flag.FlagSet)) (*clmFlags, error) {
	fs := flag.NewFlagSet("specctl clm "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	kube := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if extra != nil {
		extra(fs)
	}
	if _, err := parseInterspersed(fs, args); err != nil {
		return nil, err
	}
	if err := bolt.Resolve(fs); err != nil {
		return nil, err
	}
	return &clmFlags{kube: kube, bolt: bolt}, nil
}

func (f *clmFlags) cluster() (*kcpclient.Client, error) {
	return f.kube.client()
}

// graphWriter connects the optional Bolt endpoint. A host with no graph
// configured reports into kcp alone, which is the durable half of the record;
// the graph is the derived index a reader queries while the change runs.
func (f *clmFlags) graphWriter(ctx context.Context, stderr io.Writer, prefix string) (graph.Writer, func()) {
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
		fmt.Fprintln(stderr, "specctl clm: render, apply or report is required")
		return exitUsage
	}
	switch args[0] {
	case "render":
		return runCLMRender(args[1:], stdout, stderr)
	case "apply":
		return runCLMApply(args[1:], stdout, stderr)
	case "report":
		return runCLMReport(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl clm: unknown subcommand %q\n", args[0])
	return exitUsage
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
	options, err := newCLMFlags(args, stderr, "apply", func(fs *flag.FlagSet) {
		fs.StringVar(&contextName, "context", "", "SystemContext the model zone belongs to")
		fs.StringVar(&file, "file", "", "model zone to read; - or empty reads standard input")
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
		Cluster:   cluster,
		Namespace: options.kube.namespace,
		Context:   contextName,
		Writer:    writer,
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
		result.Context, delta.Summary(result.Delta), foldedSuffix(result.Folded))
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

func foldedSuffix(folded string) string {
	if folded == "" {
		return ""
	}
	return ", folded into " + folded
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
