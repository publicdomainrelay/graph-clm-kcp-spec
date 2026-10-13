package kcpproc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlotsCapInstancesAndFreeWhenTheirKcpIsGone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(SlotDirEnv, dir)
	t.Setenv(MaxInstancesEnv, "1")
	t.Setenv(InstanceWaitEnv, "1s")

	first, second := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := acquireSlot(context.Background(), first, nil); err != nil {
		t.Fatal(err)
	}
	if err := acquireSlot(context.Background(), first, nil); err != nil {
		t.Fatalf("the holder could not take its own slot again: %v", err)
	}
	err := acquireSlot(context.Background(), second, nil)
	if err == nil || !strings.Contains(err.Error(), first) {
		t.Fatalf("a second instance got a slot past the cap of one: %v", err)
	}

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "slot-0"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := acquireSlot(context.Background(), second, nil); err != nil {
		t.Fatalf("a lease whose kcp is gone still held the slot: %v", err)
	}

	ReleaseSlot(second)
	if data, err := os.ReadFile(filepath.Join(dir, "slot-0")); err == nil {
		t.Fatalf("ReleaseSlot left %q", data)
	}
}

func TestNoCapWhenMaxInstancesIsZero(t *testing.T) {
	t.Setenv(SlotDirEnv, t.TempDir())
	t.Setenv(MaxInstancesEnv, "0")
	for _, root := range []string{"/x/a", "/x/b", "/x/c"} {
		if err := acquireSlot(context.Background(), root, nil); err != nil {
			t.Fatal(err)
		}
	}
}
