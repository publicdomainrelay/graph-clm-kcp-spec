package e2e

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policykcp"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
	"github.com/publicdomainrelay/kcp-libs/common/logging"
)

const policyProbePath = "test/policy_probe_test.ts"

// policyContextNames are the directory-partitioned contexts the market-mini
// fixture produces; the tests delete them too, because a context name belongs
// to one repository at a time.
var policyContextNames = []string{
	"hono-bidder", "lib-cloud-init", "lib-compute-provider", "lib-market-common", "lib-requester", "test",
}

const policyViolatingProbe = `import { runComputeContract } from "@market-mini/requester";
import type { Contract } from "@market-mini/market-common";

Deno.test("the probe dials the guest directly", async () => {
  const contract: Contract = { vmId: "vm", providerId: "p", guestHost: "10.0.0.7", guestPort: 22 };
  const connection = await Deno.connect({ hostname: contract.guestHost, port: contract.guestPort });
  connection.close();
  await runComputeContract(contract);
});
`

const policyComplyingProbe = `import { runComputeContract } from "@market-mini/requester";
import type { Contract } from "@market-mini/market-common";

Deno.test("the probe reaches the guest over the relay", async () => {
  const contract: Contract = { vmId: "vm", providerId: "p", guestHost: "10.0.0.7", guestPort: 22 };
  const result = await runComputeContract(contract);
  if (typeof result.exitCode !== "number") {
    throw new Error("no exit code");
  }
});
`

// seedPolicyBranch writes the example policy library to the orphan branch
// open-policy/<repository> of the code checkout, the way specctl policy build
// would.
func seedPolicyBranch(t *testing.T, repoPath, repository string, library policy.Library) {
	t.Helper()
	ctx := context.Background()
	store := oagit.Store{Repo: repoPath}
	ref := policy.RefFor(repository, "main", store.DefaultBranch(ctx))
	if _, _, err := policygit.Init(ctx, store, repository, ref, library.Manifest, library.Lib, policyeval.LibTest()); err != nil {
		t.Fatalf("seed the policy branch: %v", err)
	}
	files, err := policykcp.Files(library)
	if err != nil {
		t.Fatal(err)
	}
	dist, err := policyeval.Dist(library)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range dist {
		files[name] = data
	}
	files[policy.CataloguePath] = policyeval.Catalogue(library)
	if _, err := policygit.Update(ctx, store, ref, files, nil, "policy seed"); err != nil {
		t.Fatalf("seed the policy branch: %v", err)
	}
}

func examplePolicyLibrary(t *testing.T) policy.Library {
	t.Helper()
	library, err := policyeval.Load(filepath.Join(repoRoot(t), "examples", "policies", "market-mini"))
	if err != nil {
		t.Fatalf("load the example policy: %v", err)
	}
	if len(library.Templates) == 0 {
		t.Fatal("the example policy has no template")
	}
	return library
}

func withEnforcement(library policy.Library, enforcement policy.Enforcement) policy.Library {
	out := library
	out.Constraints = make([]policy.Constraint, len(library.Constraints))
	copy(out.Constraints, library.Constraints)
	for index := range out.Constraints {
		out.Constraints[index].Enforcement = enforcement
	}
	return out
}

func startPolicyController(t *testing.T, ctx context.Context, agentKind string, maxAttempts int) {
	t.Helper()
	controller, err := specd.New(specd.Options{
		Kubeconfig:   e2eKubeconfig,
		Workspace:    e2eWorkspace,
		Namespace:    specapi.DefaultNamespace,
		QPS:          50,
		Burst:        100,
		Resync:       500 * time.Millisecond,
		MaxAttempts:  maxAttempts,
		RetryBackoff: time.Second,
		BatchWindow:  2 * time.Second,
		Agent:        agentKind,
		Persist:      true,
		PersistDelay: 200 * time.Millisecond,
		Log:          policyLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- controller.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("the controller stopped with %v", err)
			}
		case <-time.After(60 * time.Second):
			t.Error("the controller did not stop")
		}
	})
}

func policyLog() *slog.Logger {
	if os.Getenv("SPECD_E2E_LOG") == "" {
		return logging.Discard()
	}
	return logging.New(logging.Options{Service: "specd-policy-e2e", Writer: os.Stderr, Level: slog.LevelDebug})
}

func policyMarketMini(t *testing.T, repository string) string {
	t.Helper()
	return fixture.CopyAs(t, filepath.Join("market-mini", "compliant"), repository)
}

func policyRepository(repository, repoPath string) *spec.Repository {
	return &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: repository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{"true"},
		},
	}
}

func waitForContexts(t *testing.T, ctx context.Context, client *kcpclient.Client, repository string) []spec.SystemContext {
	t.Helper()
	waitFor(t, ctx, "the contexts of "+repository, func() bool {
		return len(repositoryContexts(t, ctx, client, repository)) > 0
	})
	return repositoryContexts(t, ctx, client, repository)
}

func repositoryContexts(t *testing.T, ctx context.Context, client *kcpclient.Client, repository string) []spec.SystemContext {
	t.Helper()
	listed, err := client.List(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace)
	if err != nil {
		return nil
	}
	out := []spec.SystemContext{}
	for index := range listed.Items {
		typed, err := kcpclient.Typed(&listed.Items[index])
		if err != nil {
			continue
		}
		systemContext, ok := typed.(*spec.SystemContext)
		if !ok || systemContext.Spec.Repository != repository {
			continue
		}
		out = append(out, *systemContext)
	}
	return out
}

// writeAttemptScenario writes a scenario whose realize steps depend on the
// attempt: the first writes the violating probe, the second the complying one.
// The controller loads the file per realize, so the test may rewrite it after
// the contexts are known.
func writeAttemptScenario(t *testing.T, path string, contexts []spec.SystemContext, repository string) {
	t.Helper()
	scenario := scriptedagent.Scenario{
		Contexts: map[string]scriptedagent.Draft{},
		Attempts: map[string][][]scriptedagent.Step{},
	}
	for _, systemContext := range contexts {
		scenario.Contexts[systemContext.Name] = scriptedagent.Draft{
			Intent: "the " + systemContext.Name + " context of " + repository,
		}
		scenario.Attempts[systemContext.Name] = [][]scriptedagent.Step{
			{{Write: &scriptedagent.Write{Path: policyProbePath, Contents: policyViolatingProbe}}},
			{{Write: &scriptedagent.Write{Path: policyProbePath, Contents: policyComplyingProbe}}},
		}
	}
	writeScenario(t, path, scenario)
}

func writeScenario(t *testing.T, path string, scenario scriptedagent.Scenario) {
	t.Helper()
	data, err := yaml.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func specEditFor(context string, repository string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": context, "namespace": specapi.DefaultNamespace},
		"spec": map[string]any{
			"repository": repository,
			"requirements": []any{map[string]any{
				"id": "r.policy-probe", "level": "MUST",
				"text":     "The integration test reaches the guest only through the relay.",
				"codeRefs": []any{"file:" + policyProbePath},
			}},
		},
	}}
}

func forgetPolicies(t *testing.T, ctx context.Context, client *kcpclient.Client) {
	t.Helper()
	listed, err := client.ListCluster(ctx, policy.ConstraintTemplateGVR())
	if err != nil {
		return
	}
	for index := range listed.Items {
		if err := client.DeleteCluster(ctx, policy.ConstraintTemplateGVR(), listed.Items[index].GetName()); err != nil {
			t.Logf("forget policy %s: %v", listed.Items[index].GetName(), err)
		}
	}
}

func repositoryPolicyStatus(t *testing.T, ctx context.Context, client *kcpclient.Client, repository string) map[string]any {
	t.Helper()
	object, err := client.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, repository)
	if err != nil {
		return nil
	}
	status, _, _ := unstructured.NestedMap(object.Object, "status", "policy")
	return status
}

func conditionStatus(t *testing.T, ctx context.Context, client *kcpclient.Client, name, condition string) string {
	t.Helper()
	object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, name)
	if err != nil {
		return ""
	}
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, raw := range conditions {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if entry["type"] == condition {
			status, _ := entry["status"].(string)
			return status
		}
	}
	return ""
}

// TestPolicyRestoreAndAudit covers the branch to kcp direction and the audit:
// a Repository created over a checkout that already has a policy branch gets
// its templates and constraints restored, and the audit fills the Repository
// status and the PolicyCompliant condition of the context that owns the
// violating file.
func TestPolicyRestoreAndAudit(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	const repository = "policy-audit"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, examplePolicyLibrary(t))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{repository}, nil)
		forgetPolicies(t, cleanupCtx, client)
	})

	startPolicyController(t, ctx, "", 1)
	applyTyped(t, ctx, client, policyRepository(repository, repoPath))

	waitFor(t, ctx, "the policy branch to be restored into kcp", func() bool {
		library, err := policykcp.Read(ctx, client)
		if err != nil {
			return false
		}
		return len(library.Templates) == 1 && len(library.Constraints) == 1
	})
	library, err := policykcp.Read(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if library.Templates[0].Name != "nodirectguestconnect" {
		t.Errorf("template = %s", library.Templates[0].Name)
	}
	if library.Constraints[0].Enforcement != policy.EnforcementDeny {
		t.Errorf("enforcement = %s", library.Constraints[0].Enforcement)
	}

	contexts := waitForContexts(t, ctx, client, repository)
	dumped := false
	waitFor(t, ctx, "the audit of the repository", func() bool {
		status := repositoryPolicyStatus(t, ctx, client, repository)
		if status == nil || status["evaluatedCommit"] == nil || status["evaluatedCommit"] == "" {
			return false
		}
		for _, systemContext := range contexts {
			got := conditionStatus(t, ctx, client, systemContext.Name, specapi.ConditionPolicyCompliant)
			if got != "True" {
				if !dumped {
					dumped = true
					t.Logf("context %s PolicyCompliant = %q; status = %v", systemContext.Name, got, status)
				}
				return false
			}
		}
		return true
	})
	for _, systemContext := range contexts {
		if got := conditionStatus(t, ctx, client, systemContext.Name, specapi.ConditionPolicyCompliant); got != "True" {
			t.Errorf("PolicyCompliant of %s = %q, want True", systemContext.Name, got)
		}
	}

	status := repositoryPolicyStatus(t, ctx, client, repository)
	totals, _ := status["totals"].(map[string]any)
	if numberOf(totals["deny"])+numberOf(totals["warn"])+numberOf(totals["dryrun"]) != 0 {
		t.Errorf("the compliant checkout has violations: %v", status)
	}

	// The violating probe lands in test/, a directory the root context owns.
	violating := filepath.Join(repoPath, policyProbePath)
	if err := os.WriteFile(violating, []byte(policyViolatingProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.Commit(t, repoPath, "a violating probe")

	waitFor(t, ctx, "the walkthrough audit to flag the probe", func() bool {
		status := repositoryPolicyStatus(t, ctx, client, repository)
		totals, _ := status["totals"].(map[string]any)
		if numberOf(totals["deny"]) == 0 {
			return false
		}
		for _, systemContext := range repositoryContexts(t, ctx, client, repository) {
			if conditionStatus(t, ctx, client, systemContext.Name, specapi.ConditionPolicyCompliant) == "False" {
				return true
			}
		}
		return false
	})
	fresh := repositoryContexts(t, ctx, client, repository)
	owner := contextOwning(t, fresh, policyProbePath)
	if got := conditionStatus(t, ctx, client, owner, specapi.ConditionPolicyCompliant); got != "False" {
		t.Errorf("PolicyCompliant of %s = %q, want False: it owns the violating file", owner, got)
	}
	status = repositoryPolicyStatus(t, ctx, client, repository)
	violations, _ := status["violations"].([]any)
	if len(violations) == 0 {
		t.Fatalf("status violations = %v", status)
	}
}

func contextOwning(t *testing.T, contexts []spec.SystemContext, file string) string {
	t.Helper()
	for _, systemContext := range contexts {
		for _, owned := range systemContext.Status.Observed.Files {
			if owned == file || strings.HasSuffix(owned, "/"+file) {
				return systemContext.Name
			}
		}
		for _, owned := range systemContext.Status.Observed.TreeFiles {
			if owned == file || strings.HasSuffix(owned, "/"+file) {
				return systemContext.Name
			}
		}
	}
	if len(contexts) == 0 {
		t.Fatal("the repository has no context")
	}
	return contexts[0].Name
}

func numberOf(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int64:
		return int(typed)
	case int:
		return typed
	}
	return 0
}

// TestPolicyGateDenyMakesTheAgentComply covers the gate: the first attempt
// writes code a policy denies, the change fails with the deny messages, the
// next attempt complies and lands.
func TestPolicyGateDenyMakesTheAgentComply(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	const repository = "policy-deny"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, examplePolicyLibrary(t))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{repository}, nil)
		forgetPolicies(t, cleanupCtx, client)
	})

	startPolicyController(t, ctx, "", 3)
	scenario := filepath.Join(t.TempDir(), "scenario.yaml")
	writeScenario(t, scenario, scriptedagent.Scenario{})
	repositoryObject := policyRepository(repository, repoPath)
	repositoryObject.Spec.Agent = &spec.AgentSpec{Kind: "scripted:" + scenario}
	applyTyped(t, ctx, client, repositoryObject)

	contexts := waitForContexts(t, ctx, client, repository)
	waitFor(t, ctx, "the first ingest", func() bool {
		for _, systemContext := range contexts {
			if systemContext.Status.RealizedSpecHash != "" {
				return true
			}
		}
		return false
	})
	writeAttemptScenario(t, scenario, contexts, repository)

	owner := contexts[0].Name
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitFor(t, ctx, "the denied attempt", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
		return len(found) >= 1 && found[0].Status.Phase == specapi.PhaseFailed
	})
	denied := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)[0]
	if !strings.Contains(denied.Status.Message, specapi.ReasonPolicyDenied) {
		t.Errorf("message = %q, want %s", denied.Status.Message, specapi.ReasonPolicyDenied)
	}
	if denied.Status.Policy == nil || len(denied.Status.Policy.Denied) == 0 {
		t.Fatalf("the denied change carries no policy status: %+v", denied.Status)
	}
	if !strings.Contains(denied.Status.Policy.Denied[0].Msg, "must not dial a guest directly") {
		t.Errorf("denied message = %q", denied.Status.Policy.Denied[0].Msg)
	}
	if !strings.Contains(denied.Status.AgentLog, "policy gate") {
		t.Errorf("the agent log does not carry the deny messages: %q", denied.Status.AgentLog)
	}

	waitFor(t, ctx, "the complying attempt to land", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseSucceeded {
				return true
			}
		}
		return false
	})
	changes := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
	landed := changes[len(changes)-1]
	if landed.Status.Commit == "" || headOf(t, repoPath) != landed.Status.Commit {
		t.Errorf("commit = %q, HEAD = %q", landed.Status.Commit, headOf(t, repoPath))
	}
	contents := readFileAt(t, landed.Status.Commit, repoPath, policyProbePath)
	if strings.Contains(contents, "Deno.connect") {
		t.Errorf("the landed probe still dials the guest: %s", contents)
	}
}

// TestPolicyGateNeverComplyingEndsPolicyDenied covers the exhausted attempts:
// an agent that writes the same violating probe every time ends Failed with
// reason PolicyDenied.
func TestPolicyGateNeverComplyingEndsPolicyDenied(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	const repository = "policy-stubborn"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, examplePolicyLibrary(t))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{repository}, nil)
		forgetPolicies(t, cleanupCtx, client)
	})

	scenario := filepath.Join(t.TempDir(), "scenario.yaml")
	contexts := startPolicyGateFixture(t, ctx, client, repository, repoPath, scenario, 2, nil)
	writeViolatingScenario(t, scenario, contexts)

	owner := contexts[0].Name
	base := headOf(t, repoPath)
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitFor(t, ctx, "the two denied attempts", func() bool {
		changes := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
		if len(changes) < 2 {
			return false
		}
		newest := changes[len(changes)-1]
		return newest.Status.Phase == specapi.PhaseFailed
	})
	time.Sleep(3 * time.Second)
	changes := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
	newest := changes[len(changes)-1]
	if !strings.Contains(newest.Status.Message, specapi.ReasonPolicyDenied) {
		t.Errorf("message = %q, want %s", newest.Status.Message, specapi.ReasonPolicyDenied)
	}
	if newest.Status.Policy == nil || len(newest.Status.Policy.Denied) == 0 {
		t.Errorf("the exhausted episode carries no deny: %+v", newest.Status.Policy)
	}
	if head := headOf(t, repoPath); head != base {
		t.Errorf("a denied change landed: HEAD = %s, want the base %s", head, base)
	}
}

// waitForIdle holds until another test's realize is not still running: a
// Running SpecToCode change of any repository keeps every repository's
// populate from running.
func waitForIdle(t *testing.T, ctx context.Context, client *kcpclient.Client) {
	t.Helper()
	waitFor(t, ctx, "the changes of the previous test to finish", func() bool {
		for _, change := range liveSpecChanges(t, ctx, client) {
			if change.Status.Phase == specapi.PhaseRunning {
				return false
			}
		}
		return true
	})
}

// startPolicyGateFixture creates the repository with a placeholder scenario,
// waits for the first ingest, and leaves the scenario file for the caller to
// fill in now that the context names are known.
func startPolicyGateFixture(t *testing.T, ctx context.Context, client *kcpclient.Client, repository, repoPath, scenario string, maxAttempts int, overrides []spec.AcceptanceOverride) []spec.SystemContext {
	t.Helper()
	waitForIdle(t, ctx, client)
	startPolicyController(t, ctx, "", maxAttempts)
	writeScenario(t, scenario, scriptedagent.Scenario{})
	object := policyRepository(repository, repoPath)
	object.Spec.AcceptanceOverrides = overrides
	object.Spec.Agent = &spec.AgentSpec{Kind: "scripted:" + scenario}
	applyTyped(t, ctx, client, object)

	contexts := waitForContexts(t, ctx, client, repository)
	waitFor(t, ctx, "the first ingest", func() bool {
		for _, systemContext := range contexts {
			if systemContext.Status.RealizedSpecHash != "" {
				return true
			}
		}
		return false
	})
	return contexts
}

func writeViolatingScenario(t *testing.T, path string, contexts []spec.SystemContext) {
	t.Helper()
	scenario := scriptedagent.Scenario{
		Contexts: map[string]scriptedagent.Draft{},
		Realize:  map[string][]scriptedagent.Step{},
	}
	for _, systemContext := range contexts {
		scenario.Contexts[systemContext.Name] = scriptedagent.Draft{Intent: systemContext.Name}
		scenario.Realize[systemContext.Name] = []scriptedagent.Step{
			{Write: &scriptedagent.Write{Path: policyProbePath, Contents: policyViolatingProbe}},
		}
	}
	writeScenario(t, path, scenario)
}

func readFileAt(t *testing.T, commit, repoPath, file string) string {
	t.Helper()
	if commit == "" {
		t.Fatal("no commit to read")
	}
	return gitOutput(t, repoPath, "show", commit+":"+file)
}

// TestPolicyWarnRecordsWithoutBlocking covers a warn-only constraint: the
// violating change lands, the warn is in the change status, and the persisted
// change record on the open-architecture branch carries it too.
func TestPolicyWarnRecordsWithoutBlocking(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	const repository = "policy-warn"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, withEnforcement(examplePolicyLibrary(t), policy.EnforcementWarn))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{repository}, nil)
		forgetPolicies(t, cleanupCtx, client)
	})

	scenario := filepath.Join(t.TempDir(), "scenario.yaml")
	contexts := startPolicyGateFixture(t, ctx, client, repository, repoPath, scenario, 3, nil)
	writeViolatingScenario(t, scenario, contexts)

	owner := contexts[0].Name
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitFor(t, ctx, "the warned change to land", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseSucceeded {
				return true
			}
		}
		return false
	})
	changes := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
	landed := changes[len(changes)-1]
	if landed.Status.Policy == nil || len(landed.Status.Policy.Warned) == 0 {
		t.Fatalf("the landed change carries no warn: %+v", landed.Status.Policy)
	}
	if len(landed.Status.Policy.Denied) != 0 {
		t.Errorf("denied = %+v, want none", landed.Status.Policy.Denied)
	}
	if headOf(t, repoPath) != landed.Status.Commit {
		t.Errorf("HEAD = %s, want the commit %s", headOf(t, repoPath), landed.Status.Commit)
	}
	record := openArchitectureChange(t, repoPath, repository, landed.Name)
	if record == nil {
		t.Fatal("the change record is not on the open-architecture branch")
	}
	if record.Status.Policy == nil || len(record.Status.Policy.Warned) == 0 {
		t.Errorf("the branch record carries no warn: %+v", record.Status.Policy)
	}
}

// TestPolicyOverrideWaivesADeny covers specctl accept --override policy:<c>:
// a repository override lets the denied change land once and is consumed.
func TestPolicyOverrideWaivesADeny(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	const repository = "policy-override"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, examplePolicyLibrary(t))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{repository}, nil)
		forgetPolicies(t, cleanupCtx, client)
	})

	scenario := filepath.Join(t.TempDir(), "scenario.yaml")
	contexts := startPolicyGateFixture(t, ctx, client, repository, repoPath, scenario, 3, []spec.AcceptanceOverride{{
		Step: "policy:no-direct-guest-connect", Reason: "a migration, tracked in issue 12", By: "operator",
	}})
	writeViolatingScenario(t, scenario, contexts)

	owner := contexts[0].Name
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitFor(t, ctx, "the waived change to land", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseSucceeded {
				return true
			}
		}
		return false
	})
	changes := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
	landed := changes[len(changes)-1]
	if landed.Status.Policy == nil || len(landed.Status.Policy.Waived) == 0 {
		t.Fatalf("the waived change carries no waiver: %+v", landed.Status.Policy)
	}
	if landed.Status.Policy.Denied != nil && len(landed.Status.Policy.Denied) > 0 {
		t.Errorf("denied = %+v, want the override to leave none", landed.Status.Policy.Denied)
	}
	waitFor(t, ctx, "the override to be consumed", func() bool {
		object, err := client.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, repository)
		if err != nil {
			return false
		}
		overrides, _, _ := unstructured.NestedSlice(object.Object, "spec", "acceptanceOverrides")
		return len(overrides) == 0
	})
	if got := conditionStatus(t, ctx, client, owner, specapi.ConditionAcceptanceOverridden); got != "" && got != "True" {
		t.Errorf("AcceptanceOverridden = %q", got)
	}
}

func openArchitectureChange(t *testing.T, repoPath, repository, change string) *spec.SpecChange {
	t.Helper()
	ctx := context.Background()
	store := oagit.Store{Repo: repoPath}
	ref := oabranch.RefFor(repository, "main", store.DefaultBranch(ctx))
	commit, err := store.Tip(ctx, ref)
	if err != nil || commit == "" {
		return nil
	}
	blobs, err := store.ReadFiles(ctx, commit)
	if err != nil {
		return nil
	}
	changes, err := oabranch.ChangeFiles(blobs)
	if err != nil {
		return nil
	}
	for index := range changes {
		if changes[index].Name == change {
			return &changes[index]
		}
	}
	return nil
}
