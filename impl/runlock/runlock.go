package runlock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const DefaultPath = ".kcp-specd/live.lock"

func PathFor(stateDir, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(stateDir, "live-"+hex.EncodeToString(sum[:])[:8]+".lock")
}

type Options struct {
	Name string

	Wait io.Writer
}

type Lock struct {
	file *os.File
	path string
}

func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

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
