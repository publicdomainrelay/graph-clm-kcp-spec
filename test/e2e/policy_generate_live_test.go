package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policykcp"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"sigs.k8s.io/yaml"
)

// generationTree reads a directory of a scripted generation tree, so the
// scenario carries the files the harness would have written.
func generationTree(t *testing.T, dir string) []scriptedagent.GeneratedFile {
	t.Helper()
	files := []scriptedagent.GeneratedFile{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, scriptedagent.GeneratedFile{Path: filepath.ToSlash(relative), Contents: string(data)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	if len(files) == 0 {
		t.Fatalf("%s holds no file", dir)
	}
	return files
}

func writeGenerationScenario(t *testing.T, field string, attempts []string) string {
	t.Helper()
	generations := [][]scriptedagent.Generation{}
	for _, dir := range attempts {
		generations = append(generations, []scriptedagent.Generation{{
			Summary: "the scripted harness wrote " + filepath.Base(dir),
			Files:   generationTree(t, dir),
		}})
	}
	scenario := scriptedagent.Scenario{}
	if field == "bindings" {
		scenario.Bindings = generations
	} else {
		scenario.Policies = generations
	}
	data, err := yaml.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func policyTestdata(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{repoRoot(t), "test", "e2e", "testdata"}, parts...)...)
}

func livePolicyChange(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) *policy.PolicyChange {
	t.Helper()
	object, err := client.Get(ctx, specapi.PolicyChangeGVR, specapi.DefaultNamespace, name)
	if err != nil {
		return nil
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil
	}
	change, ok := typed.(*policy.PolicyChange)
	if !ok {
		return nil
	}
	return change
}

func policyChangeState(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) string {
	t.Helper()
	change := livePolicyChange(t, ctx, client, name)
	if change == nil {
		return "no PolicyChange " + name
	}
	parts := []string{"phase " + change.Status.Phase}
	for _, check := range change.Status.Checks {
		state := "ok"
		if !check.Passed {
			state = "FAIL"
		}
		parts = append(parts, state+" "+check.Name)
	}
	if change.Status.Message != "" {
		parts = append(parts, change.Status.Message)
	}
	return strings.Join(parts, "; ")
}

// TestPolicyGenerateLive drives a generated policy through a private kcp: the
// harness authors a template that the mutation check refuses on the first
// attempt, the feedback reaches the second attempt, and the accepted policy
// lands on the policy branch and in kcp.
func TestPolicyGenerateLive(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	const repository = "policy-gen"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, examplePolicyLibrary(t))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	forgetPolicyChanges(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetPolicyChanges(t, cleanupCtx, client)
		forgetObjects(t, cleanupCtx, client, []string{repository}, nil)
		forgetPolicies(t, cleanupCtx, client)
	})

	scenario := writeGenerationScenario(t, "policies", []string{
		policyTestdata(t, "policy-generate", "bad"),
		policyTestdata(t, "policy-generate", "good"),
	})
	startPolicyController(t, ctx, "scripted:"+scenario, 3)
	applyTyped(t, ctx, client, policyRepository(repository, repoPath))
	waitForContexts(t, ctx, client, repository)

	const name = "policy-gen-relay-only"
	change := &policy.PolicyChange{
		Spec: policy.PolicyChangeSpec{
			Repository:        repository,
			Slug:              "relay-only",
			Prompt:            "integration tests with bidder and requester MUST always make ssh connections over the relay",
			Requirements:      []string{"lib-requester#r.relay"},
			EnforcementAction: policy.EnforcementDeny,
		},
	}
	change.Name = name
	change.SetDefaults()
	applyTyped(t, ctx, client, change)

	waitForState(t, ctx, "the PolicyChange to be evaluated", func() bool {
		held := livePolicyChange(t, ctx, client, name)
		return held != nil && held.Status.Phase == policy.PolicyPhaseEvaluated
	}, func() string { return policyChangeState(t, ctx, client, name) })

	evaluated := livePolicyChange(t, ctx, client, name)
	if evaluated.Status.Attempt != 2 {
		t.Errorf("the accepted attempt is recorded as %d, want 2: the first attempt must be refused", evaluated.Status.Attempt)
	}
	if evaluated.Status.Template != "relayonly" || len(evaluated.Status.Constraints) != 1 || evaluated.Status.Constraints[0] != "relay-only" {
		t.Errorf("the evaluated change holds template %q and constraints %v", evaluated.Status.Template, evaluated.Status.Constraints)
	}
	if evaluated.Status.Tests == nil || evaluated.Status.Tests.Passed == 0 || evaluated.Status.Tests.Failed != 0 {
		t.Errorf("the recorded tests are %+v", evaluated.Status.Tests)
	}
	for _, check := range evaluated.Status.Checks {
		if !check.Passed {
			t.Errorf("the accepted change carries the failed check %s: %s", check.Name, check.Message)
		}
	}
	if mutation := checkOf(evaluated.Status.Checks, "mutation"); mutation == nil || !strings.Contains(mutation.Message, "unrelayed-ssh") {
		t.Errorf("the mutation check says %+v, want a denied unrelayed-ssh", mutation)
	}

	// The feedback of the refused attempt is the next attempt's agent log.
	if !strings.Contains(evaluated.Status.AgentLog, "mutation") {
		t.Errorf("the recorded agent log does not carry the refused attempt: %q", evaluated.Status.AgentLog)
	}

	// The CLI lists it, and `policy accept` applies it.
	specctl, _ := buildSpecctlAndSpecd(t)
	listed := runSpecctl(t, ctx, specctl, repoPath, nil, "policy", "changes", "--repo", repository)
	if !strings.Contains(listed, name) || !strings.Contains(listed, policy.PolicyPhaseEvaluated) {
		t.Errorf("policy changes = %q", listed)
	}
	if held := runSpecctl(t, ctx, specctl, repoPath, nil, "policy", "ls"); !strings.Contains(held, name) {
		t.Errorf("policy ls does not list the change: %q", held)
	}
	accepted := runSpecctl(t, ctx, specctl, repoPath, nil, "policy", "accept", name, "--timeout", "3m")
	if !strings.Contains(accepted, policy.PolicyPhaseApplied) {
		t.Errorf("policy accept = %q", accepted)
	}
	waitForState(t, ctx, "the PolicyChange to be applied", func() bool {
		held := livePolicyChange(t, ctx, client, name)
		return held != nil && held.Status.Phase == policy.PolicyPhaseApplied
	}, func() string { return policyChangeState(t, ctx, client, name) })
	applied := livePolicyChange(t, ctx, client, name)
	if applied.Status.PolicyCommit == "" {
		t.Error("the applied change names no policy commit")
	}

	// The branch carries the template, its suite, its tests and the record.
	store := oagit.Store{Repo: repoPath}
	library, commit, err := policygit.Read(ctx, store, policy.RefFor(repository, "main", store.DefaultBranch(ctx)))
	if err != nil {
		t.Fatal(err)
	}
	// The branch may move on after the apply (the sync and the audit report
	// commit), so the change's commit is an ancestor of the tip, not the tip.
	if err := exec.Command("git", "-C", repoPath, "merge-base", "--is-ancestor",
		applied.Status.PolicyCommit, commit).Run(); err != nil {
		t.Errorf("the branch at %s does not carry the change's commit %s: %v", commit, applied.Status.PolicyCommit, err)
	}
	if _, ok := library.Files[policy.TemplateSourcePath("relay-only")]; !ok {
		t.Error("the branch carries no templates/relay-only/src.rego")
	}
	if _, ok := library.Files[policy.SuitePath("relay-only")]; !ok {
		t.Error("the branch carries no suite for the generated policy")
	}
	if _, ok := library.Files[policy.ChangePath(name)]; !ok {
		t.Error("the branch carries no changes/" + name + ".yaml record")
	}
	template, ok := library.Template("relayonly")
	if !ok {
		t.Fatal("the branch library holds no relayonly template")
	}
	if len(template.Requirements) != 1 || template.Requirements[0] != "lib-requester#r.relay" {
		t.Errorf("the template requirements are %v", template.Requirements)
	}
	if template.GeneratedBy != name {
		t.Errorf("the template records generated-by %q, want %q", template.GeneratedBy, name)
	}

	// kcp holds the constraint, and the spec says which policy guards it.
	waitForState(t, ctx, "the generated constraint in kcp", func() bool {
		held, err := policykcp.Read(ctx, client)
		if err != nil {
			return false
		}
		for _, constraint := range held.Constraints {
			if constraint.Name == "relay-only" {
				return true
			}
		}
		return false
	}, func() string { return policyState(t, ctx, client, repository) })

	waitForState(t, ctx, "the context to record its enforcing policy", func() bool {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "lib-requester")
		if err != nil {
			return false
		}
		enforced, _, _ := unstructured.NestedStringSlice(object.Object, "status", "enforcedBy")
		for _, templateName := range enforced {
			if templateName == "relayonly" {
				return true
			}
		}
		return false
	}, func() string {
		object, err := client.Get(ctx, specapi.SystemContextGVR, specapi.DefaultNamespace, "lib-requester")
		if err != nil {
			return err.Error()
		}
		enforced, _, _ := unstructured.NestedStringSlice(object.Object, "status", "enforcedBy")
		return "enforcedBy " + strings.Join(enforced, ",")
	})
}

// TestPolicyBindLive drives a generated binding: the harness proposes the
// roles and vocabulary of policies.yaml, the checks accept it and the applied
// binding keeps the pack import.
func TestPolicyBindLive(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	const repository = "policy-bind"
	repoPath := policyMarketMini(t, repository)
	seedPolicyBranch(t, repoPath, repository, examplePolicyLibrary(t))
	forgetObjects(t, ctx, client, []string{repository}, policyContextNames)
	forgetPolicies(t, ctx, client)
	forgetPolicyChanges(t, ctx, client)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetPolicyChanges(t, cleanupCtx, client)
		forgetObjects(t, cleanupCtx, client, []string{repository}, nil)
		forgetPolicies(t, cleanupCtx, client)
	})

	scenario := writeGenerationScenario(t, "bindings", []string{
		policyTestdata(t, "policy-bind", "bad"),
		policyTestdata(t, "policy-bind", "good"),
	})
	startPolicyController(t, ctx, "scripted:"+scenario, 3)
	applyTyped(t, ctx, client, policyRepository(repository, repoPath))
	waitForContexts(t, ctx, client, repository)

	const name = "policy-bind-rfp-guest-isolation"
	change := &policy.PolicyChange{
		Spec: policy.PolicyChangeSpec{
			Repository:  repository,
			Slug:        "rfp-guest-isolation",
			Pack:        "rfp-guest-isolation",
			PackVersion: "v1",
			Prompt:      "bind the pack to the repository",
		},
	}
	change.Name = name
	change.SetDefaults()
	applyTyped(t, ctx, client, change)

	waitForState(t, ctx, "the binding to be evaluated", func() bool {
		held := livePolicyChange(t, ctx, client, name)
		return held != nil && held.Status.Phase == policy.PolicyPhaseEvaluated
	}, func() string { return policyChangeState(t, ctx, client, name) })

	evaluated := livePolicyChange(t, ctx, client, name)
	if evaluated.Status.Attempt != 2 {
		t.Errorf("the accepted attempt is recorded as %d, want 2", evaluated.Status.Attempt)
	}
	if evaluated.Status.Binding == nil || len(evaluated.Status.Binding.Roles) == 0 {
		t.Fatalf("the evaluated binding carries no roles: %+v", evaluated.Status.Binding)
	}
	for _, check := range evaluated.Status.Checks {
		if !check.Passed {
			t.Errorf("the accepted binding carries the failed check %s: %s", check.Name, check.Message)
		}
	}
	if mutation := checkOf(evaluated.Status.Checks, "mutation"); mutation == nil || mutation.Message == "" {
		t.Errorf("the binding mutation check says %+v", mutation)
	}

	evaluated.Spec.Apply = true
	applyTyped(t, ctx, client, evaluated)
	waitForState(t, ctx, "the binding to be applied", func() bool {
		held := livePolicyChange(t, ctx, client, name)
		return held != nil && held.Status.Phase == policy.PolicyPhaseApplied
	}, func() string { return policyChangeState(t, ctx, client, name) })

	store := oagit.Store{Repo: repoPath}
	library, _, err := policygit.Read(ctx, store, policy.RefFor(repository, "main", store.DefaultBranch(ctx)))
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Manifest.Imports) != 1 || library.Manifest.Imports[0].Pack != "rfp-guest-isolation" {
		t.Errorf("the applied manifest imports %+v", library.Manifest.Imports)
	}
	if len(library.Manifest.Roles) == 0 {
		t.Error("the applied manifest declares no role")
	}
	if library.Manifest.Vocabulary == nil || len(library.Manifest.Vocabulary.Channels) == 0 {
		t.Error("the applied manifest declares no vocabulary")
	}
	if _, ok := library.Files[policy.ChangePath(name)]; !ok {
		t.Error("the branch carries no changes/" + name + ".yaml record")
	}
}

func checkOf(checks []policy.Check, name string) *policy.Check {
	for index := range checks {
		if checks[index].Name == name {
			return &checks[index]
		}
	}
	return nil
}

func forgetPolicyChanges(t *testing.T, ctx context.Context, client *kcpclient.Client) {
	t.Helper()
	listed, err := client.List(ctx, specapi.PolicyChangeGVR, specapi.DefaultNamespace)
	if err != nil {
		return
	}
	for index := range listed.Items {
		_ = client.Delete(ctx, specapi.PolicyChangeGVR, specapi.DefaultNamespace, listed.Items[index].GetName())
	}
}
