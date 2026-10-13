package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	packabc "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/contextpack"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	packimpl "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/contextpack"
	eval "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
)

func runEvalHistory(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl eval history", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "checkout whose own commits are walked")
	commits := fs.Int("commits", eval.DefaultHistoryCommits, "how many commits to walk, newest first")
	out := fs.String("out", "", "write the report here; the summary JSON lands beside it")
	budget := fs.Int("budget", packabc.DefaultBudget, "token budget of each pack")
	repositoryName := fs.String("repository", "", "repository whose contexts answer the owner lookup (default: the one the workspace holds)")
	root := fs.String("search-root", "", "instance root holding search.db (default: the instance of this checkout)")
	options := addGlobals(fs)
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl eval history: %v\n", err)
		return exitUsage
	}
	// The gold set is the owner lookup of this repository's contexts and the
	// pack is built from the live graph, so the reading is of the packs the
	// agents are handed, not of a stand-in built for the measurement.
	if !bolt.Enabled() {
		fmt.Fprintln(stderr, "specctl eval history: no graph: run it in a checkout specctl up started, export the lines specctl graph serve prints, or pass --bolt-url")
		return exitUsage
	}
	ctx := context.Background()
	cluster, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl eval history: %v\n", err)
		return exitError
	}
	repository, err := readRepositoryNamed(ctx, cluster, options.namespace, *repositoryName)
	if err != nil {
		fmt.Fprintf(stderr, "specctl eval history: %v\n", err)
		return exitError
	}
	reader, err := bolt.Connect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl eval history: %v\n", err)
		return exitError
	}
	defer reader.Close(ctx)
	contexts, err := observedContextsOf(ctx, cluster, options.namespace, repository)
	if err != nil {
		fmt.Fprintf(stderr, "specctl eval history: %v\n", err)
		return exitError
	}
	build := packimpl.Options{
		Reader:         reader,
		Cluster:        cluster,
		Namespace:      options.namespace,
		GraphNamespace: graphns.FromEnv(),
		Repository:     repository,
		Budget:         *budget,
	}
	if directory, ok := searchRoot(*root, options.kubeconfig); ok {
		if index, ok := searchIndexFor(directory); ok {
			defer index.Close()
			build.Index = index
		}
	}
	report, err := eval.RunHistory(ctx, eval.HistoryOptions{
		Repo:    *repo,
		Commits: *commits,
		Budget:  *budget,
		Owner: func(path string) (string, error) {
			return ownerOfPath(relativeToCheckout(repository, path), contexts), nil
		},
		Pack: func(ctx context.Context, query string) (packabc.Pack, error) {
			return packimpl.ForQuery(ctx, query, build)
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl eval history: %v\n", err)
		return exitError
	}
	fmt.Fprint(stdout, report.Markdown())
	if *out == "" {
		return exitOK
	}
	if err := writeHistoryReport(*out, report); err != nil {
		fmt.Fprintf(stderr, "specctl eval history: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stderr, "specctl eval history: wrote %s and %s\n", *out, jsonPath(*out))
	return exitOK
}

func writeHistoryReport(path string, report eval.HistoryReport) error {
	markdown, encoded := path, jsonPath(path)
	if strings.EqualFold(filepath.Ext(path), ".json") {
		markdown, encoded = markdownPath(path), path
	}
	if err := writeFile(markdown, []byte(report.Markdown())); err != nil {
		return err
	}
	body, err := report.JSON()
	if err != nil {
		return err
	}
	return writeFile(encoded, body)
}
