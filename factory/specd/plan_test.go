package specd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/finding"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/plan"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func planRepositoryObject(path string) *spec.Repository {
	return &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: path},
	}
}

func planWithItem(approved bool, item spec.PlanItem) *spec.Plan {
	return &spec.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: specapi.DefaultNamespace},
		Spec: spec.PlanSpec{
			Repository: "calc",
			Approved:   approved,
			Items:      []spec.PlanItem{item},
		},
	}
}

func planEdit(context string, requirement spec.Requirement) spec.PlanEdit {
	return spec.PlanEdit{Context: context, Requirements: []spec.Requirement{requirement}}
}

func readPlan(t *testing.T, cluster *fakeCluster, name string) *spec.Plan {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.PlanGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	planObject, ok := typed.(*spec.Plan)
	if !ok {
		t.Fatalf("%s is not a Plan", name)
	}
	return planObject
}

func readPlanItem(t *testing.T, cluster *fakeCluster, name string) spec.PlanItemStatus {
	t.Helper()
	status, ok := readPlan(t, cluster, name).ItemStatus("one")
	if !ok {
		t.Fatalf("%s has no status for item one", name)
	}
	return status
}

func readFinding(t *testing.T, cluster *fakeCluster, name string) *spec.Finding {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.FindingGVR, specapi.DefaultNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	recorded, ok := typed.(*spec.Finding)
	if !ok {
		t.Fatalf("%s is not a Finding", name)
	}
	return recorded
}

func planCluster(t *testing.T, planObject *spec.Plan) (*fakeCluster, *Controller, string) {
	t.Helper()
	repo := tempGitRepo(t, "main")
	cluster := newFakeCluster()
	apply(t, cluster, planRepositoryObject(repo))
	contextObject := systemContext("calc", func(*spec.SystemContext) {})
	realized := contextObject.Spec
	hash, err := spec.HashSystemContextSpec(realized)
	if err != nil {
		t.Fatal(err)
	}
	contextObject.Status.RealizedSpecHash = hash
	contextObject.Status.RealizedSpec = &realized
	apply(t, cluster, contextObject)
	apply(t, cluster, planObject)
	head, err := gitrepo.Head(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	return cluster, testController(cluster), head
}

func TestAnUnapprovedPlanOnlyPreviewsItsEdits(t *testing.T) {
	cluster, controller, _ := planCluster(t, planWithItem(false, spec.PlanItem{
		ID:    "one",
		Edits: []spec.PlanEdit{planEdit("calc", spec.Requirement{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum."})},
	}))

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}

	planObject := readPlan(t, cluster, "p1")
	if planObject.Status.Phase != plan.PhaseDraft {
		t.Errorf("phase = %q, want Draft", planObject.Status.Phase)
	}
	item := readPlanItem(t, cluster, "p1")
	if item.Phase != plan.ItemPending {
		t.Errorf("item phase = %q, want Pending", item.Phase)
	}
	if len(item.Preview) != 1 || !strings.HasPrefix(item.Preview[0], "calc: ") {
		t.Errorf("preview = %v, want one line for the edit", item.Preview)
	}
	contextObject := editContext(t, cluster)
	if len(contextObject.Spec.Requirements) != 0 {
		t.Errorf("an unapproved plan wrote the context: %+v", contextObject.Spec.Requirements)
	}
	if contextObject.Annotations[specapi.OriginAnnotation] == specapi.OriginPlan {
		t.Error("an unapproved plan stamped the context as a plan edit")
	}
}

func TestAnApprovedPlanAppliesItsEdit(t *testing.T) {
	cluster, controller, _ := planCluster(t, planWithItem(true, spec.PlanItem{
		ID:    "one",
		Edits: []spec.PlanEdit{planEdit("calc", spec.Requirement{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum."})},
	}))

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}

	contextObject := editContext(t, cluster)
	requirements := contextObject.Spec.Requirements
	if len(requirements) != 1 || requirements[0].ID != "r.add" || requirements[0].Text != "Add returns the sum." {
		t.Fatalf("requirements = %+v, want the edit's requirement", requirements)
	}
	if got := contextObject.Annotations[specapi.OriginAnnotation]; got != specapi.OriginPlan {
		t.Errorf("origin = %q, want plan", got)
	}
	if got := contextObject.Annotations[specapi.PlanItemAnnotation]; got != "p1/one" {
		t.Errorf("plan-item = %q, want p1/one", got)
	}
	if got := contextObject.Annotations[specapi.DeclaredAnnotation]; got != `["requirement/r.add"]` {
		t.Errorf("declared = %s, want the edit's unit", got)
	}

	item := readPlanItem(t, cluster, "p1")
	if item.Phase != plan.ItemRealizing {
		t.Errorf("item phase = %q, want Realizing", item.Phase)
	}
	hash, err := spec.HashSystemContextSpec(contextObject.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if item.AppliedSpecHashes["calc"] != hash {
		t.Errorf("appliedSpecHashes[calc] = %q, want the hash of the written spec %q", item.AppliedSpecHashes["calc"], hash)
	}
	if names := cluster.names(specapi.SpecChangeGVR); len(names) != 0 {
		t.Errorf("the plan raised %v itself, want the edit detection to do it", names)
	}

	if _, err := controller.reconcileSystemContext(context.Background(), specapi.DefaultNamespace, "calc"); err != nil {
		t.Fatal(err)
	}
	raised := cluster.names(specapi.SpecChangeGVR)
	if len(raised) != 1 {
		t.Fatalf("changes = %v, want the spec to code change the edit detection raises", raised)
	}
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, raised[0])
	if err != nil {
		t.Fatal(err)
	}
	change, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	typedChange, ok := change.(*spec.SpecChange)
	if !ok {
		t.Fatalf("%s is not a SpecChange", raised[0])
	}
	if typedChange.Spec.Direction != specapi.DirectionSpecToCode || typedChange.Spec.ToSpecHash != hash {
		t.Errorf("change = %+v, want a spec to code change to the applied hash", typedChange.Spec)
	}
}

func acceptanceCounter(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "runs")
}

func countRuns(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	return len(strings.Fields(strings.ReplaceAll(string(content), "\n", " ")))
}

func TestAnItemWithoutEditsRunsItsAcceptanceAtHead(t *testing.T) {
	counter := acceptanceCounter(t)
	cluster, controller, head := planCluster(t, planWithItem(true, spec.PlanItem{
		ID: "one",
		Acceptance: []spec.AcceptanceStep{{
			Name:    "unit",
			Command: []string{"sh", "-c", `echo run >> "$PLAN_ACCEPTANCE_COUNT"; exit 0`},
			Env:     map[string]string{"PLAN_ACCEPTANCE_COUNT": counter},
		}},
	}))

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}

	item := readPlanItem(t, cluster, "p1")
	if item.Phase != plan.ItemDone {
		t.Fatalf("item phase = %q (%s), want Done", item.Phase, item.Message)
	}
	if item.Commit != head {
		t.Errorf("commit = %q, want the repository HEAD %q", item.Commit, head)
	}
	if len(item.Acceptance) != 1 || !item.Acceptance[0].Passed {
		t.Errorf("acceptance = %+v, want the step recorded as passed", item.Acceptance)
	}
	if runs := countRuns(t, counter); runs != 1 {
		t.Errorf("the step ran %d time(s), want once", runs)
	}

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}
	if runs := countRuns(t, counter); runs != 1 {
		t.Errorf("a second reconcile at the same HEAD ran the step %d time(s), want once", runs)
	}
	if phase := readPlan(t, cluster, "p1").Status.Phase; phase != plan.PhaseComplete {
		t.Errorf("plan phase = %q, want Complete", phase)
	}
}

func TestAFailedAcceptanceStaysVerifyingAndDoesNotRunTwice(t *testing.T) {
	counter := acceptanceCounter(t)
	cluster, controller, head := planCluster(t, planWithItem(true, spec.PlanItem{
		ID: "one",
		Acceptance: []spec.AcceptanceStep{{
			Name:    "unit",
			Command: []string{"sh", "-c", `echo run >> "$PLAN_ACCEPTANCE_COUNT"; exit 1`},
			Env:     map[string]string{"PLAN_ACCEPTANCE_COUNT": counter},
		}},
	}))

	for round := 0; round < 2; round++ {
		if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
			t.Fatal(err)
		}
	}

	item := readPlanItem(t, cluster, "p1")
	if item.Phase != plan.ItemVerifying {
		t.Errorf("item phase = %q, want Verifying: the step failed", item.Phase)
	}
	if item.Commit != head {
		t.Errorf("commit = %q, want the HEAD the step ran at %q", item.Commit, head)
	}
	if len(item.Acceptance) != 1 || item.Acceptance[0].Passed || item.Acceptance[0].ExitCode != 1 {
		t.Errorf("acceptance = %+v, want the failure recorded", item.Acceptance)
	}
	if !strings.Contains(item.Message, "acceptance failed") {
		t.Errorf("message = %q, want the failure", item.Message)
	}
	if runs := countRuns(t, counter); runs != 1 {
		t.Errorf("the step ran %d time(s), want once: Evaluate only asks at a new HEAD", runs)
	}
}

func TestAnApprovedPlanMovesItsFindings(t *testing.T) {
	counter := acceptanceCounter(t)
	cluster, controller, head := planCluster(t, planWithItem(true, spec.PlanItem{
		ID:       "one",
		Findings: []string{"f-1"},
		Acceptance: []spec.AcceptanceStep{{
			Name:    "unit",
			Command: []string{"sh", "-c", `echo run >> "$PLAN_ACCEPTANCE_COUNT"; exit 0`},
			Env:     map[string]string{"PLAN_ACCEPTANCE_COUNT": counter},
		}},
	}))
	apply(t, cluster, &spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: "f-1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.FindingSpec{Repository: "calc", Source: "manual", Summary: "the requirement is not there"},
		Status:     spec.FindingStatus{State: finding.Open},
	})

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}
	planned := readFinding(t, cluster, "f-1")
	if planned.Status.State != finding.Planned {
		t.Fatalf("state = %q, want Planned", planned.Status.State)
	}
	if planned.Status.Plan != "p1" || planned.Status.Item != "one" {
		t.Errorf("finding = %+v, want plan p1 item one", planned.Status)
	}

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}
	resolved := readFinding(t, cluster, "f-1")
	if resolved.Status.State != finding.Resolved {
		t.Fatalf("state = %q, want Resolved once the item is done", resolved.Status.State)
	}
	if resolved.Status.ResolvedCommit != head {
		t.Errorf("resolvedCommit = %q, want %q", resolved.Status.ResolvedCommit, head)
	}
	if resolved.Status.Plan != "p1" || resolved.Status.Item != "one" {
		t.Errorf("finding = %+v, want plan p1 item one", resolved.Status)
	}
}

func TestAPlanLeavesADismissedFindingAlone(t *testing.T) {
	cluster, controller, _ := planCluster(t, planWithItem(true, spec.PlanItem{
		ID:       "one",
		Findings: []string{"f-1"},
		Edits: []spec.PlanEdit{planEdit("calc", spec.Requirement{
			ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum.",
		})},
	}))
	apply(t, cluster, &spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: "f-1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.FindingSpec{Repository: "calc", Source: "manual", Summary: "not this one"},
		Status:     spec.FindingStatus{State: finding.Dismissed},
	})

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}
	if state := readFinding(t, cluster, "f-1").Status.State; state != finding.Dismissed {
		t.Errorf("state = %q, want Dismissed", state)
	}
}

func TestAnInvalidPlanOnlyGetsAFailedStatus(t *testing.T) {
	cluster, controller, _ := planCluster(t, planWithItem(true, spec.PlanItem{ID: "one", DependsOn: []string{"nowhere"}}))

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}
	planObject := readPlan(t, cluster, "p1")
	if planObject.Status.Phase != plan.PhaseFailed {
		t.Errorf("phase = %q, want Failed", planObject.Status.Phase)
	}
	if len(planObject.Status.Items) != 0 {
		t.Errorf("items = %+v, want none: nothing else happens", planObject.Status.Items)
	}
	object, err := cluster.Get(context.Background(), specapi.PlanGVR, specapi.DefaultNamespace, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if got := conditionStatus(t, object, PlanConditionValid); got != "False" {
		t.Errorf("Valid = %s, want False", got)
	}
}

func TestAPlanLeavesAFindingTheNamespaceDoesNotHoldAlone(t *testing.T) {
	cluster, controller, _ := planCluster(t, planWithItem(true, spec.PlanItem{
		ID:       "one",
		Findings: []string{"f-missing"},
	}))

	if _, err := controller.reconcilePlan(context.Background(), specapi.DefaultNamespace, "p1"); err != nil {
		t.Fatal(err)
	}
	if names := cluster.names(specapi.FindingGVR); len(names) != 0 {
		t.Errorf("findings = %v, want the plan to skip a missing one", names)
	}
	if item := readPlanItem(t, cluster, "p1"); item.Phase == "" {
		t.Error("the plan got no status")
	}
}

func TestResourcesWatchPlansFindingsAndAgentRuns(t *testing.T) {
	kinds := map[string]bool{}
	for _, resource := range Resources() {
		kinds[resource.Kind] = true
	}
	for _, kind := range []string{specapi.PlanKind, specapi.FindingKind, specapi.AgentRunKind} {
		if !kinds[kind] {
			t.Errorf("Resources() does not watch %s", kind)
		}
	}
}

func TestAWatchedFindingAndAgentRunHaveAReconciler(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, &spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: "f-1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.FindingSpec{Source: "manual", Summary: "something"},
	})
	apply(t, cluster, &spec.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: specapi.DefaultNamespace},
		Spec:       spec.AgentRunSpec{Purpose: "summarize"},
	})
	controller := testController(cluster)

	for _, watched := range []struct{ kind, name string }{
		{specapi.FindingKind, "f-1"},
		{specapi.AgentRunKind, "run-1"},
	} {
		requeue, err := controller.reconcile(context.Background(),
			key{Kind: watched.kind, Namespace: specapi.DefaultNamespace, Name: watched.name})
		if err != nil {
			t.Fatalf("%s: %v", watched.kind, err)
		}
		if requeue != 0 {
			t.Errorf("%s requeued for %s, want nothing", watched.kind, requeue)
		}
	}
}
