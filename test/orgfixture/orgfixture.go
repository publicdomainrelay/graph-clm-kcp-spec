// Package orgfixture builds a polyrepo in a temp directory: one bare remote
// per repository, orphan architecture and policy branches on the members, and
// a superproject that pins the members as submodules. It is the offline stand-in
// for an org root such as socialweb-computer.
package orgfixture

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
)

// Member describes one submodule.
type Member struct {
	// Name is the repository name; its branches are open-architecture/<Name>.
	Name string

	// Fixture is the directory under fixtures/orgroot that holds its code.
	Fixture string

	// Path is where the root mounts it; empty means Name.
	Path string

	// Arch and Policy give the member orphan branches.
	Arch, Policy bool

	// Requirement is the text of the one requirement its architecture holds.
	Requirement string

	// Interactions are declared on the member's one context, so a spec-only
	// org can be checked before any code exists.
	Interactions []spec.Interaction

	// FeatureArch names a code branch whose own architecture branch,
	// open-architecture/<Name>--<FeatureArch>, indexes the initial commit. A
	// member can have it with or without the default branch's.
	FeatureArch string

	// PolicyFiles replaces the member's policy branch with these files, path to
	// contents. Empty means a policies.yaml that names the repository only.
	PolicyFiles map[string][]byte
}

// Spec describes the polyrepo.
type Spec struct {
	// Root is the superproject's repository name.
	Root string

	Members []Member

	// RootArch and RootPolicy give the superproject orphan branches.
	RootArch, RootPolicy bool

	// Library overrides the policies.yaml of the root's policy branch.
	RootPolicies string
}

// Fixture is the built polyrepo.
type Fixture struct {
	Dir string

	// Remotes holds the bare repositories, <name>.git.
	Remotes string

	// Seeds hold the work repositories used to make history.
	Seeds string

	Spec Spec

	tick int
	t    testing.TB
}

// Default is the three member polyrepo: market (guest side), provider (host
// side) and relay. market and provider have architecture and policy branches,
// relay has none, so every state of a member is present.
func Default() Spec {
	return Spec{
		Root: "socialweb-computer",
		Members: []Member{
			{Name: "market", Fixture: "market", Arch: true, Policy: true,
				Requirement: "The guest reports its own network information to the host over the relay."},
			{Name: "provider", Fixture: "provider", Path: "hono-compute-provider", Arch: true, Policy: true,
				Requirement: "The host provisions a guest from user_data and never reaches into it."},
			{Name: "relay", Fixture: "relay"},
		},
		RootArch:   true,
		RootPolicy: true,
	}
}

// Build makes the polyrepo. Every git call uses a private identity and home, so
// the machine's configuration never leaks in.
func Build(t testing.TB, spec Spec) *Fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	f := &Fixture{Dir: dir, Remotes: filepath.Join(dir, "remotes"), Seeds: filepath.Join(dir, "seeds"), Spec: spec, t: t}
	for _, d := range []string{f.Remotes, f.Seeds} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, member := range spec.Members {
		f.buildMember(member)
	}
	f.buildRoot()
	return f
}

// Env is the environment every git command of the fixture, and of a test that
// clones it, should use.
func (f *Fixture) Env() []string {
	return []string{
		"HOME=" + f.Dir,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=protocol.file.allow", "GIT_CONFIG_VALUE_0=always",
		"GIT_CONFIG_KEY_1=init.defaultBranch", "GIT_CONFIG_VALUE_1=main",
		"GIT_CONFIG_KEY_2=advice.detachedHead", "GIT_CONFIG_VALUE_2=false",
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
	}
}

// RemoteURL is the clone url of a repository of the fixture.
func (f *Fixture) RemoteURL(name string) string {
	return "file://" + filepath.Join(f.Remotes, name+".git")
}

// Git runs git in dir with the fixture's environment and returns trimmed
// output.
func (f *Fixture) Git(dir string, args ...string) string {
	f.t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), f.Env()...)
	out, err := command.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *Fixture) memberPath(m Member) string {
	if m.Path != "" {
		return m.Path
	}
	return m.Name
}

func (f *Fixture) buildMember(m Member) {
	f.t.Helper()
	bare := filepath.Join(f.Remotes, m.Name+".git")
	f.Git(f.Remotes, "init", "-q", "--bare", bare)
	seed := filepath.Join(f.Seeds, m.Name)
	f.copyFixture(m.Fixture, seed)
	f.Git(seed, "init", "-q", "-b", "main")
	f.Git(seed, "add", "-A")
	f.Git(seed, "commit", "-qm", "initial "+m.Name)
	f.Git(seed, "remote", "add", "origin", bare)
	head := f.Git(seed, "rev-parse", "HEAD")
	if m.Arch {
		f.writeArch(seed, m.Name, m.Requirement, head, oabranch.Branch(m.Name), m.Interactions...)
	}
	if m.FeatureArch != "" {
		f.writeArch(seed, m.Name, m.Requirement, head, oabranch.BranchFor(m.Name, m.FeatureArch, "main"), m.Interactions...)
	}
	if m.Policy {
		files := m.PolicyFiles
		if len(files) == 0 {
			files = map[string][]byte{policy.PoliciesPath: []byte("repository: " + m.Name + "\n")}
		}
		f.commitBranch(seed, policy.Branch(m.Name), files, "policy: initial library of "+m.Name+"\n")
	}
	f.Git(seed, "push", "-q", "origin", "--all")
}

func (f *Fixture) buildRoot() {
	f.t.Helper()
	spec := f.Spec
	bare := filepath.Join(f.Remotes, spec.Root+".git")
	f.Git(f.Remotes, "init", "-q", "--bare", bare)
	seed := filepath.Join(f.Seeds, spec.Root)
	f.copyFixture("root", seed)
	f.Git(seed, "init", "-q", "-b", "main")
	f.Git(seed, "add", "-A")
	f.Git(seed, "commit", "-qm", "initial org root")
	f.Git(seed, "remote", "add", "origin", bare)
	for _, member := range spec.Members {
		f.Git(seed, "submodule", "add", "-q", f.RemoteURL(member.Name), f.memberPath(member))
		f.Git(seed, "commit", "-qm", "add "+member.Name+" as a submodule")
	}
	if spec.RootArch {
		f.writeArch(seed, spec.Root, "The org root records which commit of each repository is current.", f.Git(seed, "rev-parse", "HEAD"), oabranch.Branch(spec.Root))
	}
	if spec.RootPolicy {
		policies := spec.RootPolicies
		if policies == "" {
			policies = "repository: " + spec.Root + "\nsubmodules:\n  members: true\n  policies: true\n"
		}
		f.writePolicy(seed, spec.Root, policies)
	}
	f.Git(seed, "push", "-q", "origin", "--all")
}

// Seed is the work repository of a repository of the fixture.
func (f *Fixture) Seed(name string) string { return filepath.Join(f.Seeds, name) }

func (f *Fixture) writeArch(seed, name, requirement, indexed, branch string, interactions ...spec.Interaction) {
	f.t.Helper()
	repository := spec.Repository{}
	repository.Name = name
	repository.Namespace = "default"
	repository.Status.IndexedCommit = indexed
	repository.Status.HeadCommit = indexed
	repository.Status.Phase = "Ready"
	context := spec.SystemContext{}
	context.Name = name
	context.Namespace = "default"
	context.Spec = spec.SystemContextSpec{
		Repository: name,
		Intent:     "The " + name + " repository of the polyrepo.",
		Requirements: []spec.Requirement{
			{ID: "r1", Level: spec.LevelMust, Text: requirement},
		},
		Interactions: interactions,
	}
	context.Status.ObservedCommit = indexed
	context.SetDefaults()
	repository.SetDefaults()
	files, err := oabranch.Files(oabranch.Snapshot{Repository: repository, Contexts: []spec.SystemContext{context}, Branch: branch})
	if err != nil {
		f.t.Fatal(err)
	}
	f.commitBranch(seed, branch, files, fmt.Sprintf("spec(%s): index %s\n\nCode-Commit: %s\n", name, indexed[:7], indexed))
}

func (f *Fixture) writePolicy(seed, name, policies string) {
	f.t.Helper()
	files := map[string][]byte{
		policy.PoliciesPath: []byte(policies),
		"README.md":         []byte("# policies of " + name + "\n"),
	}
	f.commitBranch(seed, policy.Branch(name), files, "policy: initial library of "+name+"\n")
}

// commitBranch writes files as the next commit of an orphan branch in the seed
// repository, with plumbing, and leaves the work tree alone.
func (f *Fixture) commitBranch(seed, branch string, files map[string][]byte, message string) string {
	f.t.Helper()
	ctx := context.Background()
	store := oagit.Store{Repo: seed}
	ref := "refs/heads/" + branch
	tip, err := store.Tip(ctx, ref)
	if err != nil {
		f.t.Fatal(err)
	}
	blobs, err := store.Blobs(ctx, tip)
	if err != nil {
		f.t.Fatal(err)
	}
	plan := oabranch.PlanCommit(blobs, files)
	if plan.Empty() {
		return tip
	}
	commit, err := store.Commit(ctx, ref, tip, plan, message)
	if err != nil {
		f.t.Fatal(err)
	}
	return commit
}

// Advance commits files to a member, re-indexes its architecture the way specd
// would, pushes the member, and records the new pointer in the root with a
// root commit. It returns the member commit and the root commit.
func (f *Fixture) Advance(name string, files map[string]string, message string) (string, string) {
	f.t.Helper()
	member := f.member(name)
	seed := f.Seed(name)
	for path, contents := range files {
		full := filepath.Join(seed, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	f.Git(seed, "add", "-A")
	f.Git(seed, "commit", "-qm", message)
	head := f.Git(seed, "rev-parse", "HEAD")
	if member.Arch {
		f.writeArch(seed, name, member.Requirement, head, oabranch.Branch(name), member.Interactions...)
	}
	f.Git(seed, "push", "-q", "origin", "--all")

	rootSeed := f.Seed(f.Spec.Root)
	checkout := filepath.Join(rootSeed, f.memberPath(member))
	f.Git(checkout, "fetch", "-q", "origin")
	f.Git(checkout, "checkout", "-q", head)
	f.Git(rootSeed, "add", f.memberPath(member))
	f.Git(rootSeed, "commit", "-qm", fmt.Sprintf("bump %s: %s", name, message))
	rootCommit := f.Git(rootSeed, "rev-parse", "HEAD")
	f.Git(rootSeed, "push", "-q", "origin", "main")
	return head, rootCommit
}

func (f *Fixture) member(name string) Member {
	for _, m := range f.Spec.Members {
		if m.Name == name {
			return m
		}
	}
	f.t.Fatalf("orgfixture: no member %s", name)
	return Member{}
}

// Path is the checkout path of a member inside a clone of the root.
func (f *Fixture) Path(name string) string { return f.memberPath(f.member(name)) }

// Clone clones the root recursively into a new directory of the fixture. Depth
// zero is a full clone.
func (f *Fixture) Clone(label string, depth int) string {
	f.t.Helper()
	dir := filepath.Join(f.Dir, "clones", label, f.Spec.Root)
	args := []string{"clone", "-q", "--recurse-submodules"}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprint(depth), "--shallow-submodules")
	}
	args = append(args, f.RemoteURL(f.Spec.Root), dir)
	f.Git(f.Dir, args...)
	return dir
}

func (f *Fixture) copyFixture(name, target string) {
	f.t.Helper()
	source := filepath.Join(repoRoot(f.t), "fixtures", "orgroot", name)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
	if err != nil {
		f.t.Fatalf("copy fixture %s: %v", name, err)
	}
}

func repoRoot(t testing.TB) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the repository root")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// FakeCodegraph builds test/fakecodegraph and puts it first on PATH as
// "codegraph", so code that indexes a checkout works where the real indexer is
// not installed. It does nothing when a real codegraph is already on PATH.
func FakeCodegraph(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("codegraph"); err == nil {
		return
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "codegraph"), "./test/fakecodegraph")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the fake codegraph: %v\n%s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
