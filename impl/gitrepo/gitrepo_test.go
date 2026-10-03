package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
