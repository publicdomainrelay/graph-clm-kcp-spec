package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/orgfixture"
)

// orgEnv makes the fixture's git identity and protocol settings the process's,
// because the command reads them the way a person's shell provides them.
func orgEnv(t *testing.T, f *orgfixture.Fixture) {
	t.Helper()
	for _, pair := range f.Env() {
		key, value, _ := strings.Cut(pair, "=")
		t.Setenv(key, value)
	}
}

func specctl(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestOrgLsStatusAndBriefOnAFreshRecursiveClone(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	clone := f.Clone("cli", 0)

	code, out, errOut := specctl(t, "org", "ls", "--repo", clone)
	if code != exitOK {
		t.Fatalf("ls = %d %s", code, errOut)
	}
	for _, want := range []string{"market", "hono-compute-provider", "relay", "resolved", "unspecced", "at pin"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ls lacks %q:\n%s", want, out)
		}
	}

	code, out, _ = specctl(t, "org", "ls", "--repo", clone, "-o", "json")
	var listed []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &listed) != nil || len(listed) != 3 {
		t.Fatalf("ls json = %d %s", code, out)
	}

	code, out, _ = specctl(t, "org", "status", "--repo", clone)
	if code != exitOK || !strings.Contains(out, "no open-architecture branch") {
		t.Fatalf("status = %d\n%s", code, out)
	}

	code, out, _ = specctl(t, "org", "brief", "--repo", clone)
	if code != exitOK {
		t.Fatalf("brief = %d", code)
	}
	for _, want := range []string{"# Org root socialweb-computer", "open-architecture/<repository>", "specctl org bump", "Push the member first", "| `market` |"} {
		if !strings.Contains(out, want) {
			t.Fatalf("brief lacks %q:\n%s", want, out)
		}
	}

	// Not an org root.
	if code, _, errOut := specctl(t, "org", "ls", "--repo", f.Seed("market")); code != exitError || !strings.Contains(errOut, "not an org root") {
		t.Fatalf("plain repo = %d %s", code, errOut)
	}
}

func TestOrgStatusFailsOnAnUnpublishedPinAndBumpFixesTheOrder(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	clone := f.Clone("unpublished", 0)
	market := filepath.Join(clone, "market")
	if err := os.WriteFile(filepath.Join(market, "NOTES.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.Git(market, "add", "-A")
	f.Git(market, "commit", "-qm", "local market work")

	if code, _, errOut := specctl(t, "org", "bump", "--repo", clone, "market"); code != exitError || !strings.Contains(errOut, "push it first") {
		t.Fatalf("bump before push = %d %s", code, errOut)
	}
	f.Git(market, "push", "-q", "origin", "HEAD:refs/heads/feature")
	code, out, errOut := specctl(t, "org", "bump", "--repo", clone, "--change", "c1", "market")
	if code != exitOK || !strings.Contains(out, "market") {
		t.Fatalf("bump = %d %s %s", code, out, errOut)
	}
	if code, out, _ := specctl(t, "org", "status", "--repo", clone); code != exitOK || strings.Contains(out, "error") {
		t.Fatalf("status after bump = %d\n%s", code, out)
	}

	// Pushing the root before a member is the error status exists to catch.
	f.Git(market, "commit", "-q", "--allow-empty", "-m", "unpushed")
	if code, _, _ := specctl(t, "org", "bump", "--repo", clone, "--allow-unpublished", "market"); code != exitOK {
		t.Fatal("allow-unpublished must record the pin")
	}
	code, out, _ = specctl(t, "org", "status", "--repo", clone)
	if code != exitError || !strings.Contains(out, "dangling pointer") {
		t.Fatalf("status = %d\n%s", code, out)
	}
}

func TestOrgHistoryManifestOutlineAndFetchOnAShallowClone(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	f.Advance("market", map[string]string{"lib/requester/a.ts": "export const a = 1;\n"}, "market: add a")
	clone := f.Clone("deep", 1)

	// A shallow clone has no member branches until they are fetched.
	code, out, _ := specctl(t, "org", "ls", "--repo", clone)
	if code != exitOK || strings.Contains(out, "resolved") {
		t.Fatalf("before fetch = %d\n%s", code, out)
	}
	if code, out, errOut := specctl(t, "org", "fetch", "--repo", clone); code != exitOK || !strings.Contains(out, "market: 2 branch(es)") {
		t.Fatalf("fetch = %d %s %s", code, out, errOut)
	}
	if code, out, _ := specctl(t, "org", "ls", "--repo", clone); code != exitOK || !strings.Contains(out, "resolved") {
		t.Fatalf("after fetch:\n%s", out)
	}

	code, out, errOut := specctl(t, "org", "outline", "--repo", clone, "--member", "market")
	if code != exitOK || !strings.Contains(out, "open-architecture/market@") || !strings.Contains(out, "market  requirements=1") {
		t.Fatalf("member outline = %d\n%s\n%s", code, out, errOut)
	}
	if code, out, _ := specctl(t, "org", "outline", "--repo", clone); code != exitOK || !strings.Contains(out, "member market") || !strings.Contains(out, "(specctl org outline --member market)") {
		t.Fatalf("root outline:\n%s", out)
	}

	// The clone is shallow, so the history is the one commit the clone has.
	if code, out, _ := specctl(t, "org", "history", "--repo", clone); code != exitOK || !strings.Contains(out, "+ market") {
		t.Fatalf("shallow history:\n%s", out)
	}

	// A manifest write needs the local architecture branch.
	branch := "open-architecture/socialweb-computer"
	f.Git(clone, "fetch", "-q", "origin", "refs/heads/"+branch+":refs/heads/"+branch)
	code, out, errOut = specctl(t, "org", "manifest", "--repo", clone, "--write")
	if code != exitOK || !strings.Contains(out, "committed") {
		t.Fatalf("manifest write = %d %s %s", code, out, errOut)
	}
	if code, out, _ := specctl(t, "org", "manifest", "--repo", clone); code != exitOK || !strings.Contains(out, "kind: OrgMembers") || !strings.Contains(out, "state: resolved") {
		t.Fatalf("manifest:\n%s", out)
	}
}

func TestOrgHistoryOnAFullCloneListsMemberCommits(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	f.Advance("market", map[string]string{"lib/requester/a.ts": "export const a = 1;\n"}, "market: add a")
	clone := f.Clone("fullhistory", 0)
	code, out, _ := specctl(t, "org", "history", "--repo", clone, "--member", "market", "-n", "1")
	if code != exitOK || !strings.Contains(out, "bump market: market: add a") || !strings.Contains(out, "1 commit(s)") ||
		!strings.Contains(out, "market: add a") || !strings.Contains(out, "spec @") {
		t.Fatalf("history:\n%s", out)
	}
}

func TestOrgRunDrivesATwoMemberChangeWithAScriptedAgent(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	clone := f.Clone("run", 0)
	dir := t.TempDir()
	scenario := filepath.Join(dir, "scenario.yaml")
	plan := filepath.Join(dir, "plan.yaml")
	must := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(scenario, `contexts: {}
realize:
  market-guest:
    - write: {path: lib/cloud-init/ticket.ts, contents: "export const ticket = true;\n"}
  provider-host:
    - write: {path: lib/compute-provider/ticket.ts, contents: "export const ticket = true;\n"}
`)
	must(plan, `change: ticket
steps:
  - {member: market, context: market-guest}
  - {member: hono-compute-provider, context: provider-host}
`)
	code, out, errOut := specctl(t, "org", "run", "--repo", clone, "--plan", plan, "--agent", "scripted:"+scenario, "--push")
	if code != exitOK || !strings.Contains(out, "root commit") || strings.Contains(out, "not pushed") {
		t.Fatalf("run = %d\n%s\n%s", code, out, errOut)
	}
	log := f.Git(clone, "log", "-1", "--format=%s")
	if !strings.HasPrefix(log, "bump(market, provider)") {
		t.Fatalf("root log = %q", log)
	}
	if code, out, _ := specctl(t, "org", "status", "--repo", clone); code != exitOK {
		t.Fatalf("status after run = %d\n%s", code, out)
	}

	if code, _, _ := specctl(t, "org", "run", "--repo", clone, "--plan", plan); code != exitUsage {
		t.Fatal("run without an agent is a usage error")
	}
}

func TestOrgCloneIsRecursiveAndFetchesMemberBranches(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	target := filepath.Join(t.TempDir(), "root")
	code, out, errOut := specctl(t, "org", "clone", "--depth", "1", f.RemoteURL(f.Spec.Root), target)
	if code != exitOK || !strings.Contains(out, "3 member(s)") || !strings.Contains(out, "resolved") {
		t.Fatalf("clone = %d\n%s\n%s", code, out, errOut)
	}
	if code, _, _ := specctl(t, "org"); code != exitUsage {
		t.Fatal("bare org prints usage")
	}
	if code, _, _ := specctl(t, "org", "nonsense"); code != exitUsage {
		t.Fatal("unknown subcommand")
	}
}

// memberLibrary is a policy library as a policy branch's files: the
// market-mini example, which is a repository's own policies over its own tests.
func memberLibrary(t *testing.T, repository string) map[string][]byte {
	t.Helper()
	dir := filepath.Join("..", "..", "examples", "policies", "market-mini")
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, _ := filepath.Rel(dir, path)
		data, err := os.ReadFile(path)
		if relative == "policies.yaml" {
			data = []byte(strings.Replace(string(data), "repository: market-mini", "repository: "+repository, 1))
		}
		files[filepath.ToSlash(relative)] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestPolicyEvalAtTheOrgRootReadsEveryMemberOnceAndRollsUpTheirOwnPolicies(t *testing.T) {
	orgfixture.FakeCodegraph(t)
	spec := orgfixture.Default()
	spec.Members[0].PolicyFiles = memberLibrary(t, "market")
	f := orgfixture.Build(t, spec)
	orgEnv(t, f)
	clone := f.Clone("policy", 0)

	library := filepath.Join(t.TempDir(), "orgroot-fixture")
	if err := os.CopyFS(library, os.DirFS(filepath.Join("..", "..", "examples", "policies", "orgroot-fixture"))); err != nil {
		t.Fatal(err)
	}
	args := []string{"policy", "eval", "--repo", "socialweb-computer", "--worktree", clone, "--library", library, "--cache-dir", t.TempDir(), "-o", "json"}

	code, out, errOut := specctl(t, args...)
	if code != exitOK {
		t.Fatalf("eval = %d\n%s\n%s", code, out, errOut)
	}
	var report struct {
		Members []struct {
			Name, Ref, Commit string
		}
		MemberPolicies []struct {
			Name, Path, Commit, Pinned, PolicyCommit, Message string
			Totals                                            map[string]int
		}
		Violations []struct{ Constraint string }
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}
	if len(report.Violations) != 0 {
		t.Fatalf("the root's own policy over all three repositories must be clean: %+v", report.Violations)
	}
	pins := map[string]string{}
	for _, member := range report.Members {
		pins[member.Name] = member.Commit
	}
	if len(pins) != 3 || pins["market"] != strings.TrimSpace(f.Git(filepath.Join(clone, "market"), "rev-parse", "HEAD")) {
		t.Fatalf("members = %+v: derived from the gitlinks, pinned by them", report.Members)
	}
	if len(report.MemberPolicies) != 2 {
		t.Fatalf("memberPolicies = %+v", report.MemberPolicies)
	}
	for _, row := range report.MemberPolicies {
		if row.Message != "" || row.Pinned != "" {
			t.Fatalf("row = %+v", row)
		}
	}

	// A member's own policy denies code the root's policy does not look at.
	market := filepath.Join(clone, "market")
	bad := "// a test that dials the guest directly\nexport function dial() { return Deno.connect({ hostname: \"10.0.0.2\", port: 22 }); }\n"
	if err := os.MkdirAll(filepath.Join(market, "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(market, "test", "dial_test.ts"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	f.Git(market, "add", "-A")
	f.Git(market, "commit", "-qm", "market: a test that dials the guest")
	code, out, errOut = specctl(t, append(args, "--strict")...)
	if code != exitError {
		t.Fatalf("a member's own deny must fail --strict at the root: %d\n%s\n%s", code, out, errOut)
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	denied := ""
	for _, row := range report.MemberPolicies {
		if row.Name == "market" && row.Totals["deny"] > 0 {
			denied = row.Pinned
		}
	}
	if denied == "" {
		t.Fatalf("market's row must report its deny and that the checkout is off the pin: %+v", report.MemberPolicies)
	}
	code, text, _ := specctl(t, "policy", "eval", "--repo", "socialweb-computer", "--worktree", clone, "--library", library, "--cache-dir", t.TempDir())
	if code != exitOK || !strings.Contains(text, "member policy: market") || !strings.Contains(text, "off the pin") {
		t.Fatalf("text report:\n%s", text)
	}
}

func TestTheRootsCodeGraphHoldsTheRootsOwnFilesOnly(t *testing.T) {
	orgfixture.FakeCodegraph(t)
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	clone := f.Clone("graph", 0)
	// The fake indexer walks the whole work tree, as the real one does. The
	// root's graph must still hold the root's files and nothing of a member's.
	code, out, errOut := specctl(t, "policy", "effects", "--worktree", clone, "--repo", "socialweb-computer", "-o", "json")
	_ = errOut
	if code != exitOK {
		t.Fatalf("effects = %d\n%s", code, errOut)
	}
	for _, member := range []string{"market/", "hono-compute-provider/", "relay/"} {
		if strings.Contains(out, "\""+member) || strings.Contains(out, " "+member+"lib") {
			t.Fatalf("the root's effects name a member's file (%s):\n%s", member, out)
		}
	}
}

func greenfieldOrg(t *testing.T, hostInitiator string) (*orgfixture.Fixture, string) {
	t.Helper()
	s := orgfixture.Default()
	s.Members[0].Interactions = []spec.Interaction{{
		ID: "i.report", Peer: "host", Initiator: spec.InitiatorSelf, Channel: "relay",
		Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust,
	}}
	s.Members[1].Interactions = []spec.Interaction{{
		ID: "i.provider-guest", Peer: "guest", Initiator: hostInitiator, Channel: "relay",
		Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust,
	}}
	f := orgfixture.Build(t, s)
	orgEnv(t, f)
	return f, f.Clone("greenfield", 0)
}

func TestSpecsOnlyAtTheOrgRootChecksTheMembersDeclaredInteractions(t *testing.T) {
	library := filepath.Join(t.TempDir(), "orgroot-greenfield")
	if err := os.CopyFS(library, os.DirFS(filepath.Join("..", "..", "examples", "policies", "orgroot-greenfield"))); err != nil {
		t.Fatal(err)
	}
	eval := func(clone string) (int, string) {
		code, out, errOut := specctl(t, "policy", "eval", "--repo", "socialweb-computer", "--path", clone,
			"--specs-only", "--library", library, "--strict")
		if code != exitOK && code != exitError {
			t.Fatalf("eval = %d\n%s\n%s", code, out, errOut)
		}
		return code, out
	}

	// The provider's spec declares that the host initiates toward the guest;
	// the guest is declared in the market's architecture, in another repository.
	_, clone := greenfieldOrg(t, spec.InitiatorSelf)
	code, out := eval(clone)
	if code != exitError || !strings.Contains(out, "rfp-host-reach-in") || !strings.Contains(out, "spec gate: denied") {
		t.Fatalf("the declared reach-in across repositories must be denied: %d\n%s", code, out)
	}

	// The fix is declared in the provider's spec: the guest initiates.
	_, clone = greenfieldOrg(t, spec.InitiatorPeer)
	if code, out := eval(clone); code != exitOK || !strings.Contains(out, "spec gate: allowed") {
		t.Fatalf("the corrected spec must pass: %d\n%s", code, out)
	}
}

func TestOrgHistoryJSONUsesStableKeys(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	orgEnv(t, f)
	f.Advance("market", map[string]string{"lib/requester/a.ts": "export const a = 1;\n"}, "market: add a")
	clone := f.Clone("json", 0)
	code, out, _ := specctl(t, "org", "history", "--repo", clone, "--member", "market", "-n", "1", "-o", "json")
	var entries []struct {
		Commit  string `json:"commit"`
		Subject string `json:"subject"`
		Moves   []struct {
			Member string `json:"member"`
			Path   string `json:"path"`
			From   string `json:"from"`
			To     string `json:"to"`
			Total  int    `json:"total"`
			Arch   string `json:"arch"`
		} `json:"moves"`
	}
	if code != exitOK || json.Unmarshal([]byte(out), &entries) != nil || len(entries) != 1 {
		t.Fatalf("history json = %d\n%s", code, out)
	}
	move := entries[0].Moves[0]
	if entries[0].Commit == "" || move.Member != "market" || move.Path != "market" || move.From == "" || move.To == "" || move.Total != 1 || move.Arch == "" {
		t.Fatalf("entry = %+v\n%s", entries[0], out)
	}
}
