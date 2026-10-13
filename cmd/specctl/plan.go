package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/plan"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
)

func runPlan(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "specctl plan: a verb is required")
		return exitUsage
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "apply":
		return runPlanApply(rest, stdout, stderr)
	case "ls":
		return runPlanLs(rest, stdout, stderr)
	case "show":
		return runPlanShow(rest, stdout, stderr)
	case "approve":
		return runPlanApprove(rest, stdout, stderr)
	case "preview":
		return runPlanPreview(rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl plan: unknown verb %q\n", verb)
	return exitUsage
}

// sessionRepository names the repository of the checkout, so a plan document
// that does not name one is applied to the instance the caller stands in.
func sessionRepository() string {
	if record, ok := session.ForDir("."); ok {
		return record.Repository
	}
	return ""
}

// readPlans reads a YAML stream into Plan values: every document is decoded on
// its own, the namespace and the repository an empty one leaves behind come
// from the caller, and approval never comes from the file.
func readPlans(data []byte, namespace, repository string) ([]spec.Plan, error) {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	out := []spec.Plan{}
	for {
		document, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read plan document: %w", err)
		}
		if len(bytes.TrimSpace(document)) == 0 {
			continue
		}
		subject := spec.Plan{}
		if err := yaml.Unmarshal(document, &subject); err != nil {
			return nil, fmt.Errorf("read plan document: %w", err)
		}
		if subject.Kind != "" && subject.Kind != specapi.PlanKind {
			return nil, fmt.Errorf("document kind %q is not a Plan", subject.Kind)
		}
		subject.Spec.Approved = false
		subject.Spec.ApprovedBy = ""
		if subject.Namespace == "" {
			subject.Namespace = namespace
		}
		if subject.Spec.Repository == "" {
			subject.Spec.Repository = repository
		}
		out = append(out, subject)
	}
	if len(out) == 0 {
		return nil, errors.New("no Plan document")
	}
	return out, nil
}

// approvalReset reports whether a stored plan loses its approval: only an
// approved plan whose items differ from the ones being applied resets.
func approvalReset(stored *spec.Plan, applied spec.Plan) bool {
	if stored == nil || !stored.Spec.Approved {
		return false
	}
	return !reflect.DeepEqual(stored.Spec.Items, applied.Spec.Items)
}

// planObject renders a plan the way an apply writes it: the spec and the
// metadata, never the status the controller owns.
func planObject(subject spec.Plan) (*unstructured.Unstructured, error) {
	object, err := kcpclient.Unstructured(&subject)
	if err != nil {
		return nil, err
	}
	unstructured.RemoveNestedField(object.Object, "status")
	return object, nil
}

// storedPlan reads the plan kcp holds, nil when there is none.
func storedPlan(ctx context.Context, client *kcpclient.Client, namespace, name string) (*spec.Plan, error) {
	object, err := client.Get(ctx, specapi.PlanGVR, namespace, name)
	if err != nil {
		if kcpclient.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, err
	}
	stored, ok := typed.(*spec.Plan)
	if !ok {
		return nil, fmt.Errorf("%s is not a Plan", name)
	}
	return stored, nil
}

func runPlanApply(args []string, stdout, stderr io.Writer) int {
	name := "specctl plan apply"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	files := stringsFlag{}
	fs.Var(&files, "f", "a Plan document file, - for stdin; repeatable")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", name, positional[0])
		return exitUsage
	}
	if len(files) == 0 {
		fmt.Fprintf(stderr, "%s: -f is required\n", name)
		return exitUsage
	}

	repository := sessionRepository()
	subjects := []spec.Plan{}
	for _, file := range files {
		data, err := readFile(file)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
		plans, err := readPlans(data, options.namespace, repository)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %s: %v\n", name, file, err)
			return exitError
		}
		subjects = append(subjects, plans...)
	}
	// Every document is checked before anything is written, so a plan that
	// abc/plan rejects leaves kcp untouched.
	for _, subject := range subjects {
		if subject.Name == "" {
			fmt.Fprintf(stderr, "%s: a Plan document has no metadata.name\n", name)
			return exitError
		}
		if err := plan.Validate(subject.Spec); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}

	for _, subject := range subjects {
		stored, err := storedPlan(ctx, client, options.namespace, subject.Name)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
		reset := approvalReset(stored, subject)
		subject.SetDefaults()
		if subject.Namespace == "" {
			subject.Namespace = options.namespace
		}
		object, err := planObject(subject)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
		if _, err := client.ServerSideApply(ctx, object, "specctl-plan"); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
		if reset {
			fmt.Fprintln(stdout, "approval reset: the items changed")
		}
		fmt.Fprintf(stdout, "plan %s applied (%d items)\n", subject.Name, len(subject.Spec.Items))
	}
	return exitOK
}

func runPlanLs(args []string, stdout, stderr io.Writer) int {
	name := "specctl plan ls"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
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
	listed, err := client.List(ctx, specapi.PlanGVR, options.namespace)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	subjects := []spec.Plan{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
		subject, ok := typed.(*spec.Plan)
		if !ok {
			continue
		}
		subjects = append(subjects, *subject)
	}
	sort.SliceStable(subjects, func(left, right int) bool { return subjects[left].Name < subjects[right].Name })
	if err := renderPlans(stdout, subjects, *output); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitUsage
	}
	return exitOK
}

func runPlanShow(args []string, stdout, stderr io.Writer) int {
	name := "specctl plan show"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "text", "text, json or yaml")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "%s: one plan name is required\n", name)
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	subject, err := storedPlan(ctx, client, options.namespace, positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	if subject == nil {
		fmt.Fprintf(stderr, "%s: no plan %q\n", name, positional[0])
		return exitError
	}
	if err := renderPlan(stdout, *subject, *output); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitUsage
	}
	return exitOK
}

func runPlanApprove(args []string, stdout, stderr io.Writer) int {
	name := "specctl plan approve"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	by := fs.String("by", "", "who approves the plan; $USER, else operator")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "%s: one plan name is required\n", name)
		return exitUsage
	}

	who := *by
	if who == "" {
		who = os.Getenv("USER")
	}
	if who == "" {
		who = "operator"
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	stored, err := storedPlan(ctx, client, options.namespace, positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	if stored == nil {
		fmt.Fprintf(stderr, "%s: no plan %q\n", name, positional[0])
		return exitError
	}

	namespace := options.namespace
	if namespace == "" {
		namespace = stored.Namespace
	}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.PlanKind,
		"metadata":   map[string]any{"name": stored.Name, "namespace": namespace},
		"spec":       map[string]any{"approved": true, "approvedBy": who},
	}}
	if _, err := client.ServerSideApply(ctx, object, "specctl-approve"); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	fmt.Fprintf(stdout, "plan %s approved by %s\n", stored.Name, who)
	return exitOK
}

func runPlanPreview(args []string, stdout, stderr io.Writer) int {
	name := "specctl plan preview"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "%s: one plan name is required\n", name)
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	subject, err := storedPlan(ctx, client, options.namespace, positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	if subject == nil {
		fmt.Fprintf(stderr, "%s: no plan %q\n", name, positional[0])
		return exitError
	}

	byContext := map[string]*spec.SystemContext{}
	for _, item := range subject.Spec.Items {
		fmt.Fprintf(stdout, "%s:\n", item.ID)
		for _, edit := range item.Edits {
			contextName := resolveContextName(edit.Context)
			systemContext, ok := byContext[contextName]
			if !ok {
				object, err := client.Get(ctx, specapi.SystemContextGVR, options.namespace, contextName)
				if err != nil {
					if !kcpclient.IsNotFound(err) {
						fmt.Fprintf(stderr, "%s: %v\n", name, err)
						return exitError
					}
				} else if typed, err := kcpclient.Typed(object); err != nil {
					fmt.Fprintf(stderr, "%s: %v\n", name, err)
					return exitError
				} else if found, ok := typed.(*spec.SystemContext); ok {
					systemContext = found
				}
				byContext[contextName] = systemContext
			}
			if systemContext == nil {
				fmt.Fprintf(stdout, "  %s: no such context yet\n", edit.Context)
				continue
			}
			fmt.Fprintf(stdout, "  %s\n", plan.Preview(systemContext.Spec, edit))
		}
	}
	return exitOK
}

// renderPlans writes the plan list: one row per plan, the done count over the
// spec items whose status is Done.
func renderPlans(out io.Writer, subjects []spec.Plan, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(subjects, "", "  ")
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
	fmt.Fprintln(table, "NAME\tPHASE\tAPPROVED\tDONE\tTITLE")
	for _, subject := range subjects {
		done := 0
		for _, item := range subject.Spec.Items {
			if status, ok := subject.ItemStatus(item.ID); ok && status.Phase == plan.ItemDone {
				done++
			}
		}
		approved := "no"
		if subject.Spec.Approved {
			approved = "yes"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%d/%d\t%s\n", subject.Name, subject.Status.Phase, approved, done, len(subject.Spec.Items), subject.Spec.Title)
	}
	return table.Flush()
}

// renderPlan writes one plan: the header, then one row per spec item with its
// acceptance results under it.
func renderPlan(out io.Writer, subject spec.Plan, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(subject, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
		return nil
	case "yaml":
		data, err := yaml.Marshal(subject)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "text", "":
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
	header := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintf(header, "name\t%s\n", subject.Name)
	fmt.Fprintf(header, "title\t%s\n", subject.Spec.Title)
	fmt.Fprintf(header, "repository\t%s\n", subject.Spec.Repository)
	fmt.Fprintf(header, "phase\t%s\n", subject.Status.Phase)
	fmt.Fprintf(header, "approved by\t%s\n", subject.Spec.ApprovedBy)
	if err := header.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(out)
	renderPlanItems(out, subject)
	return nil
}

func renderPlanItems(out io.Writer, subject spec.Plan) {
	header := []string{"ID", "PHASE", "COMMIT", "MESSAGE"}
	rows := [][]string{}
	for _, item := range subject.Spec.Items {
		status, ok := subject.ItemStatus(item.ID)
		phase := "-"
		if ok && status.Phase != "" {
			phase = status.Phase
		}
		rows = append(rows, []string{item.ID, phase, planCommit(status.Commit), status.Message})
	}
	widths := make([]int, len(header))
	for index, cell := range header {
		widths[index] = len(cell)
	}
	for _, row := range rows {
		for index, cell := range row {
			if len(cell) > widths[index] {
				widths[index] = len(cell)
			}
		}
	}
	write := func(row []string) {
		cells := make([]string, len(row))
		for index, cell := range row {
			cells[index] = fmt.Sprintf("%-*s", widths[index], cell)
		}
		fmt.Fprintln(out, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
	write(header)
	for index, row := range rows {
		write(row)
		status, _ := subject.ItemStatus(subject.Spec.Items[index].ID)
		for _, result := range status.Acceptance {
			verdict := "failed"
			if result.Passed {
				verdict = "passed"
			}
			fmt.Fprintf(out, "  %s %s exit %d\n", result.Name, verdict, result.ExitCode)
		}
	}
}

// planCommit is the first 12 characters of the commit an item landed at.
func planCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
