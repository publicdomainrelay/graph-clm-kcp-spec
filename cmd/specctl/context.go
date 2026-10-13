package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	packabc "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/contextpack"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltflags"
	packimpl "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/contextpack"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func runContext(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "specctl context: pack is required")
		return exitUsage
	}
	switch args[0] {
	case "pack":
		return runContextPack(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl context: unknown subcommand %q\n", args[0])
	return exitUsage
}

func runContextPack(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl context pack", flag.ContinueOnError)
	fs.SetOutput(stderr)
	contextName := fs.String("context", "", "the context the pack is for")
	file := fs.String("file", "", "a file whose owner context seeds the pack")
	query := fs.String("query", "", "search terms that seed the pack")
	repositoryName := fs.String("repository", "", "repository the pack is for (default: the one the workspace holds)")
	budget := fs.Int("budget", packabc.DefaultBudget, "token budget of the pack")
	root := fs.String("search-root", "", "instance root holding search.db (default: the instance of this checkout)")
	// --context names the system context here, so the global kubeconfig context
	// flag is left out: defining both panics on a redefined flag.
	options := addGlobalsExcept(fs, "context")
	bolt := boltflags.Add(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := bolt.Resolve(fs); err != nil {
		fmt.Fprintf(stderr, "specctl context pack: %v\n", err)
		return exitUsage
	}
	named := 0
	for _, value := range []string{*contextName, *file, *query} {
		if value != "" {
			named++
		}
	}
	if named != 1 {
		fmt.Fprintln(stderr, "specctl context pack: name one of --context, --file or --query")
		return exitUsage
	}

	ctx := context.Background()
	cluster, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl context pack: %v\n", err)
		return exitError
	}
	repository, err := readRepositoryNamed(ctx, cluster, options.namespace, *repositoryName)
	if err != nil {
		fmt.Fprintf(stderr, "specctl context pack: %v\n", err)
		return exitError
	}

	build := packimpl.Options{
		Cluster:    cluster,
		Namespace:  options.namespace,
		Repository: repository,
		Budget:     *budget,
	}
	if bolt.URL != "" {
		reader, err := bolt.Connect(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "specctl context pack: %v\n", err)
			return exitError
		}
		defer reader.Close(ctx)
		build.Reader = reader
	}
	if directory, ok := searchRoot(*root); ok {
		if index, ok := searchIndexFor(directory); ok {
			defer index.Close()
			build.Index = index
		}
	}

	var pack packabc.Pack
	switch {
	case *contextName != "":
		pack, err = packimpl.ForContext(ctx, *contextName, build)
	case *file != "":
		pack, err = packimpl.ForFile(ctx, *file, build)
	default:
		pack, err = packimpl.ForQuery(ctx, *query, build)
	}
	if err != nil {
		fmt.Fprintf(stderr, "specctl context pack: %v\n", err)
		return exitError
	}
	text, dropped := packabc.Render(pack, *budget)
	fmt.Fprint(stdout, text)
	if len(dropped) > 0 {
		fmt.Fprintf(stdout, "the pack dropped %d section(s) over %d tokens\n", len(dropped), *budget)
	}
	return exitOK
}

func readRepositoryNamed(ctx context.Context, cluster *kcpclient.Client, namespace, name string) (*spec.Repository, error) {
	if name != "" {
		object, err := cluster.Get(ctx, specapi.RepositoryGVR, namespace, name)
		if err != nil {
			return nil, err
		}
		typed, err := kcpclient.Typed(object)
		if err != nil {
			return nil, err
		}
		repository, ok := typed.(*spec.Repository)
		if !ok {
			return nil, fmt.Errorf("%s is not a Repository", name)
		}
		return repository, nil
	}
	listed, err := cluster.List(ctx, specapi.RepositoryGVR, namespace)
	if err != nil {
		return nil, err
	}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return nil, err
		}
		if repository, ok := typed.(*spec.Repository); ok {
			return repository, nil
		}
	}
	return nil, fmt.Errorf("the workspace holds no repository")
}
