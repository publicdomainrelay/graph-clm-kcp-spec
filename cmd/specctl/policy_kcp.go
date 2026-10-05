package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policykcp"
)

func runPolicyApply(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	files := stringsFlag{}
	fs.Var(&files, "f", "Gatekeeper manifest to apply, - for stdin; repeatable")
	library := fs.String("library", "", "policy directory to apply")
	prune := fs.Bool("prune", false, "delete the templates and constraints kcp holds and the library does not")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if len(files) == 0 && *library == "" {
		fmt.Fprintln(stderr, "specctl policy apply: -f or --library is required")
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy apply: %v\n", err)
		return exitError
	}

	loaded := policy.Library{}
	if *library != "" {
		var err error
		loaded, err = policyeval.Load(*library)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy apply: %v\n", err)
			return exitError
		}
	}
	for _, file := range files {
		data, err := readFile(file)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy apply: %v\n", err)
			return exitError
		}
		documents, err := loadPolicyDocuments(data)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy apply: %s: %v\n", file, err)
			return exitError
		}
		mergeLibrary(&loaded, documents)
	}
	loaded.Sort()

	existing, err := policykcp.Read(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy apply: %v\n", err)
		return exitError
	}
	for index := range loaded.Constraints {
		if loaded.Constraints[index].Template != "" {
			continue
		}
		if template, ok := loaded.TemplateOfKind(loaded.Constraints[index].Kind); ok {
			loaded.Constraints[index].Template = template.Name
			continue
		}
		if template, ok := existing.TemplateOfKind(loaded.Constraints[index].Kind); ok {
			loaded.Constraints[index].Template = template.Name
			if *library == "" {
				loaded.Templates = append(loaded.Templates, template)
			}
		}
	}

	engine, err := policyeval.NewEngine(ctx, loaded, nil)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy apply: %v\n", err)
		return exitError
	}
	if err := policykcp.Apply(ctx, client, loaded, policykcp.ApplyOptions{Prune: *prune, CRDs: engine}); err != nil {
		fmt.Fprintf(stderr, "specctl policy apply: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "%d template(s), %d constraint(s) applied to %s\n",
		len(loaded.Templates), len(loaded.Constraints), options.workspace)
	return exitOK
}

func loadPolicyDocuments(data []byte) (policy.Library, error) {
	library := policy.Library{}
	objects, err := kcpclient.Decode(data)
	if err != nil {
		return library, err
	}
	for _, object := range objects {
		switch {
		case object.GetKind() == policy.ConstraintTemplateKind:
			template, err := policykcp.ParseTemplateObject(object)
			if err != nil {
				return library, err
			}
			library.Templates = append(library.Templates, template)
		case strings.HasSuffix(object.GetAPIVersion(), policy.ConstraintGroup+"/"+policy.ConstraintVersion) ||
			strings.HasPrefix(object.GetAPIVersion(), policy.ConstraintGroup+"/"):
			constraint, err := policykcp.ParseConstraintObject(object, "")
			if err != nil {
				return library, err
			}
			library.Constraints = append(library.Constraints, constraint)
		default:
			return library, fmt.Errorf("policykcp: %s %s is neither a ConstraintTemplate nor a constraint",
				object.GetAPIVersion(), object.GetKind())
		}
	}
	return library, nil
}

func mergeLibrary(into *policy.Library, from policy.Library) {
	byName := map[string]int{}
	for index, template := range into.Templates {
		byName[template.Name] = index
	}
	for _, template := range from.Templates {
		if index, ok := byName[template.Name]; ok {
			into.Templates[index] = template
			continue
		}
		byName[template.Name] = len(into.Templates)
		into.Templates = append(into.Templates, template)
	}
	byConstraint := map[string]int{}
	for index, constraint := range into.Constraints {
		byConstraint[constraint.Name] = index
	}
	for _, constraint := range from.Constraints {
		if index, ok := byConstraint[constraint.Name]; ok {
			into.Constraints[index] = constraint
			continue
		}
		byConstraint[constraint.Name] = len(into.Constraints)
		into.Constraints = append(into.Constraints, constraint)
	}
}

func runPolicyLs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "table", "output format: table, json or name")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy ls: %v\n", err)
		return exitError
	}
	library, err := policykcp.Read(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy ls: %v\n", err)
		return exitError
	}
	if *output == "json" {
		encoded, err := json.MarshalIndent(library, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy ls: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
		return exitOK
	}
	if *output == "name" {
		for _, template := range library.Templates {
			fmt.Fprintf(stdout, "constrainttemplate/%s\n", template.Name)
		}
		for _, constraint := range library.Constraints {
			fmt.Fprintf(stdout, "%s/%s\n", policy.ConstraintResource(constraint.Kind), constraint.Name)
		}
		return exitOK
	}
	fmt.Fprintf(stdout, "%-32s %-24s %-8s %s\n", "TEMPLATE", "KIND", "LEVEL", "CONSTRAINTS")
	for _, template := range library.Templates {
		names := []string{}
		for _, constraint := range library.ConstraintsFor(template) {
			names = append(names, constraint.Name+"("+string(constraint.Enforcement)+")")
		}
		sort.Strings(names)
		fmt.Fprintf(stdout, "%-32s %-24s %-8s %s\n",
			template.Name, template.Kind, template.Level, strings.Join(names, ", "))
	}
	changes, err := listPolicyChanges(ctx, client, options.namespace, "")
	if err == nil && len(changes) > 0 {
		fmt.Fprintln(stdout)
		printPolicyChanges(stdout, stderr, changes, "table")
	}
	return exitOK
}

func runPolicyReport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "repository name")
	output := fs.String("o", "text", "output format: text or json")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *repository == "" {
		fmt.Fprintln(stderr, "specctl policy report: --repo is required")
		return exitUsage
	}
	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy report: %v\n", err)
		return exitError
	}
	object, err := client.Get(ctx, specapi.RepositoryGVR, options.namespace, *repository)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy report: %v\n", err)
		return exitError
	}
	status, found, err := unstructured.NestedMap(object.Object, "status", "policy")
	if err != nil || !found {
		fmt.Fprintf(stdout, "%s has no policy status yet\n", *repository)
		return exitOK
	}
	if *output == "json" {
		encoded, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy report: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
		return exitOK
	}
	totals, _ := status["totals"].(map[string]any)
	fmt.Fprintf(stdout, "repository: %s  policyCommit: %s  evaluatedCommit: %s\n",
		*repository, shortCommit(stringOf(status["policyCommit"])), shortCommit(stringOf(status["evaluatedCommit"])))
	fmt.Fprintf(stdout, "totals: deny %s, warn %s, dryrun %s\n",
		numberOf(totals["deny"]), numberOf(totals["warn"]), numberOf(totals["dryrun"]))
	if message := stringOf(status["message"]); message != "" {
		fmt.Fprintf(stdout, "message: %s\n", message)
	}
	violations, _ := status["violations"].([]any)
	if len(violations) == 0 {
		fmt.Fprintln(stdout, "clean")
		return exitOK
	}
	fmt.Fprintln(stdout)
	for _, raw := range violations {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		location := stringOf(entry["file"])
		if line := numberOf(entry["line"]); line != "" {
			location += ":" + line
		}
		fmt.Fprintf(stdout, "%-8s %-28s %s %s\n", stringOf(entry["enforcementAction"]),
			stringOf(entry["constraint"]), stringOf(entry["object"]), location)
		fmt.Fprintf(stdout, "         %s\n", stringOf(entry["msg"]))
	}
	return exitOK
}

func runPolicyRestore(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := addPolicyTargetFlags(fs)
	prune := fs.Bool("prune", true, "delete the templates and constraints kcp holds and the branch does not")
	options := addGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	target.resolveRepo()
	if target.repo == "" {
		fmt.Fprintln(stderr, "specctl policy restore: --repo is required")
		return exitUsage
	}
	ctx := context.Background()
	store := oagit.Store{Repo: target.path}
	library, commit, err := policygit.Read(ctx, store, target.ref())
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy restore: %v\n", err)
		return exitError
	}
	if len(library.Templates) == 0 && len(library.Constraints) == 0 {
		fmt.Fprintf(stderr, "specctl policy restore: %s holds no policy\n", target.ref())
		return exitError
	}
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy restore: %v\n", err)
		return exitError
	}
	engine, err := policyeval.NewEngine(ctx, library, nil)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy restore: %v\n", err)
		return exitError
	}
	if err := policykcp.Apply(ctx, client, library, policykcp.ApplyOptions{Prune: *prune, CRDs: engine}); err != nil {
		fmt.Fprintf(stderr, "specctl policy restore: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "restored %d template(s) and %d constraint(s) from %s at %s\n",
		len(library.Templates), len(library.Constraints), target.ref(), shortCommit(commit))
	return exitOK
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

func numberOf(value any) string {
	switch typed := value.(type) {
	case nil:
		return "0"
	case float64:
		return fmt.Sprintf("%d", int(typed))
	case int64:
		return fmt.Sprintf("%d", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case string:
		return typed
	}
	return "0"
}
