package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// policyRequesterPath is the file the policies under test judge: relay-only-ssh
// reaches it from the integration test that drives it, so a violating variant
// of this file is what a policy denial is made of. A test file alone cannot
// violate it: the indexer gives a test file a file node and its imports, not
// the body of Deno.test, so what the test reaches is the production code it
// imports.
const policyRequesterPath = "lib/requester/mod.ts"

// policyContextNames are the directory-partitioned contexts the market-mini
// fixture produces; the tests delete them too, because a context name belongs
// to one repository at a time.
var policyContextNames = []string{
	"hono-bidder", "lib-cloud-init", "lib-compute-provider", "lib-market-common", "lib-requester", "test",
}

// fixtureFile reads a file of a market-mini fixture variant: the violating
// requester dials the guest directly, the compliant one goes over the relay.
func fixtureFile(t *testing.T, variant, file string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "fixtures", "market-mini", variant, file))
	if err != nil {
		t.Fatalf("read the %s fixture %s: %v", variant, file, err)
	}
	return string(data)
}

func violatingRequester(t *testing.T) string {
	t.Helper()
	return fixtureFile(t, "violating", policyRequesterPath)
}

func compliantRequester(t *testing.T) string {
	t.Helper()
	return fixtureFile(t, "compliant", policyRequesterPath)
}

// complyingRequester is the compliant requester plus one line the agent adds:
// a failed realize leaves the worktree at HEAD, so writing the fixture's own
// content again would be no change at all and commit nothing.
func complyingRequester(t *testing.T) string {
	t.Helper()
	return compliantRequester(t) + "\n// Reached over the relay transport the RFP cloud-init deploys.\n"
}

// requesterContext is the context that owns lib/requester: the spec edit that
// makes the agent touch the requester belongs to it, not to a context the
// realize would have to write outside of.
func requesterContext(t *testing.T, ctx context.Context, client *kcpclient.Client, repository string) string {
	t.Helper()
	contexts := repositoryContexts(t, ctx, client, repository)
	names := []string{}
	for _, systemContext := range contexts {
		if systemContext.Name == "lib-requester" {
			return systemContext.Name
		}
		names = append(names, systemContext.Name)
	}
	t.Fatalf("the repository has no lib-requester context; it has %v", names)
	return ""
}

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

// waitForContexts waits for the whole partition of the fixture, not just the
// first context: a test that picks the context owning a file, or that writes a
// scenario step for every context, needs all of them.
func waitForContexts(t *testing.T, ctx context.Context, client *kcpclient.Client, repository string) []spec.SystemContext {
	t.Helper()
	waitForState(t, ctx, "the contexts of "+repository, func() bool {
		return len(repositoryContexts(t, ctx, client, repository)) >= len(policyContextNames)
	}, func() string { return contextNames(repositoryContexts(t, ctx, client, repository)) })
	return repositoryContexts(t, ctx, client, repository)
}

func contextNames(contexts []spec.SystemContext) string {
	names := []string{}
	for _, systemContext := range contexts {
		names = append(names, systemContext.Name)
	}
	sort.Strings(names)
	return fmt.Sprintf("contexts %v", names)
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
// attempt: the first writes the violating requester, the second the compliant
// one. The controller loads the file per realize, so the test may rewrite it
// after the contexts are known.
func writeAttemptScenario(t *testing.T, path string, contexts []spec.SystemContext, repository string) {
	t.Helper()
	violating := violatingRequester(t)
	complying := complyingRequester(t)
	scenario := scriptedagent.Scenario{
		Contexts: map[string]scriptedagent.Draft{},
		Attempts: map[string][][]scriptedagent.Step{},
	}
	for _, systemContext := range contexts {
		scenario.Contexts[systemContext.Name] = scriptedagent.Draft{
			Intent: "the " + systemContext.Name + " context of " + repository,
		}
		scenario.Attempts[systemContext.Name] = [][]scriptedagent.Step{
			{{Write: &scriptedagent.Write{Path: policyRequesterPath, Contents: violating}}},
			{{Write: &scriptedagent.Write{Path: policyRequesterPath, Contents: complying}}},
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
				"id": "r.policy-relay", "level": "MUST",
				"text":     "The integration test reaches the guest only through the relay.",
				"codeRefs": []any{"file:" + policyRequesterPath},
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

// policyState is what a stuck policy wait reports: the policy condition of the
// repository, what kcp holds, and the status of each ConstraintTemplate, so a
// template kcp refused names its reason instead of only timing out.
func policyState(t *testing.T, ctx context.Context, client *kcpclient.Client, repository string) string {
	t.Helper()
	parts := []string{}
	if repository != "" {
		if object, err := client.Get(ctx, specapi.RepositoryGVR, specapi.DefaultNamespace, repository); err == nil {
			conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
			for _, raw := range conditions {
				entry, ok := raw.(map[string]any)
				if !ok || entry["type"] != specapi.ConditionPolicyReady {
					continue
				}
				parts = append(parts, fmt.Sprintf("condition %v=%v (%v): %v",
					entry["type"], entry["status"], entry["reason"], entry["message"]))
			}
			status, _, _ := unstructured.NestedMap(object.Object, "status", "policy")
			parts = append(parts, fmt.Sprintf("policy status %v", status))
		} else {
			parts = append(parts, fmt.Sprintf("repository %s: %v", repository, err))
		}
	}
	library, err := policykcp.Read(ctx, client)
	if err != nil {
		parts = append(parts, fmt.Sprintf("kcp library: %v", err))
		return strings.Join(parts, "; ")
	}
	names := []string{}
	for _, template := range library.Templates {
		names = append(names, template.Name)
	}
	parts = append(parts, fmt.Sprintf("kcp holds templates %v and %d constraint(s)", names, len(library.Constraints)))
	for _, template := range library.Templates {
		object, err := client.GetCluster(ctx, policy.ConstraintTemplateGVR(), template.Name)
		if err != nil {
			parts = append(parts, fmt.Sprintf("%s status: %v", template.Name, err))
			continue
		}
		status, _, _ := unstructured.NestedMap(object.Object, "status")
		parts = append(parts, fmt.Sprintf("%s status %v", template.Name, status))
	}
	return strings.Join(parts, "; ")
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

	library := examplePolicyLibrary(t)
	waitForState(t, ctx, "the policy branch to be restored into kcp", func() bool {
		held, err := policykcp.Read(ctx, client)
		if err != nil {
			return false
		}
		return len(held.Templates) == len(library.Templates) && len(held.Constraints) == len(library.Constraints)
	}, func() string { return policyState(t, ctx, client, repository) })
	held, err := policykcp.Read(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range library.Templates {
		found := false
		for _, got := range held.Templates {
			if got.Name == want.Name && got.Kind == want.Kind {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was not restored into kcp: %v", want.Name, held.Templates)
		}
	}
	for _, constraint := range held.Constraints {
		if constraint.Enforcement != policy.EnforcementDeny {
			t.Errorf("enforcement of %s = %s", constraint.Name, constraint.Enforcement)
		}
	}

	contexts := waitForContexts(t, ctx, client, repository)
	waitForState(t, ctx, "the audit of the repository", func() bool {
		status := repositoryPolicyStatus(t, ctx, client, repository)
		if status == nil || status["evaluatedCommit"] == nil || status["evaluatedCommit"] == "" {
			return false
		}
		for _, systemContext := range contexts {
			if conditionStatus(t, ctx, client, systemContext.Name, specapi.ConditionPolicyCompliant) != "True" {
				return false
			}
		}
		return true
	}, func() string { return policyState(t, ctx, client, repository) })
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

	// The violating requester lands in lib/requester, the context that owns it.
	violating := filepath.Join(repoPath, policyRequesterPath)
	if err := os.WriteFile(violating, []byte(violatingRequester(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.Commit(t, repoPath, "a violating requester")

	waitForState(t, ctx, "the walkthrough audit to flag the violating requester", func() bool {
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
	}, func() string { return policyState(t, ctx, client, repository) })
	fresh := repositoryContexts(t, ctx, client, repository)
	owner := contextOwning(t, fresh, policyRequesterPath)
	if got := conditionStatus(t, ctx, client, owner, specapi.ConditionPolicyCompliant); got != "False" {
		t.Errorf("PolicyCompliant of %s = %q, want False: it owns the violating file", owner, got)
	}
	if owner != "lib-requester" {
		t.Errorf("the violating file belongs to %s, want lib-requester", owner)
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

	owner := requesterContext(t, ctx, client, repository)
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitForState(t, ctx, "the denied attempt", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
		return len(found) >= 1 && found[0].Status.Phase == specapi.PhaseFailed
	}, func() string { return policyState(t, ctx, client, repository) })
	denied := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)[0]
	if !strings.Contains(denied.Status.Message, specapi.ReasonPolicyDenied) {
		t.Errorf("message = %q, want %s", denied.Status.Message, specapi.ReasonPolicyDenied)
	}
	if denied.Status.Policy == nil || len(denied.Status.Policy.Denied) == 0 {
		t.Fatalf("the denied change carries no policy status: %+v", denied.Status)
	}
	denyMessages := []string{}
	for _, violation := range denied.Status.Policy.Denied {
		denyMessages = append(denyMessages, violation.Msg)
	}
	if !mentionsDeny(denyMessages, "dials a guest address directly") {
		t.Errorf("denied messages = %v, want the relay-only-ssh direct dial", denyMessages)
	}
	if !strings.Contains(denied.Status.AgentLog, "policy gate") {
		t.Errorf("the agent log does not carry the deny messages: %q", denied.Status.AgentLog)
	}

	waitForState(t, ctx, "the complying attempt to land", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseSucceeded {
				return true
			}
		}
		return false
	}, func() string { return policyState(t, ctx, client, repository) })
	changes := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
	landed := changes[len(changes)-1]
	if landed.Status.Commit == "" || headOf(t, repoPath) != landed.Status.Commit {
		t.Errorf("commit = %q, HEAD = %q", landed.Status.Commit, headOf(t, repoPath))
	}
	// gitOutput trims, so the trailing newline is compared trimmed too.
	contents := readFileAt(t, landed.Status.Commit, repoPath, policyRequesterPath)
	if contents != strings.TrimSpace(complyingRequester(t)) {
		t.Errorf("the landed requester is not the complying one:\n%s", contents)
	}
}

func mentionsDeny(messages []string, fragment string) bool {
	for _, message := range messages {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
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

	owner := requesterContext(t, ctx, client, repository)
	base := headOf(t, repoPath)
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitForState(t, ctx, "the two denied attempts", func() bool {
		changes := changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode)
		if len(changes) < 2 {
			return false
		}
		newest := changes[len(changes)-1]
		return newest.Status.Phase == specapi.PhaseFailed
	}, func() string { return policyState(t, ctx, client, repository) })
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

// writeViolatingScenario makes every realize write the violating requester, so
// the gate denies every attempt no matter which context is realized.
func writeViolatingScenario(t *testing.T, path string, contexts []spec.SystemContext) {
	t.Helper()
	violating := violatingRequester(t)
	scenario := scriptedagent.Scenario{
		Contexts: map[string]scriptedagent.Draft{},
		Realize:  map[string][]scriptedagent.Step{},
	}
	for _, systemContext := range contexts {
		scenario.Contexts[systemContext.Name] = scriptedagent.Draft{Intent: systemContext.Name}
		scenario.Realize[systemContext.Name] = []scriptedagent.Step{
			{Write: &scriptedagent.Write{Path: policyRequesterPath, Contents: violating}},
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

	owner := requesterContext(t, ctx, client, repository)
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitForState(t, ctx, "the warned change to land", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseSucceeded {
				return true
			}
		}
		return false
	}, func() string { return policyState(t, ctx, client, repository) })
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
	var record *spec.SpecChange
	waitFor(t, ctx, "the change record to reach the branch with the warn", func() bool {
		record = openArchitectureChange(t, repoPath, repository, landed.Name)
		return record != nil && record.Status.Policy != nil && len(record.Status.Policy.Warned) > 0
	})
	if record == nil {
		t.Fatal("the change record is not on the open-architecture branch")
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
		Step: "policy:relay-only-ssh", Reason: "a migration, tracked in issue 12", By: "operator",
	}})
	writeViolatingScenario(t, scenario, contexts)

	owner := requesterContext(t, ctx, client, repository)
	applySpecEdit(t, ctx, client, specEditFor(owner, repository))

	waitForState(t, ctx, "the waived change to land", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), owner, specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseSucceeded {
				return true
			}
		}
		return false
	}, func() string { return policyState(t, ctx, client, repository) })
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

// TestPolicyCliDrivesKcp covers the kcp side of the CLI: apply a library,
// list what kcp holds, and restore the policy branch.
func TestPolicyCliDrivesKcp(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	specctl, _ := buildSpecctlAndSpecd(t)

	const repository = "policy-cli"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, examplePolicyLibrary(t))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, []string{repository}, policyContextNames)
		forgetPolicies(t, cleanupCtx, client)
	})

	example := examplePolicyLibrary(t)
	empty := runSpecctl(t, ctx, specctl, repoPath, nil, "policy", "ls")
	if !strings.Contains(empty, "TEMPLATE") {
		t.Fatalf("policy ls = %q", empty)
	}
	if strings.Contains(empty, "relayonlyssh") {
		t.Fatalf("kcp already holds the template: %q", empty)
	}

	restored := runSpecctl(t, ctx, specctl, repoPath, nil,
		"policy", "restore", "--repo", repository, "--path", repoPath)
	if !strings.Contains(restored, fmt.Sprintf("restored %d template(s) and %d constraint(s)",
		len(example.Templates), len(example.Constraints))) {
		t.Errorf("policy restore = %q", restored)
	}
	listed := runSpecctl(t, ctx, specctl, repoPath, nil, "policy", "ls")
	if !strings.Contains(listed, "relayonlyssh") ||
		!strings.Contains(listed, "relay-only-ssh(deny)") {
		t.Errorf("policy ls after restore = %q", listed)
	}

	forgetPolicies(t, ctx, client)
	applied := runSpecctl(t, ctx, specctl, root, nil,
		"policy", "apply", "--library", filepath.Join(root, "examples", "policies", "market-mini"))
	if !strings.Contains(applied, fmt.Sprintf("%d template(s), %d constraint(s) applied",
		len(example.Templates), len(example.Constraints))) {
		t.Errorf("policy apply = %q", applied)
	}
	library, err := policykcp.Read(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Templates) != len(example.Templates) || len(library.Constraints) != len(example.Constraints) {
		t.Errorf("kcp holds %d template(s) and %d constraint(s)", len(library.Templates), len(library.Constraints))
	}
}

// TestPolicyLibrariesApplyWholeIntoKcp restores the three real libraries, the
// market-mini twin, the atproto-market examples and the ported opa-first-stab
// library, and asserts every template's constraint CRD is established and
// every constraint is applied: a template kcp refuses must fail here with the
// reason, not leave a half-applied library behind.
func TestPolicyLibrariesApplyWholeIntoKcp(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	forgetPolicies(t, ctx, client)
	forgetConstraintCRDs(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetPolicies(t, cleanupCtx, client)
		forgetConstraintCRDs(t, cleanupCtx, client)
	})

	for _, restored := range []struct {
		dir        string
		repository string
	}{
		{dir: filepath.Join("examples", "policies", "market-mini"), repository: "policy-market-mini"},
		{dir: filepath.Join("examples", "policies", "atproto-market"), repository: "policy-atproto-market"},
		{dir: filepath.Join("policies", "library"), repository: "policy-library"},
	} {
		dir := restored.dir
		loaded, err := policyeval.Load(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("load %s: %v", dir, err)
		}
		if len(loaded.Templates) == 0 {
			t.Fatalf("%s holds no template", dir)
		}
		// Restore the branch the way the repository reconcile does: seed the
		// library onto its policy branch and read it back out of git.
		repoPath := policyMarketMini(t, restored.repository)
		store := oagit.Store{Repo: repoPath}
		seedPolicyBranch(t, repoPath, restored.repository, loaded)
		ref := policy.RefFor(restored.repository, "main", store.DefaultBranch(ctx))
		library, _, err := policygit.Read(ctx, store, ref)
		if err != nil {
			t.Fatalf("read the %s policy branch: %v", restored.repository, err)
		}
		if len(library.Templates) != len(loaded.Templates) {
			t.Fatalf("%s: the branch holds %d template(s), want %d", dir, len(library.Templates), len(loaded.Templates))
		}
		if err := policykcp.Apply(ctx, client, library, policykcp.ApplyOptions{}); err != nil {
			t.Fatalf("restore %s: %v\n%s", dir, err, policyState(t, ctx, client, restored.repository))
		}
		for _, template := range library.Templates {
			if err := constraintCRDEstablished(ctx, client, template); err != nil {
				t.Errorf("%s: template %s: %v", dir, template.Name, err)
			}
			status := templateStatus(t, ctx, client, template.Name)
			if created, _, _ := unstructured.NestedBool(status, "created"); !created {
				t.Errorf("%s: template %s reports created=%v: %v", dir, template.Name, created, status)
			}
		}
		held, err := policykcp.Read(ctx, client)
		if err != nil {
			t.Fatal(err)
		}
		for _, constraint := range library.Constraints {
			found := false
			for _, got := range held.Constraints {
				if got.Name == constraint.Name && got.Kind == constraint.Kind {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: constraint %s (%s) is not in kcp", dir, constraint.Name, constraint.Kind)
			}
		}
	}
}

// templateStatus is the simplified byPod status of a ConstraintTemplate.
func templateStatus(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) map[string]any {
	t.Helper()
	object, err := client.GetCluster(ctx, policy.ConstraintTemplateGVR(), name)
	if err != nil {
		t.Fatalf("get template %s: %v", name, err)
	}
	status, _, _ := unstructured.NestedMap(object.Object, "status")
	return status
}

// constraintCRDEstablished fails while the constraint CRD of a template is
// missing or not Established, and names the CRD's own condition when it is.
func constraintCRDEstablished(ctx context.Context, client *kcpclient.Client, template policy.Template) error {
	crd, err := client.GetCluster(ctx, constraintCRDGVR, policy.ConstraintCRDName(template.Kind))
	if err != nil {
		return err
	}
	conditions, _, _ := unstructured.NestedSlice(crd.Object, "status", "conditions")
	for _, raw := range conditions {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if entry["type"] != "Established" {
			continue
		}
		if entry["status"] == "True" {
			return nil
		}
		return fmt.Errorf("the CRD %s is not established: %v", crd.GetName(), conditions)
	}
	return fmt.Errorf("the CRD %s has no Established condition: %v", crd.GetName(), conditions)
}

var constraintCRDGVR = schema.GroupVersionResource{
	Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions",
}

// forgetConstraintCRDs deletes the constraint CRDs of every ConstraintTemplate
// kcp holds, so a later test starts from a kcp that serves no constraint kind.
func forgetConstraintCRDs(t *testing.T, ctx context.Context, client *kcpclient.Client) {
	t.Helper()
	listed, err := client.ListCluster(ctx, policy.ConstraintTemplateGVR())
	if err != nil {
		return
	}
	for index := range listed.Items {
		kind, found, err := unstructured.NestedString(listed.Items[index].Object, "spec", "crd", "spec", "names", "kind")
		if err != nil || !found {
			continue
		}
		if err := client.DeleteCluster(ctx, constraintCRDGVR, policy.ConstraintCRDName(kind)); err != nil {
			t.Logf("forget the constraint CRD of %s: %v", kind, err)
		}
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
