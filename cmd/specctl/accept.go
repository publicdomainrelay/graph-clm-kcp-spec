package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/ingest"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
)

func runAccept(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl accept", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "the tree the acceptance steps run against")
	name := fs.String("name", "", "run only the step with this name")
	timeout := fs.Duration("timeout", 0, "the default a step may run; 0 uses the step's own timeoutSeconds or ten minutes")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir, err := repoDir(*repo)
	if err != nil {
		fmt.Fprintf(stderr, "specctl accept: %v\n", err)
		return exitError
	}

	kubeconfig, workspace, repository := options.kubeconfig, options.workspace, ""
	if record, ok := session.ForDir(dir); ok {
		if !flagSet(fs, "kubeconfig") && os.Getenv("SPECD_KUBECONFIG") == "" && kubeconfig == ".kcp-specd/admin.kubeconfig" {
			kubeconfig = record.AdminKubeconfig
		}
		if !flagSet(fs, "workspace") && workspace == "root:specs" {
			workspace = record.Workspace
		}
		repository = record.Repository
	}
	if repository == "" {
		repository = ingest.SanitizeName(filepath.Base(dir))
	}

	ctx := context.Background()
	client, err := kcpclient.New(kcpclient.Options{
		Kubeconfig: kubeconfig,
		Context:    options.context,
		Workspace:  workspace,
		Namespace:  options.namespace,
		QPS:        float32(options.qps),
		Burst:      options.burst,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl accept: %v\n", err)
		return exitError
	}
	object, err := client.Get(ctx, specapi.RepositoryGVR, options.namespace, repository)
	if err != nil {
		fmt.Fprintf(stderr, "specctl accept: Repository %s: %v\n", repository, err)
		return exitError
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		fmt.Fprintf(stderr, "specctl accept: %v\n", err)
		return exitError
	}
	typedRepository, ok := typed.(*spec.Repository)
	if !ok {
		fmt.Fprintf(stderr, "specctl accept: %s is not a Repository\n", repository)
		return exitError
	}

	steps := typedRepository.Spec.Acceptance
	if *name != "" {
		filtered := []spec.AcceptanceStep{}
		for _, step := range steps {
			if step.Name == *name {
				filtered = append(filtered, step)
			}
		}
		if len(filtered) == 0 {
			fmt.Fprintf(stderr, "specctl accept: Repository %s has no acceptance step named %q\n", repository, *name)
			return exitError
		}
		steps = filtered
	}
	if len(steps) == 0 {
		fmt.Fprintf(stdout, "Repository %s names no acceptance steps\n", repository)
		return exitOK
	}

	results := realize.RunAcceptance(ctx, steps, dir, *timeout)
	for _, result := range results {
		state := "failed"
		if result.Passed {
			state = "passed"
		}
		fmt.Fprintf(stdout, "accept %s: %s (exit %d, %.1fs)\n", result.Name, state, result.ExitCode, result.DurationSeconds)
		for _, line := range strings.Split(strings.TrimRight(result.OutputTail, "\n"), "\n") {
			fmt.Fprintf(stdout, "  %s\n", line)
		}
	}
	if blocked, gated := spec.AcceptanceBlocked(steps, results); gated {
		fmt.Fprintf(stderr, "specctl accept: %s failed (exit %d) and gates the commit\n", blocked.Name, blocked.ExitCode)
		return exitError
	}
	return exitOK
}
