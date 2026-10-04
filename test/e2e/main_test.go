package e2e

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/runlock"
)

func TestMain(m *testing.M) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: locate the repository root: %v\n", err)
		os.Exit(1)
	}
	path := os.Getenv("SPECD_LIVE_LOCK")
	if path == "" {
		path = filepath.Join(root, runlock.DefaultPath)
	}
	lock, err := runlock.Acquire(path, runlock.Options{Name: "go test ./test/e2e"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}
	createdDocs := ""
	if os.Getenv(statedir.EnvClmDocDir) == "" {
		docs, err := os.MkdirTemp("", "specd-e2e-clm-docs-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
			os.Exit(1)
		}
		os.Setenv(statedir.EnvClmDocDir, docs)
		createdDocs = docs
	}
	mark(os.Getenv("SPECD_LIVE_LOCK_MARK"), "begin")
	code := m.Run()
	mark(os.Getenv("SPECD_LIVE_LOCK_MARK"), "end")
	if err := lock.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: release the live lock: %v\n", err)
	}
	if createdDocs != "" {
		os.RemoveAll(createdDocs)
	}
	os.Exit(code)
}

func mark(path, event string) {
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s %d\n", event, time.Now().UnixNano())
}

func TestPhase12LiveLockMarker(t *testing.T) {
	if os.Getenv("SPECD_LIVE_LOCK_MARK") == "" {
		t.Skip("not a lock proof run")
	}
	time.Sleep(1200 * time.Millisecond)
}

func contextDocPath(repository, context string) string {
	return filepath.Join(os.Getenv(statedir.EnvClmDocDir), repository, context+".md")
}

func assertNoSpecArtefacts(t *testing.T, repoPath string) {
	t.Helper()
	for _, artefact := range []string{".specs", "arch.yaml"} {
		if _, err := os.Stat(filepath.Join(repoPath, artefact)); err == nil {
			t.Errorf("%s appeared in the project tree %s", artefact, repoPath)
		}
	}
	out, err := exec.Command("git", "-C", repoPath, "log", "--all", "--name-only", "--format=", "--not", "--glob=refs/heads/open-architecture/*").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, ".specs/") || line == "arch.yaml" {
			t.Errorf("a code commit carries the spec artefact %s", line)
		}
	}
}
