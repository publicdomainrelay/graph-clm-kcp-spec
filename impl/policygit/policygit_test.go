package policygit_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "README.md")
	run(t, dir, "-c", "user.name=test", "-c", "user.email=test@localhost", "commit", "-q", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func TestInitReadAndUpdate(t *testing.T) {
	repo := gitRepo(t)
	store := oagit.Store{Repo: repo}
	ref := policy.Ref("market")
	ctx := context.Background()

	commit, changed, err := policygit.Init(ctx, store, "market", ref, policy.PolicyLibrary{Version: "1"}, policyeval.Lib(), policyeval.LibTest())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || commit == "" {
		t.Fatal("init wrote nothing")
	}
	if parents := run(t, repo, "rev-list", "--parents", "-n", "1", commit); len(parents) > 42 {
		t.Fatalf("the policy branch is not an orphan: %q", parents)
	}

	library, read, err := policygit.Read(ctx, store, ref)
	if err != nil {
		t.Fatal(err)
	}
	if read != commit {
		t.Fatalf("read commit %s, want %s", read, commit)
	}
	if library.Manifest.Repository != "market" || library.Manifest.Version != "1" {
		t.Fatalf("manifest: %+v", library.Manifest)
	}
	if library.Lib != policyeval.Lib() {
		t.Fatal("lib not read back")
	}

	again, changed, err := policygit.Init(ctx, store, "market", ref, policy.PolicyLibrary{Version: "1"}, policyeval.Lib(), policyeval.LibTest())
	if err != nil {
		t.Fatal(err)
	}
	if changed || again != commit {
		t.Fatalf("a second init changed the branch: %s != %s", again, commit)
	}

	second, err := policygit.WriteChange(ctx, store, ref, "pc-1", []byte("name: pc-1\n"), "policy(market): change pc-1\n")
	if err != nil {
		t.Fatal(err)
	}
	if second == commit {
		t.Fatal("write did not commit")
	}
	history := run(t, repo, "rev-list", "--count", second)
	if history != "2\n" {
		t.Fatalf("history: %q", history)
	}
	library, _, err = policygit.Read(ctx, store, ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := library.Files[policy.ChangePath("pc-1")]; !ok {
		t.Fatalf("change not persisted: %v", library.Files)
	}
	if _, ok := library.Files[policy.PoliciesPath]; !ok {
		t.Fatal("append-only update dropped an existing file")
	}
}

func TestReadMissingBranch(t *testing.T) {
	repo := gitRepo(t)
	store := oagit.Store{Repo: repo}
	library, commit, err := policygit.Read(context.Background(), store, policy.Ref("absent"))
	if err != nil {
		t.Fatal(err)
	}
	if commit != "" || len(library.Templates) != 0 {
		t.Fatalf("a missing branch must read empty: %s %+v", commit, library)
	}
}

func TestIsPolicyPath(t *testing.T) {
	for _, name := range []string{
		policy.PoliciesPath,
		policy.LibPath,
		policy.TemplateSourcePath("relay-only"),
		policy.ConstraintPath("relay-only"),
		policy.SuitePath("relay-only"),
		policy.DistPath("relay-only"),
		policy.ReportPath("main"),
		policy.ChangePath("pc-1"),
		policy.CataloguePath,
	} {
		if !policygit.IsPolicyPath(name) {
			t.Fatalf("%s is not recognised as a policy path", name)
		}
	}
	if policygit.IsPolicyPath("src/main.ts") {
		t.Fatal("a code path was taken for a policy path")
	}
}
