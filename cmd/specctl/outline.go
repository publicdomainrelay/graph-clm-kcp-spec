package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
)

type outlineEntry struct {
	Name string `json:"name"`

	Upstream string `json:"upstream,omitempty"`

	Overlay []string `json:"overlay,omitempty"`

	Orchestrator string `json:"orchestrator,omitempty"`

	Intent string `json:"intent,omitempty"`

	Requirements int `json:"requirements"`

	Interfaces []string `json:"interfaces,omitempty"`

	Files []string `json:"files,omitempty"`

	Conditions map[string]string `json:"conditions,omitempty"`
}

func runArch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "outline" {
		fmt.Fprintln(stderr, "usage: specctl arch outline [--repository <name>] [-o text|json]")
		return exitUsage
	}
	fs := flag.NewFlagSet("specctl arch outline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repository", "", "Repository whose contexts to list; default is this repository's session, else every one")
	output := fs.String("o", "text", "text or json")
	options := addGlobals(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return exitUsage
	}
	name := *repository
	if name == "" {
		if record, ok := session.ForDir("."); ok {
			name = record.Repository
		}
	}
	client, err := options.client()
	if err != nil {
		fmt.Fprintf(stderr, "specctl arch outline: %v\n", err)
		return exitError
	}
	listed, err := client.List(context.Background(), specapi.SystemContextGVR, options.namespace)
	if err != nil {
		fmt.Fprintf(stderr, "specctl arch outline: %v\n", err)
		return exitError
	}
	entries := []outlineEntry{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			fmt.Fprintf(stderr, "specctl arch outline: %v\n", err)
			return exitError
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok || (name != "" && systemContext.Spec.Repository != name) {
			continue
		}
		entries = append(entries, outlineOf(*systemContext))
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name < entries[right].Name })
	if *output == "json" {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl arch outline: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(data))
		return exitOK
	}
	for _, entry := range entries {
		fmt.Fprintf(stdout, "%s  upstream=%s", entry.Name, valueOr(entry.Upstream, "-"))
		if len(entry.Overlay) > 0 {
			fmt.Fprintf(stdout, " overlay=%s", strings.Join(entry.Overlay, ","))
		}
		if entry.Orchestrator != "" {
			fmt.Fprintf(stdout, " orchestrator=%s", entry.Orchestrator)
		}
		fmt.Fprintf(stdout, " requirements=%d", entry.Requirements)
		for _, condition := range []string{specapi.ConditionCodeSynced, specapi.ConditionDrifted} {
			if value, ok := entry.Conditions[condition]; ok {
				fmt.Fprintf(stdout, " %s=%s", condition, value)
			}
		}
		fmt.Fprintln(stdout)
		if entry.Intent != "" {
			fmt.Fprintf(stdout, "  intent: %s\n", firstSentence(entry.Intent, 200))
		}
		if len(entry.Interfaces) > 0 {
			fmt.Fprintf(stdout, "  interfaces: %s\n", strings.Join(entry.Interfaces, ", "))
		}
		if len(entry.Files) > 0 {
			fmt.Fprintf(stdout, "  files: %s\n", strings.Join(entry.Files, ", "))
		}
	}
	return exitOK
}

func outlineOf(systemContext spec.SystemContext) outlineEntry {
	declared := spec.Canonicalize(systemContext.Spec)
	entry := outlineEntry{
		Name:         systemContext.Name,
		Upstream:     declared.Upstream,
		Overlay:      declared.Overlay,
		Orchestrator: declared.Orchestrator,
		Intent:       declared.Intent,
		Requirements: len(declared.Requirements),
		Files:        systemContext.Status.Observed.Files,
		Conditions:   map[string]string{},
	}
	for _, declaredInterface := range declared.Interfaces {
		entry.Interfaces = append(entry.Interfaces, declaredInterface.Name)
	}
	for _, condition := range systemContext.Status.Conditions {
		entry.Conditions[condition.Type] = string(condition.Status)
	}
	if len(entry.Conditions) == 0 {
		entry.Conditions = nil
	}
	return entry
}

func firstSentence(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if end := strings.Index(text, ". "); end > 0 && end < limit {
		return text[:end+1]
	}
	if len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}
