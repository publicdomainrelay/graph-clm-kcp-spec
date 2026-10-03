package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/runlock"
)

// TestMain is where the live suite takes the one lock that keeps two runs from
// corrupting each other. Every test in this package drives controllers against
// the same kcp workspace and the same working trees, so a second run started
// while the first is in flight does not measure twice: it overwrites the
// objects and the trees the first one is reading. The lock is held for the
// whole package, and the kernel drops it if the run is killed.
//
// A run started while another holds the lock prints one line and waits; the
// same lock is taken by `specctl eval`, so an eval and a live suite cannot
// collide either.
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
	mark(os.Getenv("SPECD_LIVE_LOCK_MARK"), "begin")
	code := m.Run()
	mark(os.Getenv("SPECD_LIVE_LOCK_MARK"), "end")
	if err := lock.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: release the live lock: %v\n", err)
	}
	os.Exit(code)
}

// mark appends one timestamped line to the file a lock proof watches. It is a
// no-op unless that proof started this process, so an ordinary run writes
// nothing.
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

// TestPhase12LiveLockMarker holds the package's lock for a fixed window when a
// proof started the run. The proof runs two of these at once and reads the
// begin and end marks TestMain wrote around each, so the window is where the
// lock is really held rather than where a test happens to be.
func TestPhase12LiveLockMarker(t *testing.T) {
	if os.Getenv("SPECD_LIVE_LOCK_MARK") == "" {
		t.Skip("not a lock proof run")
	}
	time.Sleep(1200 * time.Millisecond)
}
