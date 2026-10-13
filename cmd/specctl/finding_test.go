package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func TestFindingCommandName(t *testing.T) {
	if got := findingName("Add returns the wrong sign"); got != "manual-add-returns-the-wrong-sign" {
		t.Errorf("name = %q", got)
	}
	long := findingName(strings.Repeat("wide ", 20))
	if !strings.HasPrefix(long, "manual-") {
		t.Fatalf("name = %q", long)
	}
	if slug := strings.TrimPrefix(long, "manual-"); len(slug) > findingNameLimit || len(slug) < findingNameLimit-1 {
		t.Errorf("a long summary was not cut to %d characters: %q", findingNameLimit, long)
	}
	if strings.HasSuffix(long, "-") || strings.HasSuffix(long, ".") {
		t.Errorf("the cut left a trailing separator: %q", long)
	}
	if got := findingName("  ...  "); got != "manual-" {
		t.Errorf("an empty slug = %q", got)
	}
}

func TestFindingCommandSource(t *testing.T) {
	for _, wanted := range []string{"manual", "review", "proposal", "drift", "policy"} {
		got, err := findingSource(wanted)
		if err != nil || got != wanted {
			t.Errorf("findingSource(%q) = %q, %v", wanted, got, err)
		}
	}
	if got, err := findingSource(""); err != nil || got != "manual" {
		t.Errorf("an absent source = %q, %v, want manual", got, err)
	}
	if _, err := findingSource("rumour"); err == nil {
		t.Error("an unknown source was accepted")
	}
}

func fixtureFindings() []spec.Finding {
	build := func(name, state, source, summary string) spec.Finding {
		finding := spec.Finding{}
		finding.Name = name
		finding.Namespace = "default"
		finding.Spec = spec.FindingSpec{Repository: "hydradb", Source: source, Context: "calc", Unit: "r.add", Summary: summary}
		finding.Status = spec.FindingStatus{State: state, Plan: "p0011"}
		return finding
	}
	return []spec.Finding{
		build("a-open", "Open", "manual", "the open one"),
		build("b-planned", "Planned", "proposal", "the planned one"),
		build("c-resolved", "Resolved", "drift", "the resolved one"),
		build("d-dismissed", "Dismissed", "review", "the dismissed one"),
	}
}

func TestFindingCommandList(t *testing.T) {
	out := &bytes.Buffer{}
	if err := renderFindings(out, fixtureFindings(), false, "text"); err != nil {
		t.Fatalf("renderFindings: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines: %q", out.String())
	}
	if fields := strings.Fields(lines[0]); strings.Join(fields, " ") != "NAME STATE SOURCE CONTEXT UNIT PLAN SUMMARY" {
		t.Errorf("header: %q", lines[0])
	}
	for _, line := range lines[1:] {
		if strings.Contains(line, "c-resolved") || strings.Contains(line, "d-dismissed") {
			t.Errorf("a settled finding was listed without --all: %q", line)
		}
	}
	if fields := strings.Fields(lines[1]); strings.Join(fields, " ") != "a-open Open manual calc r.add p0011 the open one" {
		t.Errorf("row: %q", lines[1])
	}

	out = &bytes.Buffer{}
	if err := renderFindings(out, fixtureFindings(), true, "text"); err != nil {
		t.Fatalf("renderFindings --all: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 5 {
		t.Errorf("--all lines: %q", out.String())
	}

	out = &bytes.Buffer{}
	if err := renderFindings(out, fixtureFindings(), false, "json"); err != nil {
		t.Fatalf("renderFindings json: %v", err)
	}
	if !strings.Contains(out.String(), `"name": "a-open"`) || strings.Contains(out.String(), `"name": "c-resolved"`) {
		t.Errorf("json: %q", out.String())
	}
}

func TestFindingCommandSummaryCut(t *testing.T) {
	if got := findingSummary(strings.Repeat("x", 100)); len(got) != findingSummaryLimit {
		t.Errorf("a long summary was not cut to %d characters: %d", findingSummaryLimit, len(got))
	}
	if got := findingSummary("short"); got != "short" {
		t.Errorf("summary = %q", got)
	}
}

// buildVerbFlagSet builds one verb's flag set by running it with --help: every
// verb parses its flags before it does anything else, so the help path reaches
// the whole flag set and a flag it defines twice panics there. The panic is
// turned into a test failure, so a duplicate flag fails here instead of in a
// user's command.
func buildVerbFlagSet(t *testing.T, command, verb string) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := -1
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("specctl %s %s: %v", command, verb, recovered)
			}
		}()
		code = run([]string{command, verb, "--help"}, stdout, stderr)
	}()
	if code != exitUsage && code != exitOK {
		t.Errorf("specctl %s %s --help = %d, want %d or %d", command, verb, code, exitUsage, exitOK)
	}
}

func TestFindingCommandFlagSetsDefineEveryFlagOnce(t *testing.T) {
	for _, verb := range []string{"add", "ls", "dismiss"} {
		buildVerbFlagSet(t, "finding", verb)
	}
	for _, verb := range []string{"apply", "ls", "show", "approve", "preview", "items", "status"} {
		buildVerbFlagSet(t, "plan", verb)
	}
}
