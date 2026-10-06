package orgrealize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/orggit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/orgfixture"
)

const scenario = `
contexts: {}
realize:
  market-guest:
    - patch:
        path: lib/cloud-init/mod.ts
        find: "report-network --event vm.onNetwork"
        replace: "report-network --event vm.onNetwork --ticket"
  provider-host:
    - write:
        path: lib/compute-provider/ticket.ts
        contents: |
          // The host consumes the ticket the guest reports.
          export const ticketField = "ticket";
`

func setup(t *testing.T) (*orgfixture.Fixture, *orggit.Root, *scriptedagent.Agent) {
	t.Helper()
	f := orgfixture.Build(t, orgfixture.Default())
	root, err := orggit.Open(context.Background(), f.Clone("realize", 0))
	if err != nil {
		t.Fatal(err)
	}
	root.Env = f.Env()
	loaded, err := scriptedagent.LoadBytes([]byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	return f, root, scriptedagent.New(loaded)
}

func twoSteps() Plan {
	return Plan{
		Change: "guest-ticket",
		Body:   "The guest reports a ticket; the host reads it.",
		Steps: []Step{
			{Member: "market", Context: "market-guest"},
			{Member: "hono-compute-provider", Context: "provider-host"},
		},
	}
}

func TestATwoMemberChangeLeavesOneRootCommitThatBumpsBoth(t *testing.T) {
	f, root, stub := setup(t)
	ctx := context.Background()
	headBefore, _ := root.Git(ctx, root.Dir, "rev-parse", "HEAD")

	result, err := Run(ctx, Options{Root: root, Agent: stub, Push: true}, twoSteps())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 2 || len(result.Bumps) != 2 || result.RootCommit == "" || len(result.Unpublished) != 0 {
		t.Fatalf("result = %+v", result)
	}

	// The root log answers "which member commit, under which change".
	log := f.Git(root.Dir, "log", "--format=%s%n%b", "-1")
	if !strings.HasPrefix(log, "bump(market, provider): 2 members") {
		t.Fatalf("root commit = %s", log)
	}
	for _, want := range []string{"Spec-Change: guest-ticket", "Member: market market ", "Member: provider hono-compute-provider "} {
		if !strings.Contains(log, want) {
			t.Fatalf("root commit lacks %q:\n%s", want, log)
		}
	}
	if parent := f.Git(root.Dir, "rev-parse", "HEAD~1"); parent != headBefore {
		t.Fatalf("one root commit expected, parent = %s want %s", parent, headBefore)
	}

	// Each member carries its own commit with the same change trailer, on a
	// change branch that is on the member's remote.
	for _, step := range result.Steps {
		dir := root.MemberDir(org.Member{Path: map[string]string{"market": "market", "provider": "hono-compute-provider"}[step.Member]})
		message := f.Git(dir, "log", "-1", "--format=%B", step.To)
		if !strings.Contains(message, "Spec-Change: guest-ticket") || !strings.Contains(message, "Org-Root: "+root.Name) {
			t.Fatalf("%s commit message:\n%s", step.Member, message)
		}
		if remote := f.Git(dir, "branch", "-r", "--contains", step.To); !strings.Contains(remote, "specd/guest-ticket") {
			t.Fatalf("%s is not on its remote: %q", step.Member, remote)
		}
	}
	data, _ := os.ReadFile(filepath.Join(root.Dir, "market", "lib", "cloud-init", "mod.ts"))
	if !strings.Contains(string(data), "--ticket") {
		t.Fatal("the agent's patch is missing in the member's tree")
	}

	states, _ := root.Members(ctx, orggit.MembersOptions{})
	for _, state := range states {
		if state.Name == "relay" {
			if state.Head != state.CodeCommit {
				t.Fatal("relay was not part of the change and must not move")
			}
			continue
		}
		if state.Head != state.CodeCommit || state.Ahead != 0 {
			t.Fatalf("%s: the pin must equal the checkout after the bump: %+v", state.Name, state)
		}
	}
}

func TestWithoutPushTheResultNamesTheUnpublishedMembers(t *testing.T) {
	_, root, stub := setup(t)
	result, err := Run(context.Background(), Options{Root: root, Agent: stub}, twoSteps())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unpublished) != 2 {
		t.Fatalf("unpublished = %v", result.Unpublished)
	}
	states, _ := root.Members(context.Background(), orggit.MembersOptions{})
	var problems []string
	for _, state := range states {
		for _, p := range org.Problems(state) {
			problems = append(problems, p.Code)
		}
	}
	if !strings.Contains(strings.Join(problems, ","), org.CodeUnpublished) {
		t.Fatalf("status must flag the dangling pointer: %v", problems)
	}
}

func TestAFailingStepRestoresEveryMemberAndLeavesTheRootAlone(t *testing.T) {
	f, root, stub := setup(t)
	ctx := context.Background()
	headBefore := f.Git(root.Dir, "rev-parse", "HEAD")
	marketBefore := f.Git(filepath.Join(root.Dir, "market"), "rev-parse", "HEAD")

	plan := twoSteps()
	plan.Steps[1].Context = "provider-without-a-script"
	_, err := Run(ctx, Options{Root: root, Agent: stub, RootBranch: "specd/guest-ticket"}, plan)
	if err == nil || !strings.Contains(err.Error(), "no realize steps") {
		t.Fatalf("err = %v", err)
	}
	if f.Git(root.Dir, "rev-parse", "HEAD") != headBefore {
		t.Fatal("the root moved")
	}
	if f.Git(root.Dir, "branch", "--list", "specd/guest-ticket") != "" {
		t.Fatal("the root branch was kept")
	}
	market := filepath.Join(root.Dir, "market")
	if got := f.Git(market, "rev-parse", "HEAD"); got != marketBefore {
		t.Fatalf("market HEAD = %s, want %s", got, marketBefore)
	}
	if f.Git(market, "status", "--porcelain") != "" || f.Git(market, "branch", "--list", "specd/guest-ticket") != "" {
		t.Fatal("market keeps the failed step's work")
	}
}

func TestVerifyGatesTheCommit(t *testing.T) {
	f, root, stub := setup(t)
	failing := func(context.Context, org.MemberState, string) error { return errors.New("tests red") }
	_, err := Run(context.Background(), Options{Root: root, Agent: stub, Verify: failing}, twoSteps())
	if err == nil || !strings.Contains(err.Error(), "tests red") {
		t.Fatalf("err = %v", err)
	}
	if f.Git(filepath.Join(root.Dir, "market"), "status", "--porcelain") != "" {
		t.Fatal("market was not restored")
	}
}

func TestPlanIsCheckedBeforeAnythingIsEdited(t *testing.T) {
	f, root, stub := setup(t)
	ctx := context.Background()
	dirty := filepath.Join(root.Dir, "hono-compute-provider", "lib", "compute-provider", "mod.ts")
	if err := os.WriteFile(dirty, []byte("// uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marketBefore := f.Git(filepath.Join(root.Dir, "market"), "rev-parse", "HEAD")
	if _, err := Run(ctx, Options{Root: root, Agent: stub}, twoSteps()); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("err = %v", err)
	}
	if f.Git(filepath.Join(root.Dir, "market"), "rev-parse", "HEAD") != marketBefore ||
		f.Git(filepath.Join(root.Dir, "market"), "branch", "--list", "specd/*") != "" {
		t.Fatal("market was touched before the plan was checked")
	}
	for _, bad := range []Plan{{Change: "x"}, {Steps: twoSteps().Steps}, {Change: "x", Steps: []Step{{Member: "nobody", Context: "c"}}}} {
		if _, err := Run(ctx, Options{Root: root, Agent: stub}, bad); err == nil {
			t.Fatalf("plan %+v must be refused", bad)
		}
	}
}
