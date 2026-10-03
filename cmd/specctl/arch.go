package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/archkcp"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
)

func runImportArch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl import-arch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repository", "", "Repository name; defaults to the arch metadata name")
	repositoryPath := fs.String("repository-path", "", "path recorded on the Repository, for graph code ref resolution")
	noPrune := fs.Bool("no-prune", false, "keep SystemContexts the document no longer holds")
	noGraph := fs.Bool("no-graph", false, "do not write the graph even when a bolt url is set")
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl import-arch: %v\n", err)
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "specctl import-arch: one arch.yaml path is required")
		return exitUsage
	}
	data, err := readFile(positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "specctl import-arch: %v\n", err)
		return exitError
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl import-arch: %v\n", err)
		return exitError
	}

	result, err := archkcp.Import(ctx, client, data, archkcp.ImportOptions{
		Namespace:      options.namespace,
		Repository:     *repository,
		RepositoryPath: *repositoryPath,
		Prune:          !*noPrune,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl import-arch: %v\n", err)
		return exitError
	}

	wroteGraph := false
	if bolt.URL != "" && !*noGraph {
		writer, err := bolt.Connect(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "specctl import-arch: %v\n", err)
			return exitError
		}
		defer writer.Close(ctx)
		if err := ingest.RebuildGraph(ctx, client, writer, ingest.Options{Namespace: options.namespace}); err != nil {
			fmt.Fprintf(stderr, "specctl import-arch: %v\n", err)
			return exitError
		}
		wroteGraph = true
	}

	table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(table, "repository\t%s\n", result.Repository)
	fmt.Fprintf(table, "document\t%s\n", result.Document)
	fmt.Fprintf(table, "contexts\t%d\n", result.Contexts)
	fmt.Fprintf(table, "pruned\t%d\n", len(result.Deleted))
	fmt.Fprintf(table, "graph\t%v\n", wroteGraph)
	table.Flush()
	return exitOK
}

func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "arch", "output format: arch")
	repository := fs.String("repository", "", "Repository to export; needed when several are imported")
	output := fs.String("o", "-", "output file, - for stdout")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *format != "arch" {
		fmt.Fprintf(stderr, "specctl export: unknown format %q, only arch is supported\n", *format)
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl export: %v\n", err)
		return exitError
	}
	data, err := archkcp.Export(ctx, client, archkcp.ExportOptions{
		Namespace:  options.namespace,
		Repository: *repository,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl export: %v\n", err)
		return exitError
	}
	if *output == "-" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintf(stderr, "specctl export: %v\n", err)
			return exitError
		}
		return exitOK
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "specctl export: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stderr, "wrote %s\n", *output)
	return exitOK
}

// resolveContextName accepts either the kcp object name or the arch id, so
// `specctl graph neighbors sc.deno-kcp` finds the context named sc-deno-kcp.
func resolveContextName(value string) string {
	if spec.IsArchID(value) {
		return spec.ArchName(value)
	}
	return value
}
