package main

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"

	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/persist"
)

func runSync(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "git working tree whose open-architecture branch to sync")
	repository := fs.String("repository", "", "Repository name; default is the one whose resolved path is --repo")
	remote := fs.String("remote", "", "git remote to push the open-architecture branch to; empty pushes nothing")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir, err := repoDir(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitError
	}
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitError
	}
	name := *repository
	if name == "" {
		name, err = repositoryForPath(ctx, client, options.namespace, dir)
		if err != nil {
			fmt.Fprintf(stderr, "specctl sync: %v\n", err)
			return exitError
		}
	}
	result, err := persist.Persist(ctx, persist.Options{
		Cluster:    client,
		Namespace:  options.namespace,
		Repository: name,
		RepoPath:   dir,
		Remote:     *remote,
		Adopt:      true,
	})
	for _, context := range result.Created {
		fmt.Fprintf(stdout, "%-24s created from the branch\n", context)
	}
	for _, context := range result.Imported {
		fmt.Fprintf(stdout, "%-24s merged from the branch into kcp\n", context)
	}
	conflicted := make([]string, 0, len(result.Conflicts))
	for context := range result.Conflicts {
		conflicted = append(conflicted, context)
	}
	sort.Strings(conflicted)
	for _, context := range conflicted {
		fmt.Fprintf(stdout, "%-24s conflict: %s (kcp kept; edit the branch or kcp so they agree)\n", context, strings.Join(result.Conflicts[context], ", "))
	}
	if err != nil {
		fmt.Fprintf(stderr, "specctl sync: %v\n", err)
		return exitError
	}
	state := "unchanged"
	if result.Committed {
		state = fmt.Sprintf("committed %d path(s)", len(result.Paths))
	}
	fmt.Fprintf(stdout, "%s: %s at %s, %s", name, result.Branch, short(result.Commit), state)
	if result.Pushed {
		fmt.Fprintf(stdout, ", pushed to %s", *remote)
	}
	fmt.Fprintln(stdout)
	if len(conflicted) > 0 {
		return exitError
	}
	return exitOK
}

func runRestore(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "git working tree to restore the architecture of")
	repository := fs.String("repository", "", "Repository name; default is the origin remote's name, else the directory name of --repo")
	remote := fs.String("remote", "origin", "git remote to fetch open-architecture/<repository> from when the clone lacks it; empty fetches nothing")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir, err := repoDir(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl restore: %v\n", err)
		return exitError
	}
	name := *repository
	if name == "" {
		if top, err := session.TopLevel(dir); err == nil {
			name = repositoryNameFor(top)
		} else {
			name = ingest.SanitizeName(filepath.Base(dir))
		}
	}
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl restore: %v\n", err)
		return exitError
	}
	result, err := persist.Restore(context.Background(), persist.RestoreOptions{
		Cluster:    client,
		Namespace:  options.namespace,
		Repository: name,
		RepoPath:   dir,
		Remote:     *remote,
	})
	if errors.Is(err, persist.ErrNoBranch) {
		fmt.Fprintf(stderr, "specctl restore: %s has no %s locally or on %q; index it instead\n", dir, oabranch.Branch(name), *remote)
		return exitError
	}
	if err != nil {
		fmt.Fprintf(stderr, "specctl restore: %v\n", err)
		return exitError
	}
	from := "local branch"
	if result.Fetched {
		from = "fetched from " + *remote
	}
	fmt.Fprintf(stdout, "%s: restored %d context(s) from %s at %s (%s)\n", name, len(result.Contexts), result.Branch, short(result.Commit), from)
	return exitOK
}

func repoDir(path string) (string, error) {
	dir, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	if commit == "" {
		return "(none)"
	}
	return commit
}

func repositoryForPath(ctx context.Context, client *kcpclient.Client, namespace, dir string) (string, error) {
	listed, err := client.List(ctx, specapi.RepositoryGVR, namespace)
	if err != nil {
		return "", err
	}
	var names []string
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			return "", err
		}
		repository, ok := typed.(*spec.Repository)
		if !ok {
			continue
		}
		work := repository.WorkPath()
		if work == "" {
			continue
		}
		absolute, err := filepath.Abs(work)
		if err != nil {
			continue
		}
		if absolute == dir {
			return repository.Name, nil
		}
		names = append(names, fmt.Sprintf("%s (%s)", repository.Name, absolute))
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no Repository resolves to %s; apply one first", dir)
	}
	return "", fmt.Errorf("no Repository resolves to %s; known: %s", dir, strings.Join(names, ", "))
}
