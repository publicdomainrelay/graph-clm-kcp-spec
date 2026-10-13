package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const (
	// findingSummaryLimit cuts a rendered summary, so a long one keeps the
	// columns of a list readable.
	findingSummaryLimit = 60

	// findingNameLimit cuts the slug a manual finding's name is built from.
	findingNameLimit = 40

	findingNamePrefix = "manual-"
)

// findingSources are the sources a Finding may carry, matching the enum the
// CRD accepts.
var findingSources = []string{"review", "proposal", "drift", "policy", "manual"}

func runFinding(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "specctl finding: a verb is required")
		return exitUsage
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "add":
		return runFindingAdd(rest, stdout, stderr)
	case "ls":
		return runFindingLs(rest, stdout, stderr)
	case "dismiss":
		return runFindingDismiss(rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl finding: unknown verb %q\n", verb)
	return exitUsage
}

// findingName is the object name of a manual finding: the "manual-" prefix and
// a slug of the summary cut to forty characters, so an anonymous add still gets
// a stable, valid name.
func findingName(summary string) string {
	slug := findingSlug(summary)
	cut := []rune(slug)
	if len(cut) > findingNameLimit {
		cut = cut[:findingNameLimit]
	}
	return findingNamePrefix + strings.Trim(string(cut), ".-")
}

func findingSlug(summary string) string {
	builder := strings.Builder{}
	dashed := false
	for _, char := range strings.ToLower(summary) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '.', char == '-':
			builder.WriteRune(char)
			dashed = false
		default:
			if !dashed && builder.Len() > 0 {
				builder.WriteByte('-')
				dashed = true
			}
		}
	}
	return strings.Trim(builder.String(), ".-")
}

// findingSource checks the source a finding declares: an absent one is manual,
// and any value outside the CRD's enum fails before anything is written.
func findingSource(source string) (string, error) {
	if source == "" {
		return "manual", nil
	}
	for _, allowed := range findingSources {
		if source == allowed {
			return source, nil
		}
	}
	return "", fmt.Errorf("source %q is not one of %s", source, strings.Join(findingSources, ", "))
}

func runFindingAdd(args []string, stdout, stderr io.Writer) int {
	name := "specctl finding add"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "what the finding says")
	source := fs.String("source", "manual", "review, proposal, drift, policy or manual")
	findingContext := fs.String("context", "", "the system context the finding concerns")
	unit := fs.String("unit", "", "the declared unit the finding concerns")
	severity := fs.String("severity", "", "how serious the finding is")
	ref := fs.String("ref", "", "where the finding came from")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) > 1 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", name, positional[1])
		return exitUsage
	}
	if strings.TrimSpace(*summary) == "" {
		fmt.Fprintf(stderr, "%s: --summary is required\n", name)
		return exitUsage
	}
	checkedSource, err := findingSource(*source)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitUsage
	}

	finding := spec.Finding{}
	if len(positional) == 1 {
		finding.Name = positional[0]
	} else {
		finding.Name = findingName(*summary)
	}
	finding.SetDefaults()
	finding.Namespace = options.namespace
	finding.Spec = spec.FindingSpec{
		Repository: sessionRepository(),
		Source:     checkedSource,
		Ref:        *ref,
		Context:    *findingContext,
		Unit:       *unit,
		Summary:    *summary,
		Severity:   *severity,
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	object, err := kcpclient.Unstructured(&finding)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	unstructured.RemoveNestedField(object.Object, "status")
	if _, err := client.ServerSideApply(ctx, object, "specctl-finding"); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	if _, err := client.PatchStatus(ctx, specapi.FindingGVR, finding.Namespace, finding.Name, map[string]any{"state": "Open"}); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	fmt.Fprintf(stdout, "finding %s added\n", finding.Name)
	return exitOK
}

func runFindingLs(args []string, stdout, stderr io.Writer) int {
	name := "specctl finding ls"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "list every state, not only Open and Planned")
	output := fs.String("o", "text", "text or json")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", name, positional[0])
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	listed, err := client.List(ctx, specapi.FindingGVR, options.namespace)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	findings := []spec.Finding{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
		finding, ok := typed.(*spec.Finding)
		if !ok {
			continue
		}
		findings = append(findings, *finding)
	}
	sort.SliceStable(findings, func(left, right int) bool { return findings[left].Name < findings[right].Name })
	if err := renderFindings(stdout, findings, *all, *output); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitUsage
	}
	return exitOK
}

func runFindingDismiss(args []string, stdout, stderr io.Writer) int {
	name := "specctl finding dismiss"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "why the finding is dismissed")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "%s: one finding name is required\n", name)
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	if _, err := client.PatchStatus(ctx, specapi.FindingGVR, options.namespace, positional[0], map[string]any{"state": "Dismissed", "message": *reason}); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	fmt.Fprintf(stdout, "finding %s dismissed\n", positional[0])
	return exitOK
}

// renderFindings writes the finding list: the summary columns, cut, and only
// the open findings unless every state was asked for.
func renderFindings(out io.Writer, findings []spec.Finding, all bool, format string) error {
	shown := []spec.Finding{}
	for _, finding := range findings {
		if !all && finding.Status.State != "Open" && finding.Status.State != "Planned" {
			continue
		}
		shown = append(shown, finding)
	}
	switch format {
	case "json":
		data, err := json.MarshalIndent(shown, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
		return nil
	case "text", "":
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "NAME\tSTATE\tSOURCE\tCONTEXT\tUNIT\tPLAN\tSUMMARY")
	for _, finding := range shown {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			finding.Name,
			finding.Status.State,
			finding.Spec.Source,
			finding.Spec.Context,
			finding.Spec.Unit,
			finding.Status.Plan,
			findingSummary(finding.Spec.Summary))
	}
	return table.Flush()
}

func findingSummary(summary string) string {
	cut := []rune(summary)
	if len(cut) > findingSummaryLimit {
		return string(cut[:findingSummaryLimit])
	}
	return summary
}
