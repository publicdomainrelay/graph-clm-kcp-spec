package oagit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "main.go")
	run(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "code")
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFiles(t *testing.T, store Store, ref string, files map[string][]byte, message string) string {
	t.Helper()
	ctx := context.Background()
	tip, err := store.Tip(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := store.Blobs(ctx, tip)
	if err != nil {
		t.Fatal(err)
	}
	plan := oabranch.PlanCommit(blobs, files)
	if plan.Empty() {
		return tip
	}
	commit, err := store.Commit(ctx, ref, tip, plan, message)
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

func TestCommitCreatesAnOrphanBranchAndLeavesTheWorktreeAlone(t *testing.T) {
	repo := gitRepo(t)
	store := Store{Repo: repo}
	ref := oabranch.Ref("calc")
	statusBefore := run(t, repo, "status", "--porcelain")
	headBefore := run(t, repo, "rev-parse", "HEAD")

	first := commitFiles(t, store, ref, map[string][]byte{
		"specs/calc.yaml":   []byte("spec: one\n"),
		"context/calc.md":   []byte("# calc\n"),
		"graph/edges.jsonl": []byte(""),
	}, "first")

	if out, err := exec.Command("git", "-C", repo, "merge-base", "main", ref).CombinedOutput(); err == nil {
		t.Fatalf("the branch shares history with main: %s", out)
	}
	if got := run(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Fatalf("the worktree changed: %q", got)
	}
	if got := run(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatal("HEAD moved")
	}
	if got := run(t, repo, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
		t.Fatalf("checked out %s", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "specs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a spec file appeared in the worktree")
	}
	if got := run(t, repo, "show", ref+":specs/calc.yaml"); got != "spec: one" {
		t.Fatalf("specs/calc.yaml = %q", got)
	}
	if got := run(t, repo, "log", "-1", "--format=%an <%ae>", ref); got != "specd <specd@localhost>" {
		t.Fatalf("author %q", got)
	}

	second := commitFiles(t, store, ref, map[string][]byte{
		"specs/calc.yaml": []byte("spec: two\n"),
		"context/calc.md": []byte("# calc\n"),
	}, "second")
	if second == first {
		t.Fatal("a change made no commit")
	}
	if got := run(t, repo, "rev-parse", second+"^"); got != first {
		t.Fatalf("parent %s, want %s", got, first)
	}
	files, err := store.ReadFiles(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if string(files["specs/calc.yaml"]) != "spec: two\n" || string(files["context/calc.md"]) != "# calc\n" {
		t.Fatalf("files = %q", files)
	}
	if _, ok := files["graph/edges.jsonl"]; ok {
		t.Fatal("a removed file survived")
	}

	third := commitFiles(t, store, ref, map[string][]byte{
		"specs/calc.yaml": []byte("spec: two\n"),
		"context/calc.md": []byte("# calc\n"),
	}, "third")
	if third != second {
		t.Fatal("an identical state made a commit")
	}
}

func TestCommitRefusesAStaleParent(t *testing.T) {
	repo := gitRepo(t)
	store := Store{Repo: repo}
	ref := oabranch.Ref("calc")
	first := commitFiles(t, store, ref, map[string][]byte{"a": []byte("1")}, "first")
	commitFiles(t, store, ref, map[string][]byte{"a": []byte("2")}, "second")
	ctx := context.Background()
	blobs, err := store.Blobs(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	plan := oabranch.PlanCommit(blobs, map[string][]byte{"a": []byte("3")})
	if _, err := store.Commit(ctx, ref, first, plan, "stale"); !errors.Is(err, ErrRaced) {
		t.Fatalf("err = %v, want ErrRaced", err)
	}
	if got := run(t, repo, "show", ref+":a"); got != "2" {
		t.Fatalf("a stale writer overwrote the branch: %q", got)
	}
}

func TestTipOfAMissingBranchIsEmpty(t *testing.T) {
	repo := gitRepo(t)
	tip, err := Store{Repo: repo}.Tip(context.Background(), oabranch.Ref("nothing"))
	if err != nil || tip != "" {
		t.Fatalf("tip = %q, %v", tip, err)
	}
}

func TestNestedPathsAndALinkedWorktree(t *testing.T) {
	repo := gitRepo(t)
	worktree := filepath.Join(t.TempDir(), "wt")
	run(t, repo, "worktree", "add", "-q", "-b", "work", worktree)
	store := Store{Repo: worktree}
	ref := oabranch.Ref("calc")
	commitFiles(t, store, ref, map[string][]byte{"graph/vertices.jsonl": []byte("{}\n"), "specs/deep/x.yaml": []byte("x\n")}, "nested")
	if got := run(t, repo, "show", ref+":specs/deep/x.yaml"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := run(t, worktree, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree dirty: %q", got)
	}
}
