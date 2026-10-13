package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/plan"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const twoPlanDocuments = `
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Plan
metadata:
  name: p0011
spec:
  repository: hydradb
  approved: true
  approvedBy: alice
  items:
    - id: A
---
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Plan
metadata:
  name: p0012
spec:
  approved: true
  approvedBy: bob
  items:
    - id: B
`

func TestReadPlans(t *testing.T) {
	plans, err := readPlans([]byte(twoPlanDocuments), "team", "session-repo")
	if err != nil {
		t.Fatalf("readPlans: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("documents: got %d plans, want 2", len(plans))
	}
	if plans[0].Name != "p0011" || plans[1].Name != "p0012" {
		t.Errorf("names: got %q and %q", plans[0].Name, plans[1].Name)
	}
	if plans[0].Namespace != "team" || plans[1].Namespace != "team" {
		t.Errorf("namespaces: got %q and %q, want team", plans[0].Namespace, plans[1].Namespace)
	}
	if plans[0].Spec.Repository != "hydradb" {
		t.Errorf("a named repository was replaced: %q", plans[0].Spec.Repository)
	}
	if plans[1].Spec.Repository != "session-repo" {
		t.Errorf("an empty repository was not defaulted: %q", plans[1].Spec.Repository)
	}
	for _, subject := range plans {
		if subject.Spec.Approved || subject.Spec.ApprovedBy != "" {
			t.Errorf("%s kept its approval: approved %v by %q", subject.Name, subject.Spec.Approved, subject.Spec.ApprovedBy)
		}
	}
	if _, err := readPlans([]byte("\n"), "team", ""); err == nil {
		t.Error("an empty stream was accepted")
	}
}

func fixturePlan(name string) spec.Plan {
	subject := spec.Plan{}
	subject.Name = name
	subject.Namespace = "default"
	subject.Spec = spec.PlanSpec{
		Title:      "plans as objects",
		Repository: "hydradb",
		Approved:   true,
		ApprovedBy: "alice",
		Items: []spec.PlanItem{
			{ID: "A", Edits: []spec.PlanEdit{{Context: "calc", Intent: "adds"}}},
			{ID: "B"},
		},
	}
	subject.Status = spec.PlanStatus{
		Phase: plan.PhaseRunning,
		Items: []spec.PlanItemStatus{{
			ID:      "A",
			Phase:   plan.ItemDone,
			Commit:  "0123456789abcdef",
			Message: "acceptance passed",
			Acceptance: []spec.AcceptanceResult{
				{Name: "test", ExitCode: 0, Passed: true},
				{Name: "vet", ExitCode: 1, Passed: false},
			},
		}},
	}
	return subject
}

func TestRenderPlans(t *testing.T) {
	out := &bytes.Buffer{}
	if err := renderPlans(out, []spec.Plan{fixturePlan("p0011")}, "text"); err != nil {
		t.Fatalf("renderPlans: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", out.String())
	}
	if fields := strings.Fields(lines[0]); strings.Join(fields, " ") != "NAME PHASE APPROVED DONE TITLE" {
		t.Errorf("header: %q", lines[0])
	}
	fields := strings.Fields(lines[1])
	want := []string{"p0011", plan.PhaseRunning, "yes", "1/2", "plans", "as", "objects"}
	if strings.Join(fields, " ") != strings.Join(want, " ") {
		t.Errorf("row: got %q, want %q", fields, want)
	}

	out = &bytes.Buffer{}
	if err := renderPlans(out, []spec.Plan{fixturePlan("p0011")}, "yaml"); err == nil {
		t.Error("ls accepted yaml")
	}
	out = &bytes.Buffer{}
	if err := renderPlans(out, []spec.Plan{fixturePlan("p0011")}, "json"); err != nil {
		t.Fatalf("renderPlans json: %v", err)
	}
	if !strings.Contains(out.String(), `"name": "p0011"`) {
		t.Errorf("json: %q", out.String())
	}
}

func TestRenderPlan(t *testing.T) {
	out := &bytes.Buffer{}
	if err := renderPlan(out, fixturePlan("p0011"), "text"); err != nil {
		t.Fatalf("renderPlan: %v", err)
	}
	text := out.String()
	for _, want := range []string{"name", "p0011", "plans as objects", "hydradb", plan.PhaseRunning, "alice"} {
		if !strings.Contains(text, want) {
			t.Errorf("header is missing %q in %q", want, text)
		}
	}
	if !strings.Contains(text, "ID") || !strings.Contains(text, "PHASE") || !strings.Contains(text, "COMMIT") || !strings.Contains(text, "MESSAGE") {
		t.Errorf("item header: %q", text)
	}
	if !strings.Contains(text, "0123456789ab") || strings.Contains(text, "0123456789abc") {
		t.Errorf("commit was not cut to 12 characters: %q", text)
	}
	for _, want := range []string{"  test passed exit 0", "  vet failed exit 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("acceptance line %q missing in %q", want, text)
		}
	}
	itemB := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "B ") {
			itemB = line
		}
	}
	if itemB == "" || !strings.Contains(itemB, "-") {
		t.Errorf("an item without status did not show a dash: %q", text)
	}

	out = &bytes.Buffer{}
	if err := renderPlan(out, fixturePlan("p0011"), "json"); err != nil {
		t.Fatalf("renderPlan json: %v", err)
	}
	if !strings.Contains(out.String(), `"status"`) {
		t.Errorf("json does not print the whole plan: %q", out.String())
	}
	out = &bytes.Buffer{}
	if err := renderPlan(out, fixturePlan("p0011"), "yaml"); err != nil {
		t.Fatalf("renderPlan yaml: %v", err)
	}
	if !strings.Contains(out.String(), "phase: "+plan.PhaseRunning) {
		t.Errorf("yaml does not print the whole plan: %q", out.String())
	}
}

func TestApprovalReset(t *testing.T) {
	applied := fixturePlan("p0011")
	same := fixturePlan("p0011")
	if approvalReset(&same, applied) {
		t.Error("identical items reset the approval")
	}
	differing := fixturePlan("p0011")
	differing.Spec.Items = append(differing.Spec.Items, spec.PlanItem{ID: "C"})
	if !approvalReset(&differing, applied) {
		t.Error("changed items kept the approval")
	}
	draft := fixturePlan("p0011")
	draft.Spec.Approved = false
	if approvalReset(&draft, applied) {
		t.Error("an unapproved plan was reset")
	}
	if approvalReset(nil, applied) {
		t.Error("a plan kcp does not hold was reset")
	}
}

func TestRunPlanVerbs(t *testing.T) {
	code, _, stderr := runWith("plan")
	if code != exitUsage || !strings.Contains(stderr, "a verb is required") {
		t.Errorf("no verb: code %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWith("plan", "explode")
	if code != exitUsage || !strings.Contains(stderr, "unknown verb") {
		t.Errorf("unknown verb: code %d, stderr %q", code, stderr)
	}
	for _, verb := range []string{"show", "approve", "preview"} {
		code, _, stderr = runWith("plan", verb)
		if code != exitUsage || !strings.Contains(stderr, "specctl plan "+verb+": one plan name is required") {
			t.Errorf("%s without a name: code %d, stderr %q", verb, code, stderr)
		}
	}
	code, _, stderr = runWith("plan", "apply")
	if code != exitUsage || !strings.Contains(stderr, "specctl plan apply: -f is required") {
		t.Errorf("apply without a file: code %d, stderr %q", code, stderr)
	}
}
