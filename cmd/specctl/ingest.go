package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/cmd/internal/boltflags"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/agentfactory"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphcli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/summarize"
)

func runIngest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl ingest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "", "path to the working tree to ingest")
	repoName := fs.String("repo-name", "", "Repository name; defaults to the directory name")
	tool := fs.String("codegraph", "", "codegraph command to run")
	noGraph := fs.Bool("no-graph", false, "do not write the graph even when a bolt url is set")
	summarizeNow := fs.Bool("summarize", false, "fill the spec of every context whose intent is empty")
	agentKind := fs.String("agent", "", "agent to summarize with: scripted:<file> or claude")
	agentCommand := fs.String("agent-command", "", "model command to run (default deepseek-claude)")
	agentArgs := fs.String("agent-args", "", "model command arguments (default -p --output-format text)")
	agentTimeout := fs.Duration("agent-timeout", 3*time.Minute, "how long one model call may take")
	budget := fs.Int("bundle-budget", 0, "token budget of the context bundle")
	nodeLimit := fs.Int("bundle-nodes", 0, "how many codegraph node excerpts a bundle carries")
	managedBudget := fs.Int("context-doc-budget", 0, "token budget of the context document managed zone")
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *repo == "" {
		fmt.Fprintln(stderr, "specctl ingest: --repo is required")
		return exitUsage
	}

	agents, err := agentfactory.New(agentfactory.Options{
		Kind:    *agentKind,
		Command: *agentCommand,
		Args:    splitArgs(*agentArgs),
		Timeout: *agentTimeout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
		return exitUsage
	}
	if *summarizeNow && !agents.Configured() {
		fmt.Fprintln(stderr, "specctl ingest: --summarize needs --agent scripted:<file> or --agent claude")
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

	if !*summarizeNow {
		return exitOK
	}

	repoPath := result.SpecPath
	if abs, err := filepath.Abs(repoPath); err == nil {
		repoPath = abs
	}
	repository := readRepository(ctx, client, options.namespace, result.Repository, repoPath)

	summaries := []summarize.Result{}
	failed := false
	for _, contextResult := range result.Contexts {
		if contextResult.Skipped {
			continue
		}
		object, err := client.Get(ctx, specapi.SystemContextGVR, options.namespace, contextResult.Name)
		if err != nil {
			fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
			return exitError
		}
		existing, err := kcpclient.Typed(object)
		if err != nil {
			fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
			return exitError
		}
		if typed, ok := existing.(*spec.SystemContext); ok && typed.Spec.Intent != "" {
			continue
		}
		agent, err := agents.Agent(repository, repoPath)
		if err != nil {
			fmt.Fprintf(stderr, "specctl ingest: %v\n", err)
			return exitError
		}
		summary, err := summarize.Run(ctx, summarize.Options{
			Cluster:       client,
			Namespace:     options.namespace,
			Context:       contextResult.Name,
			Repository:    repository,
			Agent:         agent,
			Writer:        writer,
			Codegraph:     codegraphcli.Runner{Tool: *tool, Dir: repoPath},
			Budget:        *budget,
			NodeLimit:     *nodeLimit,
			ManagedBudget: *managedBudget,
		})
		if err != nil {
			fmt.Fprintf(stderr, "specctl ingest: %s: %v\n", contextResult.Name, err)
			failed = true
			continue
		}
		summaries = append(summaries, summary)
	}
	printSummarize(stdout, summaries)
	if failed {
		return exitError
	}
	return exitOK
}

func readRepository(ctx context.Context, client *kcpclient.Client, namespace, name, fallbackPath string) *spec.Repository {
	object, err := client.Get(ctx, specapi.RepositoryGVR, namespace, name)
	if err == nil {
		if typed, err := kcpclient.Typed(object); err == nil {
			if repository, ok := typed.(*spec.Repository); ok {
				return repository
			}
		}
	}
	return &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec.RepositorySpec{Path: fallbackPath},
	}
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
		switch {
		case context.Skipped:
			change = "skipped"
		case context.Created:
			change = "created"
		case context.SpecChanged:
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

func printSummarize(out io.Writer, summaries []summarize.Result) {
	if len(summaries) == 0 {
		fmt.Fprintln(out, "\nno context had an empty intent; nothing was summarized")
		return
	}
	fmt.Fprintln(out)
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "SUMMARIZED CONTEXT\tREQUIREMENTS\tINTERFACES\tDROPPED REFS\tSPEC\tDOC")
	for _, summary := range summaries {
		specState := ""
		if summary.Applied {
			specState = "written"
		}
		fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%s\t%s\n",
			summary.Context,
			len(summary.Draft.Requirements),
			len(summary.Draft.Interfaces),
			len(summary.Dropped),
			specState,
			summary.ContextDoc,
		)
	}
	table.Flush()

	// A ref the facts do not answer to is reported, not hidden: it is the one
	// thing a reader has to act on.
	for _, summary := range summaries {
		for _, dropped := range summary.Dropped {
			fmt.Fprintf(out, "  %s: dropped %s (%s)\n", dropped.Requirement, dropped.Ref, dropped.Reason)
		}
	}
}
