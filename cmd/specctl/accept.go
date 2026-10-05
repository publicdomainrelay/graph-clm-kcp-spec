package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	override := fs.String("override", "", "record an override for this gating step on the Repository and run the steps anyway; needs --reason")
	reason := fs.String("reason", "", "why the step is overridden; required with --override")
	by := fs.String("by", "", "who overrides the step; defaults to the environment's user")
	timeout := fs.Duration("timeout", 0, "the default a step may run; 0 uses the step's own timeoutSeconds or ten minutes")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *override != "" && *reason == "" {
		fmt.Fprintln(stderr, "specctl accept: --override needs --reason")
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
	if *override != "" {
		user := *by
		if user == "" {
			user = os.Getenv("USER")
		}
		if user == "" {
			user = "operator"
		}
		found := false
		for _, step := range steps {
			if step.Name == *override {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(stderr, "specctl accept: Repository %s has no acceptance step named %q\n", repository, *override)
			return exitError
		}
		if err := recordAcceptanceOverride(ctx, client, typedRepository, spec.AcceptanceOverride{
			Step:   *override,
			Reason: *reason,
			By:     user,
			At:     time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			fmt.Fprintf(stderr, "specctl accept: %v\n", err)
			return exitError
		}
		fmt.Fprintf(stdout, "override recorded on Repository %s: %s by %s (%s)\n", repository, *override, user, *reason)
	}
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
	spec.ApplyOverrides(results, typedRepository.Spec.AcceptanceOverrides)
	for _, result := range results {
		state := "failed"
		if result.Passed {
			state = "passed"
		}
		suffix := ""
		if result.Overridden {
			suffix = fmt.Sprintf(" -- overridden by %s (%s)", result.OverrideBy, result.OverrideReason)
		}
		fmt.Fprintf(stdout, "accept %s: %s (exit %d, %.1fs)%s\n", result.Name, state, result.ExitCode, result.DurationSeconds, suffix)
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

// recordAcceptanceOverride writes one override onto the Repository, replacing
// an earlier entry for the same step so a changed reason does not stack.
func recordAcceptanceOverride(ctx context.Context, client *kcpclient.Client, repository *spec.Repository, override spec.AcceptanceOverride) error {
	kept := make([]spec.AcceptanceOverride, 0, len(repository.Spec.AcceptanceOverrides)+1)
	for _, existing := range repository.Spec.AcceptanceOverrides {
		if existing.Step == override.Step {
			continue
		}
		kept = append(kept, existing)
	}
	repository.Spec.AcceptanceOverrides = append(kept, override)
	object, err := kcpclient.Unstructured(repository)
	if err != nil {
		return err
	}
	if _, err := client.Apply(ctx, object); err != nil {
		return err
	}
	return nil
}
