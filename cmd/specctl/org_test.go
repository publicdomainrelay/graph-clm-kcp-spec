package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
