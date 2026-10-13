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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
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
	case "items":
		return runPlanItems(rest, stdout, stderr)
	case "status":
		return runPlanStatus(rest, stdout, stderr)
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
	allInstances := fs.Bool("all-instances", false, "apply the plans to every instance whose kcp is ready")
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

	if *allInstances {
		return applyPlanInstances(subjects, options, stdout, stderr)
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

// instanceClient builds the client of an instance the caller does not stand
// in: the record's admin kubeconfig, workspace and namespace.
func instanceClient(record session.Record) (*kcpclient.Client, error) {
	return kcpclient.New(kcpclient.Options{
		Kubeconfig: record.AdminKubeconfig,
		Workspace:  record.Workspace,
		Namespace:  record.Namespace,
	})
}

// readyInstances is every instance on this machine whose kcp serves requests,
// as specctl ls checks it, in the order session.List returns them.
func readyInstances() ([]session.Record, error) {
	records, err := session.List()
	if err != nil {
		return nil, err
	}
	out := []session.Record{}
	for _, record := range records {
		if kcpproc.Ready(instanceOf(record)) {
			out = append(out, record)
		}
	}
	return out, nil
}

// instanceLabel names an instance the way a result line does:
// <repository>@<branch>.
func instanceLabel(record session.Record) string {
	branch := record.Branch
	if branch == "" {
		if current, err := session.CurrentBranch(record.Repo); err == nil {
			branch = current
		}
	}
	return record.Repository + "@" + branch
}

// applyPlanInstances writes the plans to every instance whose kcp is ready:
// one instance failing is reported and the rest still get the plans.
func applyPlanInstances(subjects []spec.Plan, options *globals, stdout, stderr io.Writer) int {
	name := "specctl plan apply"
	ctx := context.Background()
	records, err := readyInstances()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	if len(records) == 0 {
		fmt.Fprintf(stderr, "%s: no instance has a ready kcp\n", name)
		return exitError
	}
	failed := false
	for _, record := range records {
		label := instanceLabel(record)
		client, err := instanceClient(record)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %s: %v\n", name, label, err)
			failed = true
			continue
		}
		for _, subject := range subjects {
			if !applyPlanInstance(ctx, client, subject, options.namespace, label, stdout, stderr) {
				failed = true
			}
		}
	}
	if failed {
		return exitError
	}
	return exitOK
}

// applyPlanInstance writes one plan to one instance and reports whether it
// landed: it resets the approval of a stored plan whose items changed, like
// the apply that writes the instance the caller stands in.
func applyPlanInstance(ctx context.Context, client *kcpclient.Client, subject spec.Plan, namespace, label string, stdout, stderr io.Writer) bool {
	planName := subject.Name
	stored, err := storedPlan(ctx, client, namespace, planName)
	if err != nil {
		fmt.Fprintf(stderr, "specctl plan apply: %s: plan %s: %v\n", label, planName, err)
		return false
	}
	reset := approvalReset(stored, subject)
	subject.SetDefaults()
	if subject.Namespace == "" {
		subject.Namespace = namespace
	}
	object, err := planObject(subject)
	if err != nil {
		fmt.Fprintf(stderr, "specctl plan apply: %s: plan %s: %v\n", label, planName, err)
		return false
	}
	if _, err := client.ServerSideApply(ctx, object, "specctl-plan"); err != nil {
		fmt.Fprintf(stderr, "specctl plan apply: %s: plan %s: %v\n", label, planName, err)
		return false
	}
	if reset {
		fmt.Fprintln(stdout, "approval reset: the items changed")
	}
	fmt.Fprintf(stdout, "%s: plan %s applied\n", label, planName)
	return true
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

// planContextItem is one plan item that touches a context: the plan it belongs
// to, the item itself, its phase and the edits of that context it carries.
type planContextItem struct {
	Plan    string `json:"plan"`
	Item    string `json:"item"`
	Phase   string `json:"phase"`
	Summary string `json:"summary,omitempty"`

	// Previews is one line per edit of the context, filled against the spec
	// kcp holds; empty until the caller resolves the context.
	Edits    []spec.PlanEdit `json:"-"`
	Previews []string        `json:"previews,omitempty"`
}

// selectPlanItems picks the work an instance still owes a context: the items
// of every plan that is neither Complete nor Failed, whose own phase is
// neither Done nor Failed, and that carry an edit naming the context. Names
// are resolved the way the rest of the package resolves them, plans come in
// name order and items keep the spec order of their plan.
func selectPlanItems(subjects []spec.Plan, contextName string) []planContextItem {
	ordered := append([]spec.Plan{}, subjects...)
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].Name < ordered[right].Name })
	out := []planContextItem{}
	for _, subject := range ordered {
		switch subject.Status.Phase {
		case plan.PhaseComplete, plan.PhaseFailed:
			continue
		}
		for _, item := range subject.Spec.Items {
			status, _ := subject.ItemStatus(item.ID)
			switch status.Phase {
			case plan.ItemDone, plan.ItemFailed:
				continue
			}
			edits := []spec.PlanEdit{}
			for _, edit := range item.Edits {
				if resolveContextName(edit.Context) == contextName {
					edits = append(edits, edit)
				}
			}
			if len(edits) == 0 {
				continue
			}
			phase := status.Phase
			if phase == "" {
				phase = plan.ItemPending
			}
			out = append(out, planContextItem{
				Plan:    subject.Name,
				Item:    item.ID,
				Phase:   phase,
				Summary: item.Summary,
				Edits:   edits,
			})
		}
	}
	return out
}

// renderPlanContextItems writes the items of one context: the plan and item,
// the phase, the summary, then one indented preview line per edit.
func renderPlanContextItems(out io.Writer, views []planContextItem, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(views, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
		return nil
	case "text", "":
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
	for _, view := range views {
		line := fmt.Sprintf("- %s/%s [%s]", view.Plan, view.Item, view.Phase)
		if view.Summary != "" {
			line += " " + view.Summary
		}
		fmt.Fprintln(out, line)
		for _, preview := range view.Previews {
			fmt.Fprintf(out, "    %s\n", preview)
		}
	}
	return nil
}

func runPlanItems(args []string, stdout, stderr io.Writer) int {
	name := "specctl plan items"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	contextName := fs.String("context", "", "the SystemContext whose plan items to list")
	output := fs.String("o", "text", "text or json")
	options := addGlobalsExcept(fs, "context")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", name, positional[0])
		return exitUsage
	}
	if *contextName == "" {
		fmt.Fprintf(stderr, "%s: --context is required\n", name)
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
	resolved := resolveContextName(*contextName)
	views := selectPlanItems(subjects, resolved)

	// A context kcp does not hold yet renders every edit against the empty
	// spec, so the caller still sees what the items would add.
	contextSpec := spec.SystemContextSpec{}
	object, err := client.Get(ctx, specapi.SystemContextGVR, options.namespace, resolved)
	if err != nil {
		if !kcpclient.IsNotFound(err) {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
	} else if typed, err := kcpclient.Typed(object); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	} else if found, ok := typed.(*spec.SystemContext); ok {
		contextSpec = found.Spec
	}
	for index := range views {
		for _, edit := range views[index].Edits {
			views[index].Previews = append(views[index].Previews, plan.Preview(contextSpec, edit))
		}
	}
	if err := renderPlanContextItems(stdout, views, *output); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitUsage
	}
	return exitOK
}

// instancePlan is one instance's view of a plan: the record the plan was read
// from and the plan itself.
type instancePlan struct {
	Record session.Record
	Plan   spec.Plan
}

// planStatusRow is one row of "plan status": an item as one instance shows
// it, with the repository of the instance that owns it.
type planStatusRow struct {
	Item       string `json:"item"`
	Repository string `json:"repository"`
	Phase      string `json:"phase"`
	Instance   string `json:"instance"`
	Commit     string `json:"commit,omitempty"`
	Message    string `json:"message,omitempty"`
}

// planStatusWrite is one status patch the merge asks for: the instance to
// patch, the plan it holds and the status items to write there.
type planStatusWrite struct {
	Instance  session.Record
	Namespace string
	Plan      string
	Items     []spec.PlanItemStatus
}

// planStatusKey names an instance for the map that finds its client, so two
// instances that share a branch stay apart.
func planStatusKey(record session.Record) string {
	return record.Repo + "|" + record.Repository + "|" + record.Branch
}

// mergePlanStatus merges the views of a plan into one row per item per
// instance and the status writes the merge asks for. The phase of an item is
// the one the instance whose repository owns it recorded (item.Repository,
// else the plan's repository), Elsewhere in the instances that do not own it,
// and what an instance recorded when no view owns the item at all; an item
// nothing owns and nothing recorded shows "-". Every item its owner finished
// or failed is written into the status of the instances that show it
// Elsewhere, so a dependsOn across repositories resolves.
func mergePlanStatus(views []instancePlan) ([]planStatusRow, []planStatusWrite) {
	ordered := append([]instancePlan{}, views...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].Record.Repository != ordered[right].Record.Repository {
			return ordered[left].Record.Repository < ordered[right].Record.Repository
		}
		return ordered[left].Record.Branch < ordered[right].Record.Branch
	})

	items := []spec.PlanItem{}
	owner := map[string]string{}
	for _, view := range ordered {
		for _, item := range view.Plan.Spec.Items {
			if _, seen := owner[item.ID]; !seen {
				items = append(items, item)
			}
			if owner[item.ID] != "" {
				continue
			}
			repository := item.Repository
			if repository == "" {
				repository = view.Plan.Spec.Repository
			}
			if repository == "" {
				repository = view.Record.Repository
			}
			owner[item.ID] = repository
		}
	}

	carries := func(view instancePlan, id string) bool {
		for _, item := range view.Plan.Spec.Items {
			if item.ID == id {
				return true
			}
		}
		return false
	}
	viewOf := func(repository string) int {
		if repository == "" {
			return -1
		}
		for index, view := range ordered {
			if view.Record.Repository == repository {
				return index
			}
		}
		return -1
	}

	rows := []planStatusRow{}
	for _, item := range items {
		repository := owner[item.ID]
		index := viewOf(repository)
		shown := repository
		if shown == "" {
			shown = "-"
		}
		for at, view := range ordered {
			if !carries(view, item.ID) {
				continue
			}
			status, _ := view.Plan.ItemStatus(item.ID)
			phase := status.Phase
			if index >= 0 && at != index {
				phase = plan.ItemElsewhere
			}
			if phase == "" {
				phase = "-"
			}
			rows = append(rows, planStatusRow{
				Item:       item.ID,
				Repository: shown,
				Phase:      phase,
				Instance:   view.Record.Repository,
				Commit:     planCommit(status.Commit),
				Message:    status.Message,
			})
		}
	}

	writes := []planStatusWrite{}
	for _, item := range items {
		index := viewOf(owner[item.ID])
		if index < 0 {
			continue
		}
		owned, _ := ordered[index].Plan.ItemStatus(item.ID)
		if owned.Phase != plan.ItemDone && owned.Phase != plan.ItemFailed {
			continue
		}
		for at, view := range ordered {
			if at == index || !carries(view, item.ID) {
				continue
			}
			recorded, _ := view.Plan.ItemStatus(item.ID)
			if recorded.Phase != plan.ItemElsewhere {
				continue
			}
			writes = append(writes, planStatusWrite{
				Instance:  view.Record,
				Namespace: view.Plan.Namespace,
				Plan:      view.Plan.Name,
				Items: mergePlanItemStatus(view.Plan.Status.Items, spec.PlanItemStatus{
					ID:      item.ID,
					Phase:   owned.Phase,
					Commit:  owned.Commit,
					Message: owned.Phase + " in " + owner[item.ID],
				}),
			})
		}
	}
	return rows, writes
}

// mergePlanItemStatus replaces the entry of one item in a plan status the way
// the merge patches it: the phase, the commit and the message change, the
// other entries and the other fields of the entry stay.
func mergePlanItemStatus(items []spec.PlanItemStatus, set spec.PlanItemStatus) []spec.PlanItemStatus {
	out := append([]spec.PlanItemStatus{}, items...)
	for index := range out {
		if out[index].ID != set.ID {
			continue
		}
		out[index].Phase = set.Phase
		out[index].Commit = set.Commit
		out[index].Message = set.Message
		return out
	}
	return append(out, set)
}

// renderPlanStatus writes the merged rows: one line per item per instance.
func renderPlanStatus(out io.Writer, rows []planStatusRow, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(rows, "", "  ")
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
	fmt.Fprintln(table, "ITEM\tREPOSITORY\tPHASE\tINSTANCE\tCOMMIT\tMESSAGE")
	for _, row := range rows {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Item, row.Repository, row.Phase, row.Instance, row.Commit, row.Message)
	}
	return table.Flush()
}

// instancePlans reads one plan from the instance the caller stands in, or
// from every ready instance when all is set. Each view carries the record the
// plan came from and its client, and an instance that does not hold the plan
// is left out.
func instancePlans(ctx context.Context, planName string, options *globals, all bool) ([]instancePlan, []*kcpclient.Client, error) {
	if !all {
		client, err := options.client()
		if err != nil {
			return nil, nil, err
		}
		subject, err := storedPlan(ctx, client, options.namespace, planName)
		if err != nil {
			return nil, nil, err
		}
		if subject == nil {
			return nil, nil, nil
		}
		record := session.Record{Repository: subject.Spec.Repository}
		if local, ok := session.ForDir("."); ok {
			record = local
		}
		return []instancePlan{{Record: record, Plan: *subject}}, []*kcpclient.Client{client}, nil
	}

	records, err := readyInstances()
	if err != nil {
		return nil, nil, err
	}
	views := []instancePlan{}
	clients := []*kcpclient.Client{}
	for _, record := range records {
		client, err := instanceClient(record)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", instanceLabel(record), err)
		}
		namespace := record.Namespace
		if namespace == "" {
			namespace = options.namespace
		}
		subject, err := storedPlan(ctx, client, namespace, planName)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", instanceLabel(record), err)
		}
		if subject == nil {
			continue
		}
		views = append(views, instancePlan{Record: record, Plan: *subject})
		clients = append(clients, client)
	}
	return views, clients, nil
}

// writePlanStatus patches the status the merge asks for, one plan per
// instance: a Done or Failed item of its owner lands in the status of the
// instances that show it Elsewhere.
func writePlanStatus(ctx context.Context, views []instancePlan, clients []*kcpclient.Client, writes []planStatusWrite) error {
	byInstance := map[string]*kcpclient.Client{}
	for index, view := range views {
		byInstance[planStatusKey(view.Record)] = clients[index]
	}
	for _, write := range writes {
		client := byInstance[planStatusKey(write.Instance)]
		if client == nil {
			continue
		}
		status := map[string]any{"items": write.Items}
		if _, err := client.PatchStatus(ctx, specapi.PlanGVR, write.Namespace, write.Plan, status); err != nil {
			return fmt.Errorf("%s: plan %s: %w", instanceLabel(write.Instance), write.Plan, err)
		}
	}
	return nil
}

func runPlanStatus(args []string, stdout, stderr io.Writer) int {
	name := "specctl plan status"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "text", "text or json")
	allInstances := fs.Bool("all-instances", false, "read the plan from every instance whose kcp is ready")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "%s: one plan name is required\n", name)
		return exitUsage
	}
	planName := positional[0]

	ctx := context.Background()
	views, clients, err := instancePlans(ctx, planName, options, *allInstances)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	if len(views) == 0 {
		fmt.Fprintf(stderr, "%s: no plan %q\n", name, planName)
		return exitError
	}
	rows, writes := mergePlanStatus(views)
	if *allInstances {
		if err := writePlanStatus(ctx, views, clients, writes); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return exitError
		}
	}
	if err := renderPlanStatus(stdout, rows, *output); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitUsage
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
