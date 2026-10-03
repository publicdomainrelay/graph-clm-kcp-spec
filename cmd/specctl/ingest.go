package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/cmd/internal/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
)

func runIngest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl ingest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "", "path to the working tree to ingest")
	repoName := fs.String("repo-name", "", "Repository name; defaults to the directory name")
	tool := fs.String("codegraph", "", "codegraph command to run")
	noGraph := fs.Bool("no-graph", false, "do not write the graph even when a bolt url is set")
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *repo == "" {
		fmt.Fprintln(stderr, "specctl ingest: --repo is required")
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitError
	}

	var writer graph.Writer
	if bolt.URL != "" && !*noGraph {
		graphClient, err := bolt.Connect(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
			return exitError
		}
		defer graphClient.Close(ctx)
		writer = graphClient
	}

	result, err := ingest.Run(ctx, client, ingest.Options{
		RepoPath:       *repo,
		RepositoryName: *repoName,
		Namespace:      options.namespace,
		Tool:           *tool,
		Writer:         writer,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitError
	}
	printIngest(stdout, result, writer != nil)
	return exitOK
}

func printIngest(out io.Writer, result ingest.Result, wroteGraph bool) {
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintf(table, "repository\t%s\n", result.Repository)
	fmt.Fprintf(table, "path\t%s\n", result.SpecPath)
	fmt.Fprintf(table, "commit\t%s\n", truncate(result.Commit, 12))
	fmt.Fprintf(table, "graph\t%v\n", wroteGraph)
	fmt.Fprintln(table)
	fmt.Fprintln(table, "CONTEXT\tFILES\tINTERFACES\tFINGERPRINT\tVALID\tSYNCED\tCHANGE")
	for _, context := range result.Contexts {
		change := ""
		if context.Created {
			change = "created"
		} else if context.SpecChanged {
			change = "spec"
		}
		fmt.Fprintf(table, "%s\t%d\t%d\t%s\t%s\t%s\t%s\n",
			context.Name,
			len(context.Observed.Files),
			len(context.Observed.Interfaces),
			truncate(context.Fingerprint, 12),
			conditionStatusOf(context.Conditions, specapi.ConditionSpecValid),
			conditionStatusOf(context.Conditions, specapi.ConditionCodeSynced),
			change,
		)
	}
	table.Flush()
}
