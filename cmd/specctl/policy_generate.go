package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const policyWaitTimeout = 20 * time.Minute

// runPolicyGenerate creates a PolicyChange: specd's harness authors the policy
// from the prompt and the requirements, checks it and records it as Evaluated.
func runPolicyGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "repository name the policy is generated for")
	prompt := fs.String("prompt", "", "the invariant in natural language")
	requirements := stringsFlag{}
	fs.Var(&requirements, "requirement", "requirement the policy enforces, as ctx#id; repeatable")
	contexts := stringsFlag{}
	fs.Var(&contexts, "systemcontext", "SystemContext the generation is given; repeatable")
	enforcement := fs.String("enforcement", "", "deny, warn or dryrun (default deny)")
	slug := fs.String("slug", "", "template slug; default derives from the prompt")
	apply := fs.Bool("apply", false, "commit and apply the policy once the checks pass")
	wait := fs.Bool("wait", false, "wait for the change to reach Evaluated, Applied or Failed")
	timeout := fs.Duration("timeout", policyWaitTimeout, "how long --wait waits")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *repository == "" {
		fmt.Fprintln(stderr, "specctl policy generate: --repo is required")
		return exitUsage
	}
	if strings.TrimSpace(*prompt) == "" {
		fmt.Fprintln(stderr, "specctl policy generate: --prompt is required")
		return exitUsage
	}
	if _, err := policy.RequirementRefs(requirements); err != nil {
		fmt.Fprintf(stderr, "specctl policy generate: %v\n", err)
		return exitUsage
	}
	change := &policy.PolicyChange{
		Spec: policy.PolicyChangeSpec{
			Repository:        *repository,
			Slug:              defaultSlug(*slug, *prompt),
			Prompt:            *prompt,
			Requirements:      requirements,
			Contexts:          contexts,
			EnforcementAction: policy.Enforcement(*enforcement),
			Apply:             *apply,
		},
	}
	change.Name = *repository + "-" + change.Spec.Slug
	return createPolicyChange(change, options, *wait, *timeout, stdout, stderr)
}

// runPolicyBind creates a PolicyChange in bind mode: the harness proposes the
// roles and the vocabulary of policies.yaml for one pack.
func runPolicyBind(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy bind", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "repository name the binding is generated for")
	pack := fs.String("pack", "", "pack the binding targets")
	version := fs.String("pack-version", "", "pack version; default is the version the repository imports")
	contexts := stringsFlag{}
	fs.Var(&contexts, "systemcontext", "SystemContext the generation is given; repeatable")
	apply := fs.Bool("apply", false, "commit and apply the binding once the checks pass")
	wait := fs.Bool("wait", false, "wait for the change to reach Evaluated, Applied or Failed")
	timeout := fs.Duration("timeout", policyWaitTimeout, "how long --wait waits")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *repository == "" || *pack == "" {
		fmt.Fprintln(stderr, "specctl policy bind: --repo and --pack are required")
		return exitUsage
	}
	change := &policy.PolicyChange{
		Spec: policy.PolicyChangeSpec{
			Repository:  *repository,
			Slug:        *pack,
			Pack:        *pack,
			PackVersion: *version,
			Prompt:      "bind the pack " + *pack + " to " + *repository,
			Contexts:    contexts,
			Apply:       *apply,
		},
	}
	change.Name = *repository + "-bind-" + *pack
	return createPolicyChange(change, options, *wait, *timeout, stdout, stderr)
}

// runPolicyAccept sets apply on an evaluated change and waits for it to land.
func runPolicyAccept(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy accept", flag.ContinueOnError)
	fs.SetOutput(stderr)
	wait := fs.Bool("wait", true, "wait for the change to reach Applied")
	timeout := fs.Duration("timeout", policyWaitTimeout, "how long --wait waits")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "specctl policy accept: exactly one PolicyChange name is required")
		return exitUsage
	}
	name := positional[0]
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy accept: %v\n", err)
		return exitError
	}
	change, err := readPolicyChange(ctx, client, options.namespace, name)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy accept: %v\n", err)
		return exitError
	}
	if change.Status.Phase != policy.PolicyPhaseEvaluated {
		fmt.Fprintf(stderr, "specctl policy accept: %s is %s, not %s\n", name, change.Status.Phase, policy.PolicyPhaseEvaluated)
		return exitError
	}
	change.Spec.Apply = true
	stamped, err := kcpclient.Unstructured(change)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy accept: %v\n", err)
		return exitError
	}
	if _, err := client.Apply(ctx, stamped); err != nil {
		fmt.Fprintf(stderr, "specctl policy accept: %v\n", err)
		return exitError
	}
	if !*wait {
		fmt.Fprintf(stdout, "%s accepted; specd applies it\n", name)
		return exitOK
	}
	return waitForPolicyChange(ctx, client, options.namespace, name, *timeout, stdout, stderr)
}

func createPolicyChange(change *policy.PolicyChange, options *globals, wait bool, timeout time.Duration, stdout, stderr io.Writer) int {
	change.SetDefaults()
	if err := policy.ValidatePolicyChange(change); err != nil {
		fmt.Fprintf(stderr, "specctl policy: %v\n", err)
		return exitUsage
	}
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy: %v\n", err)
		return exitError
	}
	object, err := kcpclient.Unstructured(change)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy: %v\n", err)
		return exitError
	}
	created, err := client.Create(ctx, object)
	if err != nil {
		if kcpclient.IsAlreadyExists(err) {
			fmt.Fprintf(stderr, "specctl policy: %s/%s already exists\n", change.Namespace, change.Name)
			return exitError
		}
		fmt.Fprintf(stderr, "specctl policy: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "policychange/%s created (%s)\n", created.GetName(), change.Spec.Mode())
	if !wait {
		return exitOK
	}
	return waitForPolicyChange(ctx, client, change.Namespace, change.Name, timeout, stdout, stderr)
}

func waitForPolicyChange(ctx context.Context, client *kcpclient.Client, namespace, name string, timeout time.Duration, stdout, stderr io.Writer) int {
	deadline := time.Now().Add(timeout)
	for {
		change, err := readPolicyChange(ctx, client, namespace, name)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy: %v\n", err)
			return exitError
		}
		switch change.Status.Phase {
		case policy.PolicyPhaseEvaluated:
			printPolicyChange(stdout, change)
			fmt.Fprintf(stdout, "%s is Evaluated; run specctl policy accept %s to apply it\n", name, name)
			return exitOK
		case policy.PolicyPhaseApplied:
			printPolicyChange(stdout, change)
			return exitOK
		case policy.PolicyPhaseFailed:
			printPolicyChange(stderr, change)
			return exitError
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(stderr, "specctl policy: %s is %s after %s\n", name, change.Status.Phase, timeout)
			return exitError
		}
		select {
		case <-ctx.Done():
			return exitError
		case <-time.After(2 * time.Second):
		}
	}
}

func readPolicyChange(ctx context.Context, client *kcpclient.Client, namespace, name string) (*policy.PolicyChange, error) {
	object, err := client.Get(ctx, specapi.PolicyChangeGVR, namespace, name)
	if err != nil {
		return nil, err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	change, ok := typed.(*policy.PolicyChange)
	if !ok {
		return nil, fmt.Errorf("%s is not a PolicyChange", name)
	}
	return change, nil
}

func printPolicyChange(out io.Writer, change *policy.PolicyChange) {
	fmt.Fprintf(out, "policychange/%s  %s  mode=%s  slug=%s\n",
		change.Name, change.Status.Phase, change.Spec.Mode(), change.Status.Slug)
	if change.Status.Attempt > 0 && change.Status.Phase != policy.PolicyPhaseApplied {
		fmt.Fprintf(out, "attempts: %d\n", change.Status.Attempt)
	}
	for _, check := range change.Status.Checks {
		state := "ok"
		if !check.Passed {
			state = "FAIL"
		}
		fmt.Fprintf(out, "  %-4s %s", state, check.Name)
		if check.Message != "" {
			fmt.Fprintf(out, ": %s", check.Message)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "violations against the head model: %d\n", len(change.Status.Violations))
	for _, violation := range change.Status.Violations {
		location := ""
		if violation.Location != nil {
			location = violation.Location.File
			if violation.Location.Line > 0 {
				location = fmt.Sprintf("%s:%d", violation.Location.File, violation.Location.Line)
			}
		}
		fmt.Fprintf(out, "  %-8s %-10s %s  %s\n", violation.Enforcement, violation.Severity, violation.Constraint, location)
		fmt.Fprintf(out, "           %s\n", violation.Msg)
	}
	if change.Status.Message != "" {
		fmt.Fprintf(out, "message: %s\n", change.Status.Message)
	}
	if change.Status.PolicyCommit != "" {
		fmt.Fprintf(out, "policy commit: %s\n", shortCommit(change.Status.PolicyCommit))
	}
}

// runPolicyChanges lists the PolicyChanges kcp holds.
func runPolicyChanges(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy changes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "only the changes of this repository")
	output := fs.String("o", "table", "table, json or name")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy changes: %v\n", err)
		return exitError
	}
	changes, err := listPolicyChanges(ctx, client, options.namespace, *repository)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy changes: %v\n", err)
		return exitError
	}
	return printPolicyChanges(stdout, stderr, changes, *output)
}

func listPolicyChanges(ctx context.Context, client *kcpclient.Client, namespace, repository string) ([]policy.PolicyChange, error) {
	listed, err := client.List(ctx, specapi.PolicyChangeGVR, namespace)
	if err != nil {
		return nil, err
	}
	out := []policy.PolicyChange{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			continue
		}
		change, ok := typed.(*policy.PolicyChange)
		if !ok {
			continue
		}
		if repository != "" && change.Spec.Repository != repository {
			continue
		}
		out = append(out, *change)
	}
	return out, nil
}

func printPolicyChanges(stdout, stderr io.Writer, changes []policy.PolicyChange, output string) int {
	switch output {
	case "json":
		encoded, err := json.MarshalIndent(changes, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy changes: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
	case "name":
		for _, change := range changes {
			fmt.Fprintf(stdout, "policychange/%s\n", change.Name)
		}
	default:
		fmt.Fprintf(stdout, "%-40s %-24s %-10s %-8s %s\n", "POLICYCHANGE", "REPOSITORY", "MODE", "PHASE", "SLUG")
		for _, change := range changes {
			fmt.Fprintf(stdout, "%-40s %-24s %-10s %-8s %s\n",
				change.Name, change.Spec.Repository, change.Spec.Mode(), change.Status.Phase, change.Status.Slug)
		}
	}
	return exitOK
}

func defaultSlug(slug, prompt string) string {
	if slug != "" {
		return slug
	}
	words := strings.FieldsFunc(strings.ToLower(prompt), func(char rune) bool {
		return !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9')
	})
	kept := []string{}
	for _, word := range words {
		if word == "" || policyStopWords[word] {
			continue
		}
		kept = append(kept, word)
		if len(kept) == 5 {
			break
		}
	}
	if len(kept) == 0 {
		return "policy"
	}
	joined := strings.Join(kept, "-")
	if len(joined) > 40 {
		joined = strings.Trim(joined[:40], "-")
	}
	return joined
}

var policyStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "to": true,
	"of": true, "for": true, "with": true, "must": true, "always": true,
	"never": true, "is": true, "be": true, "that": true, "it": true,
}
