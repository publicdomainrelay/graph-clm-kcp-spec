// Package runlock is the one lock a live suite takes while it runs. The live
// tests and the eval both start kcp controllers against the same workspace and
// write the same objects and working trees, so two of them at once do not
// measure twice: they corrupt each other. The lock is an flock on a file, which
// the kernel releases even when a run is killed, so a crashed run leaves no
// stale lock behind.
package runlock

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// DefaultName is the file the lock is taken on, relative to the repository
// root. It lives beside kcp's own state, which is git-ignored.
const DefaultPath = ".kcp-specd/live.lock"

type Options struct {
	// Name is what the waiting message calls this run.
	Name string

	// Wait receives the one line printed when the lock is held and this run
	// has to wait. Nil means standard error.
	Wait io.Writer
}

type Lock struct {
	file *os.File
	path string
}

// Path is the file the lock was taken on.
func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Acquire takes the exclusive lock on path, creating the file and its
// directory as needed. It does not return until the lock is held: when another
// run holds it, one line naming the file goes to Options.Wait first, so a
// person sees why the suite is quiet and that it will proceed.
func Acquire(path string, options Options) (*Lock, error) {
	if path == "" {
		path = DefaultPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("runlock: create %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("runlock: open %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return nil, fmt.Errorf("runlock: lock %s: %w", path, err)
		}
		wait := options.Wait
		if wait == nil {
			wait = os.Stderr
		}
		name := options.Name
		if name == "" {
			name = "this run"
		}
		fmt.Fprintf(wait, "runlock: %s waits for the live lock %s, held by another live run; it proceeds when that run ends\n", name, path)
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
			file.Close()
			return nil, fmt.Errorf("runlock: wait for %s: %w", path, err)
		}
	}
	return &Lock{file: file, path: path}, nil
}

// Release drops the lock. The file is left in place: it is the meeting point,
// not the state.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	if closeErr := l.file.Close(); err == nil {
		err = closeErr
	}
	l.file = nil
	return err
}
