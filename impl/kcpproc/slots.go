package kcpproc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// MaxInstancesEnv caps how many kcp instances this user runs on the host
	// at once: specctl up, every live test package and every eval start one,
	// and each costs a kcp, a kine and a graph-node.
	MaxInstancesEnv = "SPECD_MAX_INSTANCES"

	// InstanceWaitEnv is how long a start waits for a free slot.
	InstanceWaitEnv = "SPECD_INSTANCE_WAIT"

	SlotDirEnv = "SPECD_SLOT_DIR"

	DefaultInstanceWait = 15 * time.Minute

	slotPoll = time.Second
)

// MaxInstances is the cap: SPECD_MAX_INSTANCES, else half of the CPUs
// and never fewer than two. Zero or less means no cap.
func MaxInstances() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(MaxInstancesEnv))); err == nil {
		return value
	}
	return max(2, runtime.NumCPU()/2)
}

func instanceWait() time.Duration {
	if value, err := time.ParseDuration(os.Getenv(InstanceWaitEnv)); err == nil && value > 0 {
		return value
	}
	return DefaultInstanceWait
}

// SlotDir holds one lease file per slot. It is per user and host-wide, under
// $XDG_RUNTIME_DIR when there is one, so a reboot clears it.
func SlotDir() string {
	if dir := os.Getenv(SlotDirEnv); dir != "" {
		return dir
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "specd-slots")
	}
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "specd", "slots")
	}
	return filepath.Join(os.TempDir(), "specd-slots-"+strconv.Itoa(os.Getuid()))
}

// acquireSlot leases a slot to root and returns once it holds one. A lease
// names the root it serves; it is free again as soon as no kcp, kine or
// graph-node of that root runs, so a crash or a kill never leaks a slot.
func acquireSlot(ctx context.Context, root string, progress io.Writer) error {
	limit := MaxInstances()
	if limit <= 0 {
		return nil
	}
	dir := SlotDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("kcpproc: slot dir: %w", err)
	}
	deadline := time.Now().Add(instanceWait())
	announced := false
	for {
		held, busy, err := tryLease(dir, root, limit)
		if err != nil {
			return err
		}
		if held {
			return nil
		}
		if !announced && progress != nil {
			fmt.Fprintf(progress, "kcpproc: all %d instance slots are busy (%s); waiting for one (set %s to change the cap)\n",
				limit, strings.Join(busy, ", "), MaxInstancesEnv)
			announced = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("kcpproc: no instance slot within %s: %d of %d busy: %s", instanceWait(), len(busy), limit, strings.Join(busy, ", "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(slotPoll):
		}
	}
}

func tryLease(dir, root string, limit int) (bool, []string, error) {
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return false, nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	busy := []string{}
	free := -1
	for slot := 0; slot < limit; slot++ {
		path := filepath.Join(dir, "slot-"+strconv.Itoa(slot))
		data, err := os.ReadFile(path)
		holder := strings.TrimSpace(string(data))
		switch {
		case err != nil || holder == "":
			if free < 0 {
				free = slot
			}
		case holder == root:
			return true, nil, nil
		case len(Running(holder)) == 0 && !freshLease(path):
			_ = os.Remove(path)
			if free < 0 {
				free = slot
			}
		default:
			busy = append(busy, holder)
		}
	}
	if free < 0 {
		sort.Strings(busy)
		return false, busy, nil
	}
	path := filepath.Join(dir, "slot-"+strconv.Itoa(free))
	return true, nil, os.WriteFile(path, []byte(root+"\n"), 0o600)
}

// freshLease keeps a lease that was written moments ago: its kcp may still be
// starting and not visible yet.
func freshLease(path string) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) < 2*time.Minute
}

// ReleaseSlot frees the lease of root at once; a stopped instance frees it
// lazily anyway.
func ReleaseSlot(root string) {
	dir := SlotDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "slot-") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == root {
			_ = os.Remove(path)
		}
	}
}
