package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func tempRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "first")
	return dir
}

func TestHeadAndBranch(t *testing.T) {
	ctx := context.Background()
	dir := tempRepo(t)

	if !IsRepo(ctx, dir) {
		t.Fatal("a fresh git repository must be recognised")
	}
	head, err := Head(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(head) != 40 {
		t.Fatalf("head = %q, want a full sha", head)
	}
	branch, err := Branch(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "second")
	moved, err := Head(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if moved == head {
		t.Fatal("head must follow a new commit")
	}
}

func TestWorktreeCommitFastForward(t *testing.T) {
	ctx := context.Background()
	repo := tempRepo(t)
	base, err := Head(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	branch := BranchPrefix + "calc/abcdef12"
	if err := WorktreeAdd(ctx, repo, worktree, branch, base); err != nil {
		t.Fatalf("worktree add: %v", err)
	}
	t.Cleanup(func() { _ = WorktreeRemove(ctx, repo, worktree) })

	// The index ingest writes is never part of a realize commit.
	if err := os.MkdirAll(filepath.Join(worktree, codegraphDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, codegraphDir, "codegraph.db"), []byte("index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "calc.go"), []byte("package calc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	commit, err := CommitAll(ctx, worktree, "realize calc")
	if err != nil {
		t.Fatal(err)
	}
	if commit == "" {
		t.Fatal("a worktree with an edit must produce a commit")
	}
	author, err := run(ctx, worktree, "log", "-1", "--format=%an <%ae>")
	if err != nil {
		t.Fatal(err)
	}
	if author != AuthorName+" <"+AuthorEmail+">" {
		t.Errorf("author = %q, want the tool's own", author)
	}
	tracked, err := run(ctx, worktree, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tracked, codegraphDir) {
		t.Errorf("the codegraph index was committed: %s", tracked)
	}
	if !strings.Contains(tracked, "calc.go") {
		t.Errorf("the agent's file was not committed: %s", tracked)
	}

	files, err := ChangedFiles(ctx, worktree, base, commit)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "calc.go" {
		t.Errorf("changed files = %v, want [calc.go]", files)
	}
	stat, err := ChangedLines(ctx, worktree, base, commit)
	if err != nil || stat == "" {
		t.Errorf("shortstat = %q, %v", stat, err)
	}

	// A second commit with nothing staged is not an empty commit.
	if again, err := CommitAll(ctx, worktree, "realize calc"); err != nil {
		t.Fatal(err)
	} else if again != "" {
		t.Errorf("an unchanged tree produced the commit %s", again)
	}

	if err := WorktreeRemove(ctx, repo, worktree); err != nil {
		t.Fatalf("worktree remove: %v", err)
	}
	if err := FastForward(ctx, repo, "main", commit); err != nil {
		t.Fatalf("fast-forward: %v", err)
	}
	landed, err := Head(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if landed != commit {
		t.Errorf("head = %s, want the realize commit %s", landed, commit)
	}
	if _, err := os.Stat(filepath.Join(repo, "calc.go")); err != nil {
		t.Errorf("the managed tree did not move: %v", err)
	}
	if err := DeleteBranch(ctx, repo, branch); err != nil {
		t.Fatalf("delete branch: %v", err)
	}
}

func TestFastForwardRefusesADivergedBranch(t *testing.T) {
	ctx := context.Background()
	repo := tempRepo(t)
	base, err := Head(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if err := WorktreeAdd(ctx, repo, worktree, "spec/calc/1", base); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "agent.txt"), []byte("agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit, err := CommitAll(ctx, worktree, "realize")
	if err != nil {
		t.Fatal(err)
	}
	if err := WorktreeRemove(ctx, repo, worktree); err != nil {
		t.Fatal(err)
	}

	// A human pushes something on top of the branch the change was built on.
	if err := os.WriteFile(filepath.Join(repo, "human.txt"), []byte("human\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.email=human@example.com", "-c", "user.name=human", "commit", "-qm", "human")
	if err := FastForward(ctx, repo, "main", commit); err == nil {
		t.Fatal("a diverged branch must not be fast-forwarded")
	}
}

func TestErrorsOutsideARepository(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if IsRepo(ctx, dir) {
		t.Fatal("a plain directory is not a repository")
	}
	if _, err := Head(ctx, dir); err == nil {
		t.Fatal("head outside a repository must fail")
	}
	if _, err := Branch(ctx, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("branch in a missing directory must fail")
	}
}
