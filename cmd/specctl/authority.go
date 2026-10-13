package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

type declaredView struct {
	Context string `json:"context"`

	Declared []string `json:"declared"`

	Pending []string `json:"pending,omitempty"`
}

func runArchAuthority(verb string, args []string, stdout, stderr io.Writer) int {
	name := "specctl arch " + verb
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	units := stringsFlag{}
	fs.Var(&units, "unit", "a spec unit: intent, requirement/<id>, interface/<name> or interaction/<id>")
	all := fs.Bool("all", false, "release every declared unit")
	output := fs.String("o", "text", "text or json")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "%s: one context name is required\n", name)
		return exitUsage
	}
	for _, unit := range units {
		if !authority.ValidUnit(unit) {
			fmt.Fprintf(stderr, "%s: %q is not a spec unit\n", name, unit)
			return exitUsage
		}
	}
	switch {
	case verb == "declare" && len(units) == 0:
		fmt.Fprintf(stderr, "%s: at least one --unit is required\n", name)
		return exitUsage
	case verb == "release" && len(units) == 0 && !*all:
		fmt.Fprintf(stderr, "%s: --unit or --all is required\n", name)
		return exitUsage
	}

	ctx := context.Background()
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	contextName := resolveContextName(positional[0])
	object, err := client.Get(ctx, specapi.SystemContextGVR, options.namespace, contextName)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	systemContext, ok := typed.(*spec.SystemContext)
	if !ok {
		fmt.Fprintf(stderr, "%s: %s is not a SystemContext\n", name, contextName)
		return exitError
	}
	recorded, err := authority.Parse(systemContext.Annotations[specapi.DeclaredAnnotation])
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}

	if verb == "declared" {
		view := declaredView{Context: contextName, Declared: recorded.Sorted()}
		if realized := systemContext.Status.RealizedSpec; realized != nil {
			view.Pending = authority.Changed(*realized, systemContext.Spec).Without(recorded).Sorted()
		}
		if *output == "json" {
			data, err := json.MarshalIndent(view, "", "  ")
			if err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", name, err)
				return exitError
			}
			fmt.Fprintln(stdout, string(data))
			return exitOK
		}
		for _, unit := range view.Declared {
			fmt.Fprintf(stdout, "declared  %s\n", unit)
		}
		for _, unit := range view.Pending {
			fmt.Fprintf(stdout, "pending   %s\n", unit)
		}
		return exitOK
	}

	next := recorded.Union(authority.NewSet(units...))
	if verb == "release" {
		next = recorded.Without(authority.NewSet(units...))
		if *all {
			next = authority.Set{}
		}
	}
	if next.Equal(recorded) {
		fmt.Fprintf(stdout, "%s: unchanged\n", contextName)
		return exitOK
	}
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	if formatted := authority.Format(next); formatted != "" {
		annotations[specapi.DeclaredAnnotation] = formatted
	} else {
		delete(annotations, specapi.DeclaredAnnotation)
	}
	object.SetAnnotations(annotations)
	if _, err := client.Update(ctx, object); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s: declared %d unit(s)\n", contextName, len(next))
	return exitOK
}

type proposalView struct {
	Change string `json:"change"`

	Context string `json:"context"`

	spec.Proposal
}

func runArchProposals(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl arch proposals", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "text", "text or json")
	options := addGlobals(fs)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, "specctl arch proposals: at most one context name")
		return exitUsage
	}
	only := ""
	if len(positional) == 1 {
		only = resolveContextName(positional[0])
	}
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl arch proposals: %v\n", err)
		return exitError
	}
	listed, err := client.List(context.Background(), specapi.SpecChangeGVR, options.namespace)
	if err != nil {
		fmt.Fprintf(stderr, "specctl arch proposals: %v\n", err)
		return exitError
	}
	views := []proposalView{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			fmt.Fprintf(stderr, "specctl arch proposals: %v\n", err)
			return exitError
		}
		change, ok := typed.(*spec.SpecChange)
		if !ok || (only != "" && change.Spec.SystemContext != only) {
			continue
		}
		for _, proposal := range change.Status.Proposals {
			views = append(views, proposalView{Change: change.Name, Context: change.Spec.SystemContext, Proposal: proposal})
		}
	}
	sort.SliceStable(views, func(left, right int) bool {
		if views[left].Context != views[right].Context {
			return views[left].Context < views[right].Context
		}
		return views[left].Change < views[right].Change
	})
	if *output == "json" {
		data, err := json.MarshalIndent(views, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl arch proposals: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(data))
		return exitOK
	}
	for _, view := range views {
		fmt.Fprintf(stdout, "%s  %s  %s\n", view.Context, view.Unit, view.Change)
		fmt.Fprintf(stdout, "  current:  %s\n", valueOr(view.Current, "(absent)"))
		fmt.Fprintf(stdout, "  proposed: %s\n", valueOr(view.Proposed, "(absent)"))
	}
	if len(views) > 0 {
		fmt.Fprintln(stdout, "release a unit with specctl arch release <context> --unit <unit> to let the next summary write it")
	}
	return exitOK
}
