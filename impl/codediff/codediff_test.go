package codediff_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codediff"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "test")
	write(t, dir, "keep.txt", "one\ntwo\nthree\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func fileNamed(diff policy.CodeDiff, name string) (policy.CodeDiffFile, bool) {
	for _, file := range diff.Spec.Files {
		if file.Path == name {
			return file, true
		}
	}
	return policy.CodeDiffFile{}, false
}

func TestBuildReportsAddedLinesAtTheirHeadLineNumbers(t *testing.T) {
	dir := repo(t)
	write(t, dir, "keep.txt", "one\ntwo changed\nthree\nfour\n")
	git(t, dir, "commit", "-q", "-am", "edit")

	diff, err := codediff.Build(context.Background(), dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if diff.Kind != policy.CodeDiffKind {
		t.Errorf("kind %q", diff.Kind)
	}
	if diff.Spec.Base == "" || diff.Spec.Head == "" {
		t.Errorf("base/head not resolved: %+v", diff.Spec)
	}
	file, ok := fileNamed(diff, "keep.txt")
	if !ok {
		t.Fatalf("keep.txt missing from %+v", diff.Spec.Files)
	}
	if file.Status != "modified" {
		t.Errorf("status %q", file.Status)
	}
	if len(file.Added) != 2 {
		t.Fatalf("added %+v", file.Added)
	}
	if file.Added[0].Line != 2 || file.Added[0].Text != "two changed" {
		t.Errorf("first added %+v", file.Added[0])
	}
	if file.Added[1].Line != 4 || file.Added[1].Text != "four" {
		t.Errorf("second added %+v", file.Added[1])
	}
	if len(file.Removed) != 1 || file.Removed[0].Line != 2 || file.Removed[0].Text != "two" {
		t.Errorf("removed %+v", file.Removed)
	}
}

func TestBuildReportsAddedAndDeletedFiles(t *testing.T) {
	dir := repo(t)
	write(t, dir, "new file.txt", "hello\n")
	if err := os.Remove(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "add and delete")

	diff, err := codediff.Build(context.Background(), dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	added, ok := fileNamed(diff, "new file.txt")
	if !ok {
		t.Fatalf("new file.txt missing from %+v", diff.Spec.Files)
	}
	if added.Status != "added" {
		t.Errorf("added status %q", added.Status)
	}
	if len(added.Added) != 1 || added.Added[0].Line != 1 || added.Added[0].Text != "hello" {
		t.Errorf("added %+v", added.Added)
	}
	deleted, ok := fileNamed(diff, "keep.txt")
	if !ok {
		t.Fatalf("keep.txt missing from %+v", diff.Spec.Files)
	}
	if deleted.Status != "deleted" {
		t.Errorf("deleted status %q", deleted.Status)
	}
	if len(deleted.Removed) != 3 {
		t.Errorf("removed %+v", deleted.Removed)
	}
}

func TestBuildNeedsABase(t *testing.T) {
	dir := repo(t)
	if _, err := codediff.Build(context.Background(), dir, "", "HEAD"); err == nil {
		t.Fatal("expected an error for a missing base")
	}
}
