package runlock_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/runlock"
)

func TestAcquireWaitsForTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "live.lock")
	first, err := runlock.Acquire(path, runlock.Options{Name: "the first run"})
	if err != nil {
		t.Fatal(err)
	}

	wait := &bytes.Buffer{}
	second := make(chan *runlock.Lock, 1)
	failed := make(chan error, 1)
	go func() {
		lock, err := runlock.Acquire(path, runlock.Options{Name: "the second run", Wait: wait})
		if err != nil {
			failed <- err
			return
		}
		second <- lock
	}()

	select {
	case lock := <-second:
		lock.Release()
		t.Fatal("the second acquisition did not wait for the first")
	case err := <-failed:
		t.Fatalf("the second acquisition failed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if !strings.Contains(wait.String(), "waits for the live lock") || !strings.Contains(wait.String(), "the second run") {
		t.Errorf("the wait message = %q", wait.String())
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case lock := <-second:
		if lock.Path() != path {
			t.Errorf("lock path = %q", lock.Path())
		}
		if err := lock.Release(); err != nil {
			t.Fatal(err)
		}
	case err := <-failed:
		t.Fatalf("the second acquisition failed after the release: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("the second acquisition never took the lock")
	}
}

func TestAcquireCreatesTheFileAndItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "live.lock")
	lock, err := runlock.Acquire(path, runlock.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if lock.Path() != path {
		t.Errorf("path = %q, want %q", lock.Path(), path)
	}
}
