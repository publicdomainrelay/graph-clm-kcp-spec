package org

import (
	"reflect"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const gitmodulesText = `[submodule "hono-compute-provider"]
	path = hono-compute-provider
	url = https://github.com/publicdomainrelay/compute-provider-digitalocean.git
# a comment
[submodule ".reference/compute-contract-reference-implementation-poc"]
	path = .reference/compute-contract-reference-implementation-poc
	url = https://github.com/publicdomainrelay/compute-contract-reference-implementation-poc
[submodule "atproto-market"]
	path = atproto-market
	url = git@github.com:publicdomainrelay/atproto-market.git
	branch = main
`

func TestParseGitmodulesSortsByPathAndReadsBranch(t *testing.T) {
	got, err := ParseGitmodules(gitmodulesText)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, s := range got {
		paths = append(paths, s.Path)
	}
	want := []string{".reference/compute-contract-reference-implementation-poc", "atproto-market", "hono-compute-provider"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if got[1].Branch != "main" || got[1].Name != "atproto-market" {
		t.Fatalf("atproto-market = %+v", got[1])
	}
}

func TestParseGitmodulesRejectsAnEntryWithoutPath(t *testing.T) {
	if _, err := ParseGitmodules("[submodule \"x\"]\n\turl = u\n"); err == nil {
		t.Fatal("want an error for a submodule without a path")
	}
	if _, err := ParseGitmodules("[submodule \"x\"]\n\tpath\n"); err == nil {
		t.Fatal("want an error for a line that is not key = value")
	}
}

func TestRepositoryCandidatesPreferTheCheckoutDirectory(t *testing.T) {
	subs, _ := ParseGitmodules(gitmodulesText)
	var provider Submodule
	for _, s := range subs {
		if s.Path == "hono-compute-provider" {
			provider = s
		}
	}
	want := []string{"hono-compute-provider", "compute-provider-digitalocean"}
	if got := provider.RepositoryCandidates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
}

func TestURLBase(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/o/r.git": "r",
		"https://github.com/o/r/":    "r",
		"git@github.com:o/r.git":     "r",
		"../sibling":                 "sibling",
		"file:///tmp/remotes/m.git":  "m",
		"":                           "",
	} {
		if got := URLBase(url); got != want {
			t.Errorf("URLBase(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestSelectArchPicksTheNewestEntryThatDescribesThePin(t *testing.T) {
	ancestors := map[string]string{"c1": "c3", "c2": "c3"} // c1 and c2 are ancestors of c3
	isAncestor := func(a, b string) bool { return ancestors[a] == b }
	history := []ArchEntry{{Commit: "a3", IndexedCommit: "c4"}, {Commit: "a2", IndexedCommit: "c2"}, {Commit: "a1", IndexedCommit: "c1"}}

	got, ok := SelectArch(history, "c3", isAncestor)
	if !ok || got.Commit != "a2" {
		t.Fatalf("got %+v %v, want a2: c4 is newer than the pin", got, ok)
	}
	got, ok = SelectArch(history, "c4", isAncestor)
	if !ok || got.Commit != "a3" {
		t.Fatalf("got %+v %v, want the exact match a3", got, ok)
	}
	if _, ok := SelectArch(history, "zzz", isAncestor); ok {
		t.Fatal("no entry describes zzz")
	}
	if _, ok := SelectArch([]ArchEntry{{Commit: "a", IndexedCommit: ""}}, "c3", isAncestor); ok {
		t.Fatal("an entry that indexed nothing never matches")
	}
}

func TestStateOf(t *testing.T) {
	cases := []struct {
		initialized, hasArch, selected bool
		want                           State
	}{
		{false, true, true, StateUninitialized},
		{true, false, false, StateUnspecced},
		{true, true, true, StateResolved},
		{true, true, false, StateStale},
	}
	for _, c := range cases {
		if got := StateOf(c.initialized, c.hasArch, c.selected); got != c.want {
			t.Errorf("StateOf(%v,%v,%v) = %s, want %s", c.initialized, c.hasArch, c.selected, got, c.want)
		}
	}
}

func TestManifestRoundTripsAndSortsByPath(t *testing.T) {
	manifest := NewManifest("root", "open-architecture/root", []Member{
		{Name: "b", Path: "b", CodeCommit: "2", State: StateUnspecced},
		{Name: "a", Path: "a", URL: "u", CodeCommit: "1", State: StateResolved,
			Arch: &BranchRef{Branch: "open-architecture/a", Commit: "x", IndexedCommit: "1"}},
	})
	data, err := manifest.Render()
	if err != nil {
		t.Fatal(err)
	}
	again, err := manifest.Render()
	if err != nil || string(again) != string(data) {
		t.Fatal("render is not deterministic")
	}
	parsed, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, manifest) {
		t.Fatalf("round trip differs:\n%+v\n%+v", parsed, manifest)
	}
	if parsed.Members[0].Name != "a" {
		t.Fatalf("members not sorted by path: %+v", parsed.Members)
	}
	if _, err := ParseManifest([]byte("kind: Other\n")); err == nil {
		t.Fatal("want an error for another kind")
	}
	if strings.Contains(string(data), "spec:") {
		t.Fatal("the manifest must hold references, not specs")
	}
}

func TestBumpMessageRoundTrips(t *testing.T) {
	bumps := []Bump{
		{Member: "atproto-market", Path: "atproto-market", From: "05fe29612a62", To: "91ab3c2d4e5f", Spec: "7777777"},
		{Member: "compute-provider", Path: "hono-compute-provider", From: "d722abc", To: "e833bcd"},
	}
	message := Message(bumps, "change-1", "two repos, one feature")
	if !strings.HasPrefix(message, "bump(atproto-market, compute-provider): 2 members\n") {
		t.Fatalf("subject: %q", message)
	}
	if got := ParseBumps(message); !reflect.DeepEqual(got, bumps) {
		t.Fatalf("parsed %+v, want %+v", got, bumps)
	}
	if !strings.Contains(message, "Spec-Change: change-1") {
		t.Fatalf("no change trailer: %s", message)
	}
	single := Subject(bumps[:1])
	if single != "bump(atproto-market): 05fe296 -> 91ab3c2" {
		t.Fatalf("single subject: %q", single)
	}
}

func TestParseHistoryKeepsOnlyCommitsThatMovedAPointer(t *testing.T) {
	out := strings.Join([]string{
		"commit aaaa\t2026-10-05T10:00:00Z\tAda\tbump the market",
		"",
		":160000 160000 1111 2222 M\tatproto-market",
		":100644 100644 3333 4444 M\tREADME.md",
		"commit bbbb\t2026-10-04T10:00:00Z\tAda\tdocs only",
		"",
		":100644 100644 3333 4444 M\tREADME.md",
		"commit cccc\t2026-10-03T10:00:00Z\tBo\tadd the relay",
		"",
		":000000 160000 0000 5555 A\tdid-key-ingress-proxy",
		":160000 000000 6666 0000 D\told-thing",
		"",
	}, "\n")
	got := ParseHistory(out)
	if len(got) != 2 {
		t.Fatalf("commits = %+v", got)
	}
	if got[0].Commit != "aaaa" || len(got[0].Moves) != 1 || got[0].Moves[0] != (Move{Path: "atproto-market", From: "1111", To: "2222"}) {
		t.Fatalf("first = %+v", got[0])
	}
	added, removed := got[1].Moves[0], got[1].Moves[1]
	if !added.Added() || added.To != "5555" || !removed.Removed() || removed.From != "6666" {
		t.Fatalf("second = %+v", got[1])
	}
	if got[1].Subject != "add the relay" || got[1].Author != "Bo" {
		t.Fatalf("meta = %+v", got[1])
	}
}

func TestPolicyMembersAreDerivedAndPinnedByTheGitlink(t *testing.T) {
	members := []Member{
		{Name: "atproto-market", Path: "atproto-market", URL: "u1", CodeCommit: "c1"},
		{Name: "provider", Path: "hono-compute-provider", URL: "u2", CodeCommit: "c2"},
		{Name: "typescript-helpers", Path: "typescript-helpers", URL: "u3", CodeCommit: "c3"},
	}
	explicit := []policy.Member{
		{Name: "provider", Roles: map[string]policy.RoleBinding{"host": {}}, Ref: "ignored-ref-wins-if-set"},
		{Name: "elsewhere", URL: "u9"},
	}
	got := PolicyMembers(members, policy.Submodules{Members: true, Exclude: []string{"typescript-*"}}, explicit)
	names := []string{}
	for _, m := range got {
		names = append(names, m.Name)
	}
	if want := []string{"atproto-market", "provider", "elsewhere"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if got[0].Ref != "c1" || got[0].URL != "u1" {
		t.Fatalf("derived member = %+v", got[0])
	}
	if got[1].Ref != "ignored-ref-wins-if-set" || got[1].URL != "u2" || len(got[1].Roles) != 1 {
		t.Fatalf("explicit override = %+v", got[1])
	}
	only := PolicyMembers(members, policy.Submodules{Members: true, Include: []string{"atproto-*"}}, nil)
	if len(only) != 1 || only[0].Name != "atproto-market" {
		t.Fatalf("include = %+v", only)
	}
}

func TestMembersConnectTheGraphWithoutCopyingIt(t *testing.T) {
	members := []Member{{Name: "atproto-market", Path: "atproto-market", CodeCommit: "c1", State: StateResolved,
		Arch: &BranchRef{Branch: "open-architecture/atproto-market", Commit: "a1"}}}
	vertices, edges := graph.Build(graph.Snapshot{Namespace: "ns", Repository: rootRepo("root"), Members: GraphRefs(members)})
	var memberRows, repoRows int
	for _, set := range vertices {
		switch set.Label {
		case graph.LabelMember:
			memberRows = len(set.Rows)
			if set.Rows[0].Props["archCommit"] != "a1" || set.Rows[0].Props["state"] != "resolved" {
				t.Fatalf("member props = %v", set.Rows[0].Props)
			}
		case graph.LabelRepo:
			repoRows = len(set.Rows)
		}
	}
	if memberRows != 1 || repoRows != 1 {
		t.Fatalf("member vertices %d, repo vertices %d: the root must write its own repo and the member reference only", memberRows, repoRows)
	}
	for _, set := range edges {
		if set.Type == graph.EdgeResolvesTo {
			if len(set.Rows) != 1 || set.Rows[0].To != graph.RepoIDIn("ns", "atproto-market") ||
				set.Rows[0].From != graph.MemberIDIn("ns", "root", "atproto-market") {
				t.Fatalf("RESOLVES_TO = %+v", set.Rows)
			}
			return
		}
	}
	t.Fatal("no RESOLVES_TO edge set")
}

func rootRepo(name string) spec.Repository {
	repository := spec.Repository{}
	repository.Name = name
	return repository
}

func TestAnUninitializedMemberTakesTheNameTheLibraryGivesIt(t *testing.T) {
	// No checkout means no branches to find the repository name in: the library
	// names the member "provider" and the submodule's url is that repository.
	members := []Member{{Name: "hono-compute-provider", Path: "hono-compute-provider", URL: "u", CodeCommit: "c",
		Candidates: []string{"hono-compute-provider", "provider"}}}
	got := PolicyMembers(members, policy.Submodules{Members: true}, []policy.Member{{Name: "provider", TestGlobs: []string{"t/**"}}})
	if len(got) != 1 || got[0].Name != "provider" || got[0].Ref != "c" || len(got[0].TestGlobs) != 1 {
		t.Fatalf("members = %+v", got)
	}
	// An exact name still wins over a candidate.
	exact := PolicyMembers(members, policy.Submodules{Members: true}, []policy.Member{{Name: "hono-compute-provider"}, {Name: "provider"}})
	if len(exact) != 2 || exact[0].Name != "hono-compute-provider" || exact[1].Name != "provider" {
		t.Fatalf("exact = %+v", exact)
	}
}

func TestProblemsTreatAShallowCheckoutsPinAsUnknownNotUnpublished(t *testing.T) {
	state := MemberState{
		Member:      Member{Name: "m", Path: "m", CodeCommit: "abcdef0123", State: StateResolved},
		Initialized: true, PinPresent: true, Published: false, Shallow: true,
	}
	problems := Problems(state)
	if len(problems) != 1 || problems[0].Code != CodeUnverified || problems[0].Severity != SeverityInfo {
		t.Fatalf("shallow: %+v", problems)
	}
	state.Shallow = false
	problems = Problems(state)
	if len(problems) != 1 || problems[0].Code != CodeUnpublished || problems[0].Severity != SeverityError {
		t.Fatalf("full: %+v", problems)
	}
	state.Published = true
	if got := Problems(state); len(got) != 0 {
		t.Fatalf("published: %+v", got)
	}
}
