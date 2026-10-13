package e2e

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/finding"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/plan"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const (
	planLiveRepository = "plan-calc"

	planLivePlan = "plan-calc"

	planLiveItem = "subtract"

	planLiveFinding = "plan-calc-subtract"

	planLiveContext = "calc"
)

// TestPlanLiveDraftApproveRealizeVerify drives one plan the whole way on a live
// kcp: it previews while it is a draft, applies its edit once it is approved,
// waits for the scripted agent to realize the change the edit raised, runs its
// acceptance at the commit that landed, moves the finding it names to Resolved,
// and leaves plans/plan-calc.yaml on the repository's orphan branch.
func TestPlanLiveDraftApproveRealizeVerify(t *testing.T) {
	t.Parallel()
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", planLiveRepository)
	names := []string{planLiveContext, "cmd-calc", planLiveRepository}
	repositories := []string{planLiveRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	_ = client.Delete(ctx, specapi.PlanGVR, specapi.DefaultNamespace, planLivePlan)
	_ = client.Delete(ctx, specapi.FindingGVR, specapi.DefaultNamespace, planLiveFinding)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
		_ = client.Delete(cleanupCtx, specapi.PlanGVR, specapi.DefaultNamespace, planLivePlan)
		_ = client.Delete(cleanupCtx, specapi.FindingGVR, specapi.DefaultNamespace, planLiveFinding)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: planLiveRepository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{"go", "test", "./..."},
			Agent:  &spec.AgentSpec{Kind: "scripted:" + phase6Scenario(t, "scenario.yaml")},
		},
	})
	applyTyped(t, ctx, client, planLiveBaseline())

	startPlan2bController(t, ctx, specd.DefaultMaxAttempts, 2*time.Second)

	waitFor(t, ctx, "the first ingest of calc", func() bool {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, planLiveContext)
		if err != nil {
			return false
		}
		fingerprint, _, _ := unstructured.NestedString(object.Object, "status", "observed", "fingerprint")
		realized, _, _ := unstructured.NestedString(object.Object, "status", "realizedSpecHash")
		return fingerprint != "" && realized != ""
	})
	baseCommit := headOf(t, repoPath)
	before := liveContextNames(t, ctx, client, planLiveContext)

	applyTyped(t, ctx, client, &spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: planLiveFinding, Namespace: specapi.DefaultNamespace},
		Spec: spec.FindingSpec{
			Repository: planLiveRepository,
			Source:     "manual",
			Context:    planLiveContext,
			Unit:       "Subtract",
			Summary:    "the calc package has no Subtract",
		},
	})
	applyTyped(t, ctx, client, planLivePlanObject())

	// A plan that is not approved only previews: it drafts, it names the edit it
	// would make, and it writes nothing.
	var drafted spec.PlanItemStatus
	waitForState(t, ctx, "the plan to be drafted", func() bool {
		planObject, ok := readPlanLive(t, ctx, client)
		if !ok || planObject.Status.Phase != plan.PhaseDraft {
			return false
		}
		item, recorded := planObject.ItemStatus(planLiveItem)
		if !recorded || len(item.Preview) == 0 {
			return false
		}
		drafted = item
		return true
	}, planLiveState(t, ctx, client))

	if !strings.HasPrefix(drafted.Preview[0], planLiveContext+": ") {
		t.Errorf("preview = %v, want a line for the edit of %s", drafted.Preview, planLiveContext)
	}
	object := getContext(t, ctx, client, planLiveContext)
	if got := object.GetAnnotations()[specapi.OriginAnnotation]; got == specapi.OriginPlan {
		t.Error("an unapproved plan stamped the context as a plan edit")
	}
	if after := liveContextNames(t, ctx, client, planLiveContext); after != before {
		t.Errorf("an unapproved plan changed the spec of %s:\nbefore %s\nafter  %s", planLiveContext, before, after)
	}

	approvePlanLive(t, ctx, client)

	var applied string
	waitForState(t, ctx, "the item to realize its edit", func() bool {
		planObject, ok := readPlanLive(t, ctx, client)
		if !ok {
			return false
		}
		item, recorded := planObject.ItemStatus(planLiveItem)
		if !recorded || item.Phase != plan.ItemRealizing {
			return false
		}
		hash := item.AppliedSpecHashes[planLiveContext]
		if hash == "" {
			return false
		}
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), planLiveContext, specapi.DirectionSpecToCode) {
			if change.Spec.ToSpecHash == hash {
				applied = hash
				return true
			}
		}
		return false
	}, planLiveState(t, ctx, client))

	// The change the plan edit raised is the one the agent realizes.
	var landed spec.SpecChange
	waitForState(t, ctx, "the SpecToCode change of "+planLiveContext+" to succeed", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), planLiveContext, specapi.DirectionSpecToCode) {
			if change.Spec.ToSpecHash == applied && change.Status.Phase == specapi.PhaseSucceeded {
				landed = change
				return true
			}
		}
		return false
	}, planLiveState(t, ctx, client))

	commit := landed.Status.Commit
	if commit == "" {
		t.Fatalf("the change reports no commit: %+v", landed.Status)
	}
	if head := headOf(t, repoPath); head != commit {
		t.Errorf("HEAD = %s, want the change's commit %s", head, commit)
	}
	if head := headOf(t, repoPath); head == baseCommit {
		t.Error("HEAD did not advance")
	}

	// The item is done at that commit, and its acceptance passed there.
	var done spec.PlanItemStatus
	waitForState(t, ctx, "the item to be done", func() bool {
		planObject, ok := readPlanLive(t, ctx, client)
		if !ok {
			return false
		}
		item, recorded := planObject.ItemStatus(planLiveItem)
		if !recorded || item.Phase != plan.ItemDone {
			return false
		}
		done = item
		return true
	}, planLiveState(t, ctx, client))

	if done.Commit != commit {
		t.Errorf("item commit = %q, want the checkout HEAD %q", done.Commit, commit)
	}
	if len(done.Acceptance) == 0 {
		t.Errorf("the item recorded no acceptance: %+v", done)
	}
	for _, result := range done.Acceptance {
		if !result.Passed {
			t.Errorf("acceptance %s did not pass: %+v", result.Name, result)
		}
	}

	waitForState(t, ctx, "the plan to complete", func() bool {
		planObject, ok := readPlanLive(t, ctx, client)
		return ok && planObject.Status.Phase == plan.PhaseComplete
	}, planLiveState(t, ctx, client))

	// The finding the item names is resolved by the commit that landed.
	var resolved spec.Finding
	waitForState(t, ctx, "the finding to resolve", func() bool {
		recorded, ok := readFindingLive(t, ctx, client)
		if !ok || recorded.Status.State != finding.Resolved {
			return false
		}
		resolved = *recorded
		return true
	}, func() string {
		recorded, ok := readFindingLive(t, ctx, client)
		if !ok {
			return "the finding is not readable"
		}
		return recorded.Status.State + " " + recorded.Status.Plan + "/" + recorded.Status.Item + " " + recorded.Status.ResolvedCommit
	})

	if resolved.Status.ResolvedCommit != commit {
		t.Errorf("resolvedCommit = %q, want %q", resolved.Status.ResolvedCommit, commit)
	}
	if resolved.Status.Plan != planLivePlan || resolved.Status.Item != planLiveItem {
		t.Errorf("finding = %+v, want plan %s item %s", resolved.Status, planLivePlan, planLiveItem)
	}

	// The whole plan is written to the repository's orphan branch.
	var doc planLiveDoc
	waitForState(t, ctx, "plans/"+planLivePlan+".yaml on the orphan branch", func() bool {
		contents := planLiveOrphanDoc(t, repoPath)
		if contents == "" {
			return false
		}
		parsed := planLiveDoc{}
		if err := yaml.Unmarshal([]byte(contents), &parsed); err != nil {
			return false
		}
		if parsed.Status.Phase != plan.PhaseComplete {
			return false
		}
		doc = parsed
		return true
	}, func() string { return planLiveOrphanDoc(t, repoPath) })

	if len(doc.Status.Items) != 1 || doc.Status.Items[0].ID != planLiveItem {
		t.Fatalf("plans doc items = %+v, want the one item", doc.Status.Items)
	}
	if got := doc.Status.Items[0].Commit; got != commit {
		t.Errorf("plans doc commit = %q, want %q", got, commit)
	}
}

// planLiveDoc is the summary of a Plan that plans/<name>.yaml holds: everything
// this test reads back from the orphan branch.
type planLiveDoc struct {
	Status struct {
		Phase string `json:"phase"`

		Items []struct {
			ID string `json:"id"`

			Phase string `json:"phase"`

			Commit string `json:"commit"`
		} `json:"items"`
	} `json:"status"`
}

func planLiveBaseline() *spec.SystemContext {
	baseline := plan2bCalcBaseline()
	baseline.Spec.Repository = planLiveRepository
	return baseline
}

func planLivePlanObject() *spec.Plan {
	return &spec.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: planLivePlan, Namespace: specapi.DefaultNamespace},
		Spec: spec.PlanSpec{
			Repository: planLiveRepository,
			Title:      "Subtract lands in the calc package",
			Items: []spec.PlanItem{{
				ID:       planLiveItem,
				Summary:  "Add Subtract and a test that calls it",
				Findings: []string{planLiveFinding},
				Edits: []spec.PlanEdit{{
					Context: planLiveContext,
					Interfaces: []spec.Interface{{
						Name: "Subtract", Kind: "function",
						Signature: "func Subtract(a, b int) int", File: "calc/calc.go",
					}},
					Requirements: []spec.Requirement{{
						ID: "r.subtract", Level: spec.LevelShould,
						Text:     "Subtract returns the difference of two integers.",
						CodeRefs: []string{"function:Subtract", "file:calc/calc.go"},
					}},
				}},
				Acceptance: []spec.AcceptanceStep{{
					Name:    "go test ./...",
					Command: []string{"go", "test", "./..."},
				}},
			}},
		},
	}
}

// approvePlanLive applies spec.approved=true to the plan the way the approve
// verb does: the plan as it is, with the status dropped.
func approvePlanLive(t *testing.T, ctx context.Context, client *kcpclient.Client) {
	t.Helper()
	planObject, ok := readPlanLive(t, ctx, client)
	if !ok {
		t.Fatalf("the plan %s is not readable", planLivePlan)
	}
	planObject.Spec.Approved = true
	planObject.Spec.ApprovedBy = "the plan live test"
	object, err := kcpclient.Unstructured(planObject)
	if err != nil {
		t.Fatal(err)
	}
	unstructured.RemoveNestedField(object.Object, "status")
	if _, err := client.ServerSideApply(ctx, object, "specctl-approve"); err != nil {
		t.Fatalf("approve the plan: %v", err)
	}
}

func readPlanLive(t *testing.T, ctx context.Context, client *kcpclient.Client) (*spec.Plan, bool) {
	t.Helper()
	object, err := client.Get(ctx, specapi.PlanGVR, specapi.DefaultNamespace, planLivePlan)
	if err != nil {
		return nil, false
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, false
	}
	planObject, ok := typed.(*spec.Plan)
	if !ok {
		return nil, false
	}
	return planObject, true
}

func readFindingLive(t *testing.T, ctx context.Context, client *kcpclient.Client) (*spec.Finding, bool) {
	t.Helper()
	object, err := client.Get(ctx, specapi.FindingGVR, specapi.DefaultNamespace, planLiveFinding)
	if err != nil {
		return nil, false
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil, false
	}
	recorded, ok := typed.(*spec.Finding)
	if !ok {
		return nil, false
	}
	return recorded, true
}

// liveContextNames is the spec of a context as the ids of its requirements and
// the names of its interfaces: what a plan must not change until it is
// approved, and what the code-to-spec direction must not move under it.
func liveContextNames(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) string {
	t.Helper()
	object := getContext(t, ctx, client, name)
	return strings.Join(entryNames(object, "spec", "requirements"), ",") +
		" | " + strings.Join(entryNames(object, "spec", "interfaces"), ",")
}

// entryNames is the id (or the name) of every entry of a spec list, and nothing
// where the list is not there: an empty list and an absent one read the same.
func entryNames(object *unstructured.Unstructured, fields ...string) []string {
	entries, found, err := unstructured.NestedSlice(object.Object, fields...)
	if err != nil || !found {
		return nil
	}
	out := []string{}
	for _, entry := range entries {
		mapping, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"id", "name"} {
			if value, ok := mapping[key].(string); ok && value != "" {
				out = append(out, value)
				break
			}
		}
	}
	return out
}

// planLiveState is what a plan wait reports when it times out.
func planLiveState(t *testing.T, ctx context.Context, client *kcpclient.Client) func() string {
	return func() string {
		planObject, ok := readPlanLive(t, ctx, client)
		if !ok {
			return "the plan is not readable"
		}
		item, _ := planObject.ItemStatus(planLiveItem)
		changes := []string{}
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), planLiveContext, specapi.DirectionSpecToCode) {
			changes = append(changes, change.Name+" "+change.Status.Phase+" to="+change.Spec.ToSpecHash)
		}
		return "plan phase " + planObject.Status.Phase +
			", item " + item.Phase + " (" + item.Message + ")" +
			", applied " + item.AppliedSpecHashes[planLiveContext] +
			", commit " + item.Commit +
			", changes " + strings.Join(changes, "; ")
	}
}

// planLiveOrphanDoc is plans/<plan>.yaml as the repository's orphan branch holds
// it, or "" while the branch or the file is not there yet.
func planLiveOrphanDoc(t *testing.T, repoPath string) string {
	t.Helper()
	path := oabranch.Branch(planLiveRepository) + ":" + oabranch.PlanPath(planLivePlan)
	output, err := exec.Command("git", "-C", repoPath, "show", path).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(output)
}
