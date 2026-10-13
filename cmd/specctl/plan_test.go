package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/plan"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
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

func TestPlanItemsSelectsTheWorkOfAContext(t *testing.T) {
	live := fixturePlan("p0011")
	live.Spec.Items = []spec.PlanItem{
		{ID: "A", Summary: "adds the flag", Edits: []spec.PlanEdit{
			{Context: "calc", Intent: "adds"},
			{Context: "sc.deno-kcp", Intent: "elsewhere"},
		}},
		{ID: "B"},
		{ID: "C", Edits: []spec.PlanEdit{{Context: "other", Intent: "other"}}},
		{ID: "D", Edits: []spec.PlanEdit{{Context: "calc", Intent: "done"}}},
		{ID: "E", Edits: []spec.PlanEdit{{Context: "calc", Intent: "failed"}}},
		{ID: "F", Summary: "waits", Edits: []spec.PlanEdit{{Context: "calc", Intent: "next"}}},
	}
	live.Status.Phase = plan.PhaseRunning
	live.Status.Items = []spec.PlanItemStatus{
		{ID: "A", Phase: plan.ItemVerifying},
		{ID: "D", Phase: plan.ItemDone},
		{ID: "E", Phase: plan.ItemFailed},
	}
	complete := fixturePlan("p0002")
	complete.Status.Phase = plan.PhaseComplete
	complete.Spec.Items = []spec.PlanItem{{ID: "A", Edits: []spec.PlanEdit{{Context: "calc"}}}}
	failed := fixturePlan("p0003")
	failed.Status.Phase = plan.PhaseFailed
	failed.Spec.Items = []spec.PlanItem{{ID: "A", Edits: []spec.PlanEdit{{Context: "calc"}}}}

	views := selectPlanItems([]spec.Plan{live, complete, failed}, "calc")
	if len(views) != 2 {
		t.Fatalf("items: got %d views %+v, want 2", len(views), views)
	}
	if views[0].Plan != "p0011" || views[0].Item != "A" {
		t.Errorf("first view: got %s/%s, want p0011/A", views[0].Plan, views[0].Item)
	}
	if views[0].Phase != plan.ItemVerifying {
		t.Errorf("phase: got %q, want %q", views[0].Phase, plan.ItemVerifying)
	}
	if views[0].Summary != "adds the flag" {
		t.Errorf("summary: got %q", views[0].Summary)
	}
	if len(views[0].Edits) != 1 || views[0].Edits[0].Context != "calc" {
		t.Errorf("edits kept the other contexts: %+v", views[0].Edits)
	}
	if views[1].Item != "F" || views[1].Phase != plan.ItemPending {
		t.Errorf("second view: got %s [%s], want F [Pending]", views[1].Item, views[1].Phase)
	}
	for _, view := range views {
		if view.Item == "D" || view.Item == "E" {
			t.Errorf("a Done or Failed item survived: %+v", views)
		}
	}
}

func TestPlanItemsResolvesArchContextsAndPending(t *testing.T) {
	subject := fixturePlan("p0011")
	subject.Status.Phase = plan.PhaseRunning
	subject.Status.Items = nil
	subject.Spec.Items = []spec.PlanItem{
		{ID: "A", Edits: []spec.PlanEdit{{Context: "sc.deno-kcp", Intent: "adds"}}},
		{ID: "B", Edits: []spec.PlanEdit{{Context: "calc"}}},
	}
	views := selectPlanItems([]spec.Plan{subject}, "sc-deno-kcp")
	if len(views) != 1 || views[0].Item != "A" {
		t.Fatalf("arch id was not resolved: %+v", views)
	}
	if views[0].Phase != plan.ItemPending {
		t.Errorf("an item without status: got %q, want %q", views[0].Phase, plan.ItemPending)
	}

	if views := selectPlanItems([]spec.Plan{subject}, "absent"); len(views) != 0 {
		t.Errorf("a context nobody touches listed %+v", views)
	}
}

func TestPlanItemsRendersTheLines(t *testing.T) {
	views := []planContextItem{{
		Plan:     "p0011",
		Item:     "A",
		Phase:    plan.ItemPending,
		Summary:  "adds the flag",
		Previews: []string{"calc: adds 1 requirement"},
	}, {
		Plan:  "p0012",
		Item:  "B",
		Phase: plan.ItemRealizing,
	}}
	out := &bytes.Buffer{}
	if err := renderPlanContextItems(out, views, "text"); err != nil {
		t.Fatalf("renderPlanContextItems: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	want := []string{
		"- p0011/A [Pending] adds the flag",
		"    calc: adds 1 requirement",
		"- p0012/B [Realizing]",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines: got %q, want %q", lines, want)
	}
	for index, line := range lines {
		if line != want[index] {
			t.Errorf("line %d: got %q, want %q", index, line, want[index])
		}
	}

	out = &bytes.Buffer{}
	if err := renderPlanContextItems(out, views, "json"); err != nil {
		t.Fatalf("renderPlanContextItems json: %v", err)
	}
	text := out.String()
	for _, want := range []string{`"plan": "p0011"`, `"item": "A"`, `"phase": "Pending"`, `"previews"`} {
		if !strings.Contains(text, want) {
			t.Errorf("json is missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "Edits") {
		t.Errorf("json leaked the edits: %q", text)
	}
}

func TestPlanItemsRequiresAContext(t *testing.T) {
	code, _, stderr := runWith("plan", "items")
	if code != exitUsage || !strings.Contains(stderr, "specctl plan items: --context is required") {
		t.Errorf("no context: code %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWith("plan", "items", "--context", "calc", "extra")
	if code != exitUsage || !strings.Contains(stderr, "unexpected argument") {
		t.Errorf("positional: code %d, stderr %q", code, stderr)
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
	for _, verb := range []string{"show", "approve", "preview", "status"} {
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

// planStatusView is one instance's view of a plan, the way plan status reads
// it: the record of the instance and the plan itself.
func planStatusView(repository, branch string, subject spec.Plan) instancePlan {
	return instancePlan{
		Record: session.Record{Repository: repository, Branch: branch},
		Plan:   subject,
	}
}

// planStatusPlan builds the plan an instance holds: one repository, the items
// and the status entries the test names.
func planStatusPlan(repository string, items []spec.PlanItem, statuses []spec.PlanItemStatus) spec.Plan {
	subject := spec.Plan{
		Spec:   spec.PlanSpec{Repository: repository, Items: items},
		Status: spec.PlanStatus{Items: statuses},
	}
	subject.Name = "p0011"
	subject.Namespace = "team"
	return subject
}

// planStatusRowsOf picks the rows one instance shows.
func planStatusRowsOf(rows []planStatusRow, instance string) []planStatusRow {
	out := []planStatusRow{}
	for _, row := range rows {
		if row.Instance == instance {
			out = append(out, row)
		}
	}
	return out
}

// TestPlanStatusMergeOwnerPhase: an item owned by repository b shows b's
// phase, and the instances that do not own it show Elsewhere.
func TestPlanStatusMergeOwnerPhase(t *testing.T) {
	items := []spec.PlanItem{{ID: "A", Repository: "b", Summary: "act on b"}}
	views := []instancePlan{
		planStatusView("a", "main", planStatusPlan("a", items, []spec.PlanItemStatus{
			{ID: "A", Phase: plan.ItemElsewhere, Message: "the instance of b acts on this item"},
		})),
		planStatusView("b", "main", planStatusPlan("a", items, []spec.PlanItemStatus{
			{ID: "A", Phase: plan.ItemDone, Commit: "abcdef0123456789"},
		})),
	}

	rows, _ := mergePlanStatus(views)
	if len(rows) != 2 {
		t.Fatalf("rows: got %d, want one per item per instance", len(rows))
	}
	owner := planStatusRowsOf(rows, "b")
	if len(owner) != 1 {
		t.Fatalf("b rows: got %d, want 1", len(owner))
	}
	if owner[0].Phase != plan.ItemDone || owner[0].Repository != "b" || owner[0].Item != "A" {
		t.Errorf("owner row: %+v, want item A of b in phase Done", owner[0])
	}
	if owner[0].Commit != "abcdef012345" {
		t.Errorf("owner commit: got %q, want the first 12 characters", owner[0].Commit)
	}
	other := planStatusRowsOf(rows, "a")
	if len(other) != 1 {
		t.Fatalf("a rows: got %d, want 1", len(other))
	}
	if other[0].Phase != plan.ItemElsewhere || other[0].Repository != "b" {
		t.Errorf("non-owner row: %+v, want Elsewhere with repository b", other[0])
	}
}

// TestPlanStatusMergeWritesDoneIntoElsewhere: a Done item of b lands in the
// status of a, which shows it Elsewhere, with the message that names where it
// finished.
func TestPlanStatusMergeWritesDoneIntoElsewhere(t *testing.T) {
	items := []spec.PlanItem{{ID: "A", Repository: "b"}}
	views := []instancePlan{
		planStatusView("a", "main", planStatusPlan("a", items, []spec.PlanItemStatus{
			{ID: "A", Phase: plan.ItemElsewhere},
			{ID: "B", Phase: plan.ItemPending},
		})),
		planStatusView("b", "main", planStatusPlan("a", items, []spec.PlanItemStatus{
			{ID: "A", Phase: plan.ItemDone, Commit: "abcdef0123456789"},
		})),
	}

	_, writes := mergePlanStatus(views)
	if len(writes) != 1 {
		t.Fatalf("writes: got %d, want one for the instance that shows Elsewhere", len(writes))
	}
	write := writes[0]
	if write.Instance.Repository != "a" || write.Plan != "p0011" || write.Namespace != "team" {
		t.Errorf("write target: %+v, want plan p0011 of a", write)
	}
	if len(write.Items) != 2 {
		t.Fatalf("items: got %d, want the two a holds", len(write.Items))
	}
	if write.Items[0].ID != "A" || write.Items[0].Phase != plan.ItemDone || write.Items[0].Commit != "abcdef0123456789" {
		t.Errorf("written item: %+v, want Done with b's commit", write.Items[0])
	}
	if write.Items[0].Message != "Done in b" {
		t.Errorf("message: got %q, want %q", write.Items[0].Message, "Done in b")
	}
	if write.Items[1].Phase != plan.ItemPending {
		t.Errorf("the other entry changed: %+v", write.Items[1])
	}
}

// TestPlanStatusMergeNoOwner: an item no instance owns shows "-", and nothing
// is written for it.
func TestPlanStatusMergeNoOwner(t *testing.T) {
	items := []spec.PlanItem{{ID: "A", Summary: "nobody acts"}}
	views := []instancePlan{
		planStatusView("a", "main", planStatusPlan("c", items, nil)),
		planStatusView("b", "main", planStatusPlan("c", items, nil)),
	}

	rows, writes := mergePlanStatus(views)
	if len(rows) != 2 {
		t.Fatalf("rows: got %d, want one per instance", len(rows))
	}
	for _, row := range rows {
		if row.Phase != "-" {
			t.Errorf("row %+v: phase %q, want -", row, row.Phase)
		}
		if row.Repository != "c" {
			t.Errorf("row %+v: repository %q, want c", row, row.Repository)
		}
	}
	if len(writes) != 0 {
		t.Errorf("writes: got %d, want none for an item nothing owns", len(writes))
	}
}

// TestPlanStatusMergeKeepsAFinishedItem: an item the instance holds itself is
// not written where the instance already recorded its own phase.
func TestPlanStatusMergeKeepsAFinishedItem(t *testing.T) {
	items := []spec.PlanItem{{ID: "A"}}
	views := []instancePlan{
		planStatusView("a", "main", planStatusPlan("a", items, []spec.PlanItemStatus{
			{ID: "A", Phase: plan.ItemPending},
		})),
		planStatusView("b", "main", planStatusPlan("a", items, []spec.PlanItemStatus{
			{ID: "A", Phase: plan.ItemDone, Commit: "abcdef0123456789"},
		})),
	}

	rows, writes := mergePlanStatus(views)
	if len(writes) != 0 {
		t.Errorf("writes: got %d, want none where a does not show Elsewhere", len(writes))
	}
	owner := planStatusRowsOf(rows, "a")
	if len(owner) != 1 || owner[0].Phase != plan.ItemPending {
		t.Errorf("owner rows: %+v, want a Pending row of a", owner)
	}
}
