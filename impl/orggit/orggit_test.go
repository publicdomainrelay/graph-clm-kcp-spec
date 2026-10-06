package orggit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/orgfixture"
)

func open(t *testing.T, f *orgfixture.Fixture, dir string) *Root {
	t.Helper()
	root, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	root.Env = f.Env()
	return root
}

func byName(states []org.MemberState) map[string]org.MemberState {
	out := map[string]org.MemberState{}
	for _, state := range states {
		out[state.Name] = state
	}
	return out
}

func TestMembersOfAFullRecursiveClone(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("full", 0)
	root := open(t, f, clone)
	states, err := root.Members(context.Background(), MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Fatalf("members = %d: %+v", len(states), states)
	}
	m := byName(states)

	market := m["market"]
	if market.State != org.StateResolved || market.Arch == nil || market.Policy == nil {
		t.Fatalf("market = %+v", market)
	}
	if market.Arch.IndexedCommit != market.CodeCommit {
		t.Fatalf("the architecture indexed %s, the pin is %s", market.Arch.IndexedCommit, market.CodeCommit)
	}
	if !market.Initialized || !market.PinPresent || !market.Published || market.Dirty {
		t.Fatalf("market live state = %+v", market)
	}

	// The checkout directory is hono-compute-provider, the repository is
	// provider: the url's name finds its branches.
	provider := m["provider"]
	if provider.State != org.StateResolved || provider.Path != "hono-compute-provider" {
		t.Fatalf("provider = %+v", provider)
	}

	relay := m["relay"]
	if relay.State != org.StateUnspecced || relay.Arch != nil || relay.Policy != nil {
		t.Fatalf("relay = %+v", relay)
	}
}

func TestAMemberWhoseDirectoryIsNotItsRepositoryNameStillResolves(t *testing.T) {
	spec := orgfixture.Default()
	spec.Members[1].Name = "hono-compute-provider"
	spec.Members[1].Path = ""
	f := orgfixture.Build(t, spec)
	root := open(t, f, f.Clone("name", 0))
	states, err := root.Members(context.Background(), MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := byName(states)["hono-compute-provider"]; got.State != org.StateResolved {
		t.Fatalf("provider = %+v", got)
	}
}

func TestRepositoryNameFallsBackToTheURLName(t *testing.T) {
	// The checkout directory is "hono-compute-provider" but the member's own
	// repository is called "provider": the url's name is the second candidate.
	spec := orgfixture.Default()
	spec.Members[1].Path = "hono-compute-provider"
	f := orgfixture.Build(t, spec)
	root := open(t, f, f.Clone("url", 0))
	states, err := root.Members(context.Background(), MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := byName(states)["provider"]
	if got.State != org.StateResolved || got.Path != "hono-compute-provider" || got.Arch.Branch != "open-architecture/provider" {
		t.Fatalf("provider = %+v", got)
	}
}

func TestShallowCloneFetchesOnlyTheBranchesAMemberNeeds(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	f.Advance("market", map[string]string{"lib/requester/extra.ts": "export const x = 1;\n"}, "second market commit")
	clone := f.Clone("shallow", 1)
	root := open(t, f, clone)
	ctx := context.Background()

	before, err := root.Members(ctx, MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := byName(before)["market"]; got.State != org.StateUnspecced {
		t.Fatalf("a shallow clone has no orphan branch yet: %+v", got)
	}

	results, err := root.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fetched := map[string][]string{}
	for _, result := range results {
		fetched[result.Member] = result.Branches
	}
	if len(fetched["market"]) != 2 {
		t.Fatalf("market fetched %v, want its arch and policy branch", fetched["market"])
	}
	if len(fetched["relay"]) != 0 {
		t.Fatalf("relay has no orphan branches, fetched %v", fetched["relay"])
	}

	after, err := root.Members(ctx, MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	market := byName(after)["market"]
	if market.State != org.StateResolved || market.Policy == nil {
		t.Fatalf("after fetch: %+v", market)
	}
}

func TestUninitializedSubmoduleIsReportedNotRead(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	dir := filepath.Join(f.Dir, "clones", "bare")
	f.Git(f.Dir, "clone", "-q", f.RemoteURL(f.Spec.Root), dir)
	root := open(t, f, dir)
	states, err := root.Members(context.Background(), MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Fatalf("members = %d", len(states))
	}
	for _, state := range states {
		if state.Initialized || state.State != org.StateUninitialized || state.CodeCommit == "" {
			t.Fatalf("%s = %+v: the pin is known, the checkout is not", state.Name, state)
		}
		problems := org.Problems(state)
		if len(problems) != 1 || problems[0].Code != org.CodeUninitialized {
			t.Fatalf("%s problems = %+v", state.Name, problems)
		}
	}
}

func TestStatusProblemsForDirtyAheadAndUnpublishedMembers(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("work", 0)
	root := open(t, f, clone)
	ctx := context.Background()
	market := filepath.Join(clone, "market")

	if err := os.WriteFile(filepath.Join(market, "lib", "requester", "mod.ts"), []byte("// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.Git(market, "commit", "-qam", "local work, not pushed")

	states, err := root.Members(ctx, MembersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := byName(states)["market"]
	if got.Ahead != 1 || got.Behind != 0 || got.Head == got.CodeCommit {
		t.Fatalf("market = %+v", got)
	}
	if !got.Published {
		t.Fatal("the pin itself is on origin, only HEAD is local")
	}
	codes := []string{}
	for _, p := range org.Problems(got) {
		codes = append(codes, p.Code)
	}
	if strings.Join(codes, ",") != org.CodeAhead {
		t.Fatalf("problems = %v", codes)
	}

	// A bump to an unpublished commit is refused.
	if _, _, err := root.Bump(ctx, []string{"market"}, BumpOptions{}); err == nil || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("bump of an unpublished commit: %v", err)
	}
	f.Git(market, "push", "-q", "origin", "HEAD:main")
	commit, bumps, err := root.Bump(ctx, []string{"market"}, BumpOptions{Change: "change-7", Body: "market edit"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bumps) != 1 || bumps[0].To != got.Head || bumps[0].From != got.CodeCommit {
		t.Fatalf("bumps = %+v", bumps)
	}
	message := f.Git(clone, "log", "-1", "--format=%B", commit)
	if !strings.HasPrefix(message, "bump(market): ") || !strings.Contains(message, "Spec-Change: change-7") {
		t.Fatalf("message = %q", message)
	}
	parsed := org.ParseBumps(message)
	if len(parsed) != 1 || parsed[0].Path != "market" {
		t.Fatalf("trailers = %+v", parsed)
	}

	after, _ := root.Members(ctx, MembersOptions{})
	if m := byName(after)["market"]; m.CodeCommit != got.Head || m.Ahead != 0 {
		t.Fatalf("after bump: %+v", m)
	}
	if again, _, err := root.Bump(ctx, nil, BumpOptions{}); err != nil || again != "" {
		t.Fatalf("a second bump has nothing to do: %q %v", again, err)
	}
}

func TestHistoryShowsHowPointersMovedAndWhichSpecEachResolvesTo(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	m1, r1 := f.Advance("market", map[string]string{"lib/requester/a.ts": "export const a = 1;\n"}, "market: add a")
	m2, r2 := f.Advance("market", map[string]string{"lib/requester/b.ts": "export const b = 1;\n"}, "market: add b")
	_, r3 := f.Advance("relay", map[string]string{"lib/relay-server/c.ts": "export const c = 1;\n"}, "relay: add c")
	clone := f.Clone("history", 0)
	root := open(t, f, clone)
	ctx := context.Background()

	entries, err := root.History(ctx, HistoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// 3 advances plus the 3 submodule additions.
	if len(entries) != 6 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].Commit != r3 || entries[1].Commit != r2 || entries[2].Commit != r1 {
		t.Fatalf("order: %s %s %s, want %s %s %s", entries[0].Commit, entries[1].Commit, entries[2].Commit, r3, r2, r1)
	}
	move := entries[1].Details[0]
	if move.Member != "market" || move.To != m2 || move.From != m1 || move.Total != 1 || len(move.Commits) != 1 {
		t.Fatalf("second market move = %+v", move)
	}
	if move.Commits[0].Subject != "market: add b" {
		t.Fatalf("member commit = %+v", move.Commits[0])
	}
	if move.Arch == "" {
		t.Fatal("the pin resolves to an architecture commit of the member")
	}
	relayMove := entries[0].Details[0]
	if relayMove.Member != "relay" || relayMove.Arch != "" {
		t.Fatalf("relay has no spec: %+v", relayMove)
	}
	last := entries[len(entries)-1]
	if !last.Details[0].Added() || last.Details[0].Message != "added" {
		t.Fatalf("the oldest entries add submodules: %+v", last.Details[0])
	}

	only, err := root.History(ctx, HistoryOptions{Member: "market", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 2 || only[0].Commit != r2 || only[1].Commit != r1 {
		t.Fatalf("market only: %+v", only)
	}
}

func TestManifestIsWrittenToTheRootsArchitectureBranchWithoutTouchingTheTree(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("manifest", 0)
	root := open(t, f, clone)
	ctx := context.Background()
	headBefore := f.Git(clone, "rev-parse", "HEAD")
	statusBefore := f.Git(clone, "status", "--porcelain")

	// A clone has the root's architecture only as a remote-tracking ref; the
	// manifest write needs the local branch the persist keeps, so create it.
	branch := root.ArchBranch(ctx)
	f.Git(clone, "branch", branch, "origin/"+branch)
	archBefore := f.Git(clone, "rev-parse", branch)

	commit, changed, err := root.WriteManifest(ctx)
	if err != nil || !changed || commit == archBefore {
		t.Fatalf("write = %q %v %v", commit, changed, err)
	}
	if again, changed, err := root.WriteManifest(ctx); err != nil || changed || again != commit {
		t.Fatalf("a second write must be a no-op: %q %v %v", again, changed, err)
	}
	if f.Git(clone, "rev-parse", "HEAD") != headBefore || f.Git(clone, "status", "--porcelain") != statusBefore {
		t.Fatal("the work tree, index or HEAD moved")
	}
	// The branch kept its own files.
	tree := f.Git(clone, "ls-tree", "-r", "--name-only", branch)
	if !strings.Contains(tree, "arch.yaml") || !strings.Contains(tree, "members.yaml") {
		t.Fatalf("tree = %s", tree)
	}
	manifest, ok, err := root.ReadManifest(ctx)
	if err != nil || !ok {
		t.Fatalf("read = %v %v", ok, err)
	}
	if len(manifest.Members) != 3 {
		t.Fatalf("members = %+v", manifest.Members)
	}
	market, _ := manifest.Member("market")
	if market.Arch == nil || market.Arch.Commit == "" || market.State != org.StateResolved {
		t.Fatalf("market = %+v", market)
	}
	data, _ := os.ReadFile(filepath.Join(clone, "market", "lib", "requester", "mod.ts"))
	if strings.Contains(f.Git(clone, "show", branch+":members.yaml"), string(data)) {
		t.Fatal("members.yaml must not carry code")
	}
}

func TestReadArchReadsTheMembersBranchInPlace(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	root := open(t, f, f.Clone("read", 0))
	ctx := context.Background()
	states, _ := root.Members(ctx, MembersOptions{})
	market := byName(states)["market"]
	files, err := root.ReadArch(ctx, market.Member, []string{"specs/market.yaml", "repository.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["specs/market.yaml"]), "reports its own network information") {
		t.Fatalf("spec = %s", files["specs/market.yaml"])
	}
	if _, err := root.ReadArch(ctx, byName(states)["relay"].Member, nil); err == nil {
		t.Fatal("relay has no architecture")
	}
	policies, err := root.ReadPolicy(ctx, market.Member, []string{"policies.yaml"})
	if err != nil || !strings.Contains(string(policies["policies.yaml"]), "repository: market") {
		t.Fatalf("policy = %v %v", policies, err)
	}
}

func TestOpenInsideAMemberClimbsToTheRoot(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("climb", 0)
	root, err := Open(context.Background(), filepath.Join(clone, "market", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(clone)
	got, _ := filepath.EvalSymlinks(root.Dir)
	if got != resolved {
		t.Fatalf("root = %s, want %s", got, resolved)
	}
}

func TestCloneHelperFetchesMemberBranches(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	dir := filepath.Join(f.Dir, "clones", "helper")
	root, err := Clone(context.Background(), f.RemoteURL(f.Spec.Root), dir, CloneOptions{Depth: 1, Env: f.Env()})
	if err != nil {
		t.Fatal(err)
	}
	states, _ := root.Members(context.Background(), MembersOptions{})
	if got := byName(states)["market"]; got.State != org.StateResolved {
		t.Fatalf("market = %+v", got)
	}
}

func TestAnnotateAddsMembersToAPersistSnapshot(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("annotate", 0)
	ctx := context.Background()

	// persist builds its Root from the repository's path, so a root that is
	// itself a submodule of something larger does not describe its parent.
	root := &Root{Dir: clone, Name: f.Spec.Root, Env: f.Env()}
	snapshot := oabranch.Snapshot{}
	if err := root.Annotate(ctx, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Members) != 3 || snapshot.Extra[org.MembersPath] == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	manifest, err := org.ParseManifest(snapshot.Extra[org.MembersPath])
	if err != nil || manifest.Metadata.Name != f.Spec.Root {
		t.Fatalf("manifest = %+v %v", manifest, err)
	}

	plain := &Root{Dir: f.Seed("market"), Name: "market", Env: f.Env()}
	empty := oabranch.Snapshot{}
	if err := plain.Annotate(ctx, &empty); err != nil || empty.Extra != nil || empty.Members != nil {
		t.Fatalf("a repository without submodules is left alone: %+v %v", empty, err)
	}
}

func TestAPinDescribedOnlyByACodeBranchsArchitectureResolvesWhenItIndexedThePin(t *testing.T) {
	spec := orgfixture.Default()
	spec.Members[2].FeatureArch = "spec/iroh"
	f := orgfixture.Build(t, spec)
	ctx := context.Background()

	root := open(t, f, f.Clone("feature", 0))
	states, _ := root.Members(ctx, MembersOptions{})
	relay := byName(states)["relay"]
	if relay.State != org.StateResolved || relay.Arch == nil || relay.Arch.Branch != "open-architecture/relay--spec-iroh" ||
		relay.Arch.IndexedCommit != relay.CodeCommit {
		t.Fatalf("relay = %+v %+v", relay, relay.Arch)
	}
	if files, err := root.ReadArch(ctx, relay.Member, []string{"repository.yaml"}); err != nil || len(files["repository.yaml"]) == 0 {
		t.Fatalf("read the feature architecture: %v", err)
	}

	// The pin moves; the code branch's architecture was not re-indexed, and a
	// feature branch only ever matches the exact commit it indexed.
	f.Advance("relay", map[string]string{"lib/relay-server/x.ts": "export const x = 1;\n"}, "relay: x")
	moved := open(t, f, f.Clone("feature-moved", 0))
	states, _ = moved.Members(ctx, MembersOptions{})
	if got := byName(states)["relay"]; got.State != org.StateStale {
		t.Fatalf("after the pin moved: %+v", got)
	}
}

func TestFetchDepthDeepensAShallowCloneSoHistoryListsMemberCommits(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	for _, message := range []string{"market: one", "market: two", "market: three"} {
		f.Advance("market", map[string]string{"lib/requester/" + strings.ReplaceAll(message, " ", "-") + ".ts": "export const x = 1;\n"}, message)
	}
	ctx := context.Background()
	root := open(t, f, f.Clone("deepen", 1))

	before, err := root.History(ctx, HistoryOptions{Member: "market"})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].Details[0].Total != 0 || before[0].Details[0].Message == "" {
		t.Fatalf("a depth 1 clone has one root commit and cannot list member commits: %+v", before)
	}

	results, err := root.FetchWith(ctx, FetchOptions{Depth: 50})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Member != "(root)" || !strings.Contains(results[0].Message, "deepened") {
		t.Fatalf("results = %+v", results)
	}
	after, err := root.History(ctx, HistoryOptions{Member: "market"})
	if err != nil {
		t.Fatal(err)
	}
	// 3 advances and the addition.
	if len(after) != 4 {
		t.Fatalf("entries = %d", len(after))
	}
	move := after[0].Details[0]
	if move.Total != 1 || len(move.Commits) != 1 || move.Commits[0].Subject != "market: three" {
		t.Fatalf("newest move = %+v", move)
	}
}

func TestASingleBranchCloneCannotTellWhetherAPinIsPublished(t *testing.T) {
	f := orgfixture.Build(t, orgfixture.Default())
	clone := f.Clone("single", 0)
	root := open(t, f, clone)
	ctx := context.Background()
	states, _ := root.Members(ctx, MembersOptions{})
	if byName(states)["market"].Shallow {
		t.Fatal("a full clone tracks every branch")
	}
	f.Git(filepath.Join(clone, "market"), "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	states, _ = root.Members(ctx, MembersOptions{})
	if got := byName(states)["market"]; !got.Shallow {
		t.Fatalf("a single branch clone sees a few of the remote's branches: %+v", got)
	}
}
