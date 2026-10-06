package policyeval_test

import (
	"context"
	"encoding/json"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orggit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orgrealize"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/orgfixture"
)

func orgLibrary(t *testing.T) policy.Library {
	t.Helper()
	library, err := policyeval.Load(filepath.Join(root, "examples", "policies", "orgroot-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func TestSubmodulesBecomeMembersPinnedByTheGitlink(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("members", 0)
	library := orgLibrary(t)
	ctx := context.Background()

	members, paths, err := policyeval.EffectiveMembers(ctx, library, clone, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	byName := map[string]policy.Member{}
	for _, member := range members {
		names = append(names, member.Name)
		byName[member.Name] = member
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "market,provider,relay" {
		t.Fatalf("members = %v", names)
	}
	root := &orggit.Root{Dir: clone, Name: library.Manifest.Repository}
	states, _ := root.Members(ctx, orggit.MembersOptions{})
	for _, state := range states {
		member := byName[state.Name]
		if member.Ref != state.CodeCommit || member.URL != state.URL {
			t.Fatalf("%s: ref %s url %s, want the gitlink %s %s", state.Name, member.Ref, member.URL, state.CodeCommit, state.URL)
		}
		if paths[state.Name] != filepath.Join(clone, state.Path) {
			t.Fatalf("%s: path %q, the submodule checkout is already there", state.Name, paths[state.Name])
		}
	}
	if len(byName["provider"].Roles) != 1 || byName["market"].Roles["guest"].Globs[0] != "lib/cloud-init/**" {
		t.Fatalf("the library's roles must survive the derivation: %+v", byName)
	}

	// Without the switch the list is the library's own.
	plain := library
	manifest := plain.Manifest
	manifest.Submodules = nil
	plain.Manifest = manifest
	explicit, _, err := policyeval.EffectiveMembers(ctx, plain, clone, nil)
	if err != nil || len(explicit) != len(library.Manifest.Members) {
		t.Fatalf("explicit = %v %v", explicit, err)
	}
}

func TestUninitializedSubmodulesStillBecomeMembersByURL(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	dir := filepath.Join(f.Dir, "clones", "shallowroot", f.Spec.Root)
	f.Git(f.Dir, "clone", "-q", f.RemoteURL(f.Spec.Root), dir)
	members, paths, err := policyeval.EffectiveMembers(context.Background(), orgLibrary(t), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 || len(paths) != 0 {
		t.Fatalf("members %d, paths %v: no checkout, so the url is the source", len(members), paths)
	}
	for _, member := range members {
		if member.URL == "" || member.Ref == "" {
			t.Fatalf("%+v", member)
		}
	}
}

func TestRollupEvaluatesEachMembersOwnPolicyAndNeverMergesThem(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("rollup", 0)
	library := orgLibrary(t)
	seen := map[string]string{}
	rows, err := policyeval.MemberRollup(context.Background(), library, clone,
		func(_ context.Context, member policyeval.MemberEvaluation) (policy.Report, error) {
			seen[member.State.Name] = member.PolicyRef
			if member.State.Name == "provider" {
				return policy.Report{}, context.DeadlineExceeded
			}
			return policy.Report{Totals: map[policy.Enforcement]int{policy.EnforcementDeny: 1}, Violations: []policy.Violation{{Constraint: "c"}}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "provider" || rows[1].Name != "market" {
		t.Fatalf("rows = %+v: relay has no policy branch and gets no row", rows)
	}
	if rows[1].Totals[policy.EnforcementDeny] != 1 || rows[1].PolicyCommit == "" || rows[1].Commit == "" {
		t.Fatalf("market row = %+v", rows[1])
	}
	if rows[0].Message == "" || rows[0].Violations != nil {
		t.Fatalf("a failed member is a row with a message: %+v", rows[0])
	}
	if !strings.Contains(seen["market"], "open-policy/market") {
		t.Fatalf("policy ref = %q", seen["market"])
	}

	off := library
	manifest := off.Manifest
	manifest.Submodules = &policy.Submodules{Members: true}
	off.Manifest = manifest
	if rows, err := policyeval.MemberRollup(context.Background(), off, clone, nil); rows != nil || err != nil {
		t.Fatalf("policies: false means no rollup: %v %v", rows, err)
	}
	_ = org.StateResolved
}

// textGraph is a CodeGraph made of file texts only. The effect classifiers read
// source text, so this is enough to classify a member's code without the
// codegraph indexer.
func textGraph(t *testing.T, name, dir string) policy.CodeGraph {
	t.Helper()
	graph := policy.CodeGraph{
		APIVersion: policy.APIVersion,
		Kind:       policy.CodeGraphKind,
		Metadata:   policy.ObjectMeta{Name: name},
		Spec:       policy.CodeGraphSpec{Repository: name, Texts: map[string]string{}},
	}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ts") {
			return nil
		}
		relative, _ := filepath.Rel(dir, path)
		relative = filepath.ToSlash(relative)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		graph.Spec.Files = append(graph.Spec.Files, policy.CodeGraphFile{Path: relative, Language: "typescript"})
		graph.Spec.Texts[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.Sort()
	return graph
}

func orgModel(t *testing.T, library policy.Library, clone string) policy.ArchitectureModel {
	t.Helper()
	ctx := context.Background()
	members, paths, err := policyeval.EffectiveMembers(ctx, library, clone, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A library with no submodules switch lists its members itself; their
	// checkouts are still the submodules of the clone.
	states, err := (&orggit.Root{Dir: clone}).Members(ctx, orggit.MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{}
	for _, state := range states {
		dirs[state.Name] = filepath.Join(clone, state.Path)
	}
	binding := library.Manifest.Binding()
	var models []policy.ModelMember
	for _, member := range members {
		dir := paths[member.Name]
		if dir == "" {
			dir = dirs[member.Name]
		}
		graph := textGraph(t, member.Name, dir)
		computed, err := effects.Apply(&graph, effects.Options{IncludeExtras: true})
		if err != nil {
			t.Fatal(err)
		}
		models = append(models, policy.ModelMember{
			Name: member.Name, Graph: graph, Effects: computed,
			Binding: binding.Merge(policy.Binding{Roles: member.Roles}),
		})
	}
	rootGraph := textGraph(t, library.Manifest.Repository, clone)
	rootGraph.Spec.Files = nil // the root's own tree: no member code, no .ts of its own that matters
	model, err := policy.BuildModel(policy.ModelInput{
		Repository: library.Manifest.Repository, Graph: rootGraph, Binding: binding, Members: models,
	})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func evaluateModel(t *testing.T, library policy.Library, model policy.ArchitectureModel) policy.Report {
	t.Helper()
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	object, err := policy.Unstructured(data)
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyeval.Evaluate(context.Background(), policyeval.Evaluation{
		Library: library, Repository: library.Manifest.Repository,
		Reviewed: []*unstructured.Unstructured{object}, Inventory: []*unstructured.Unstructured{object},
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func violationsOf(report policy.Report) []string {
	out := []string{}
	for _, violation := range report.Violations {
		out = append(out, violation.Constraint)
	}
	sort.Strings(out)
	return out
}

func TestTheCombinedModelOfTheFixtureIsClean(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	library := orgLibrary(t)
	model := orgModel(t, library, f.Clone("combined", 0))

	flows := model.FlowsWhere(policy.FlowFilter{From: "guest", To: "host"})
	if len(flows) != 1 || len(flows[0].Evidence) != 1 || !strings.HasPrefix(flows[0].Evidence[0], "market/") {
		t.Fatalf("guest -> host flows = %+v: the guest's report is evidence in the market repository", flows)
	}
	if got := violationsOf(evaluateModel(t, library, model)); len(got) != 0 {
		t.Fatalf("violations = %v", got)
	}
}

// TestAMemberAloneCannotSeeTheOtherSidesFlow is proof B of plan 0009 in an org
// root: the provider on its own has no guest that reports, because the guest is
// written in the market. The root's combined model has both, so the same pack
// is satisfied there and only there.
func TestAMemberAloneCannotSeeTheOtherSidesFlow(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("alone", 0)
	library := orgLibrary(t)

	alone := library
	manifest := alone.Manifest
	manifest.Submodules = nil
	manifest.Members = []policy.Member{{Name: "provider", Roles: library.Manifest.Members[1].Roles}}
	alone.Manifest = manifest
	model := orgModel(t, alone, clone)
	got := violationsOf(evaluateModel(t, alone, model))
	if len(got) != 1 || got[0] != "rfp-guest-reports-network" {
		t.Fatalf("provider alone: %v, want the guest-report require", got)
	}
	if combined := violationsOf(evaluateModel(t, library, orgModel(t, library, clone))); len(combined) != 0 {
		t.Fatalf("combined: %v", combined)
	}
}

const reachInScenario = `
contexts: {}
realize:
  provider-host:
    - write:
        path: lib/compute-provider/inspect.ts
        contents: |
          // Discover the guest's address the way the old provider did.
          export async function inspectIp(vmName: string): Promise<string> {
            const command = new Deno.Command("container", { args: ["inspect", vmName] });
            const { stdout } = await command.output();
            return new TextDecoder().decode(stdout);
          }
`

// TestAnAgentChangeInOneRepositoryIsDeniedAtTheRoot runs the stub agent from
// the org root: it edits the provider, the root records the pointer, and the
// root's policy (the combined model) denies the reach-in the edit added.
func TestAnAgentChangeInOneRepositoryIsDeniedAtTheRoot(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("agent", 0)
	library := orgLibrary(t)
	ctx := context.Background()

	root, err := orggit.Open(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	root.Env = f.Env()
	loaded, err := scriptedagent.LoadBytes([]byte(reachInScenario))
	if err != nil {
		t.Fatal(err)
	}
	result, err := orgrealize.Run(ctx, orgrealize.Options{Root: root, Agent: scriptedagent.New(loaded)}, orgrealize.Plan{
		Change: "discover-ip",
		Steps:  []orgrealize.Step{{Member: "hono-compute-provider", Context: "provider-host"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bumps) != 1 || result.Bumps[0].Member != "provider" {
		t.Fatalf("bumps = %+v", result.Bumps)
	}

	report := evaluateModel(t, library, orgModel(t, library, clone))
	got := violationsOf(report)
	if len(got) != 1 || got[0] != "rfp-host-reach-in" {
		t.Fatalf("violations = %v, want the host reach-in", got)
	}
	site := report.Violations[0].Msg
	if !strings.Contains(site, "provider/lib/compute-provider/inspect.ts") {
		t.Fatalf("the finding must name the file in the member: %s", site)
	}
}
