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

func TestEnsureCheckoutClonesAndFollowsTheSource(t *testing.T) {
	ctx := context.Background()
	source := tempRepo(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	command := exec.Command("git", "clone", "-q", "--bare", source, bare)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git clone --bare: %v: %s", err, output)
	}

	dir := filepath.Join(t.TempDir(), "cache", "unseen")
	first, err := EnsureCheckout(ctx, "file://"+bare, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !IsRepo(ctx, dir) {
		t.Fatal("the checkout was not cloned")
	}
	if _, err := os.Stat(filepath.Join(dir, "file.txt")); err != nil {
		t.Fatalf("the clone has no working tree: %v", err)
	}

	again, err := EnsureCheckout(ctx, "file://"+bare, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Errorf("HEAD = %s, want %s", again, first)
	}

	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "second")
	git(t, source, "push", "-q", bare, "main")
	third, err := EnsureCheckout(ctx, "file://"+bare, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Error("the checkout did not follow the source")
	}
	contents, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(contents)) != "two" {
		t.Errorf("the working tree = %q, want the new commit", contents)
	}
}

func TestEnsureCheckoutHonoursARef(t *testing.T) {
	ctx := context.Background()
	source := tempRepo(t)
	git(t, source, "tag", "v1")
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "second")

	dir := filepath.Join(t.TempDir(), "cache", "ref")
	if _, err := EnsureCheckout(ctx, source, "v1", dir); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(contents)) != "one" {
		t.Errorf("the working tree = %q, want the tagged commit", contents)
	}
	if _, err := EnsureCheckout(ctx, source, "main", filepath.Join(t.TempDir(), "cache", "again")); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCheckoutRejectsAnEmptyURL(t *testing.T) {
	if _, err := EnsureCheckout(context.Background(), "", "", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("an empty url was accepted")
	}
}

func TestEnsureCheckoutFollowsANamedRef(t *testing.T) {
	ctx := context.Background()
	source := tempRepo(t)
	git(t, source, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(source, "feature.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "feature")
	git(t, source, "checkout", "-q", "main")

	dir := filepath.Join(t.TempDir(), "cache", "ref")
	if _, err := EnsureCheckout(ctx, source, "feature", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Fatalf("the branch was not checked out: %v", err)
	}

	git(t, source, "checkout", "-q", "feature")
	if err := os.WriteFile(filepath.Join(source, "feature.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", "-A")
	git(t, source, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "feature again")
	git(t, source, "checkout", "-q", "main")
	if _, err := EnsureCheckout(ctx, source, "feature", dir); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "feature.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(contents)) != "two" {
		t.Errorf("the checkout = %q, want the branch as it is now", contents)
	}
}

func TestEnsureCheckoutDropsACacheFromAnotherSource(t *testing.T) {
	ctx := context.Background()
	bare := func(name, contents string) string {
		source := tempRepo(t)
		if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, source, "add", "-A")
		git(t, source, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-qm", "content")
		target := filepath.Join(t.TempDir(), name+".git")
		command := exec.Command("git", "clone", "-q", "--bare", source, target)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git clone --bare: %v: %s", err, output)
		}
		return target
	}
	first := bare("first", "from the first source\n")
	second := bare("second", "from the second source\n")

	dir := filepath.Join(t.TempDir(), "cache", "unseen")
	if _, err := EnsureCheckout(ctx, "file://"+first, "", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureCheckout(ctx, "file://"+second, "", dir); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(contents)) != "from the second source" {
		t.Fatalf("the cache still holds the first source: %q", contents)
	}
}

func TestCommitAllLeavesTheCodegraphIndexOutWhateverIgnoresIt(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		repo := tempRepo(t)
		if excluded {
			exclude := filepath.Join(repo, ".git", "info", "exclude")
			if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(exclude, []byte(".codegraph/\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Join(repo, ".codegraph"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, ".codegraph", "codegraph.db"), []byte("index"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "new.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		commit, err := CommitAll(context.Background(), repo, "work")
		if err != nil {
			t.Fatalf("excluded=%v: %v", excluded, err)
		}
		out, err := exec.Command("git", "-C", repo, "show", "--name-only", "--format=", commit).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(out)); got != "new.go" {
			t.Fatalf("excluded=%v: the commit carries %q, want new.go only", excluded, got)
		}
	}
}
