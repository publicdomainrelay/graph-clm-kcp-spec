package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
)

func runPolicyModel(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy model", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "repository name")
	worktree := fs.String("worktree", "", "code checkout to index and model")
	commit := fs.String("commit", "", "commit of --path to export and model")
	path := fs.String("path", ".", "git repository holding the policy branch")
	branch := fs.String("branch", "", "code branch")
	defaultBranch := fs.String("default-branch", "main", "default code branch")
	libraryDir := fs.String("library", "", "read the policy library from this directory instead of the branch")
	output := fs.String("o", "text", "text or json")
	classifiers := stringsFlag{}
	fs.Var(&classifiers, "classifiers", "directory of extra classifier packs; repeatable")
	testGlobs := stringsFlag{}
	fs.Var(&testGlobs, "test-glob", "test file glob; repeatable")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *worktree != "" && *commit != "" {
		fmt.Fprintln(stderr, "specctl policy model: --worktree and --commit are exclusive")
		return exitUsage
	}
	if *repository == "" && *worktree != "" {
		*repository = repositoryForWorktree(*worktree)
	}
	if *repository == "" {
		*repository = filepath.Base(*path)
	}

	ctx := context.Background()
	library, err := loadLibrary(ctx, *repository, *libraryDir, *path, *branch, *defaultBranch)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}

	codeDir := *worktree
	cleanup := func() {}
	if *commit != "" {
		codeDir, cleanup, err = exportCommit(ctx, *path, *commit)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
			return exitError
		}
	} else if codeDir == "" {
		codeDir = *path
	}
	defer cleanup()
	if absolute, absErr := filepath.Abs(codeDir); absErr == nil {
		codeDir = absolute
	}
	resolved := *commit
	if resolved == "" {
		resolved = codegraphfacts.GitCommit(ctx, codeDir)
	}
	if len(testGlobs) == 0 {
		testGlobs = library.Manifest.TestGlobs
	}

	contexts := loadContexts(ctx, *path, *repository, *branch, *defaultBranch)
	graph, err := codegraphfacts.Build(ctx, codeDir, codegraphfacts.Options{
		Repository: *repository,
		Branch:     *branch,
		Commit:     resolved,
		TestGlobs:  testGlobs,
		Contexts:   codegraphfacts.ContextsByFile(contexts),
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}
	computed, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: classifierDirs(codeDir, classifiers),
		IncludeExtras:   true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}

	model, err := policy.BuildModel(policy.ModelInput{
		Repository: *repository,
		Graph:      graph,
		Effects:    computed,
		Contexts:   modelContexts(contexts),
		Binding:    library.Manifest.Binding(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
		return exitError
	}

	if *output == "json" {
		encoded, err := json.MarshalIndent(model, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy model: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
		return exitOK
	}
	printModel(stdout, resolved, model)
	return exitOK
}

func modelContexts(contexts []spec.SystemContext) []policy.ModelContext {
	out := make([]policy.ModelContext, 0, len(contexts))
	for _, context := range contexts {
		out = append(out, policy.ModelContext{Name: context.Name, Labels: context.Labels})
	}
	return out
}

func printModel(out io.Writer, commit string, model policy.ArchitectureModel) {
	fmt.Fprintf(out, "repository: %s  commit: %s\n", model.Metadata.Name, shortCommit(commit))
	fmt.Fprintf(out, "components: %d\n", len(model.Spec.Components))
	for _, component := range model.Spec.Components {
		context := component.Context
		if context == "" {
			context = "-"
		}
		fmt.Fprintf(out, "  %-24s roles=[%s] context=%s source=%s\n",
			component.Name, strings.Join(component.Roles, ","), context, component.Source)
	}
	fmt.Fprintf(out, "effects: %d\n", len(model.Spec.Effects))
	fmt.Fprintf(out, "flows: %d\n", len(model.Spec.Flows))
	for _, flow := range model.Spec.Flows {
		fmt.Fprintf(out, "  %s -> %s  initiator=%s channel=%s carries=[%s] purpose=%s source=%s\n",
			flow.From, flow.To, flow.Initiator, emptyDash(flow.Channel),
			strings.Join(flow.Carries, ","), emptyDash(flow.Purpose), flow.Source)
		for _, evidence := range flow.Evidence {
			fmt.Fprintf(out, "      %s\n", evidence)
		}
	}
	fmt.Fprintf(out, "triggers: %d\n", len(model.Spec.Triggers))
	counts := map[string]int{}
	keys := []string{}
	for _, trigger := range model.Spec.Triggers {
		key := trigger.From + " -> " + trigger.To
		if counts[key] == 0 {
			keys = append(keys, key)
		}
		counts[key]++
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(out, "  %s\n", key)
	}
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
