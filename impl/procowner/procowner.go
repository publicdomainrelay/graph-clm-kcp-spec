package procowner

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Owner names the specd process that drives a change: a token unique to the
// process, its pid and when it started. A change records it in status while it
// is in flight, so a process that finds the record later can tell whether the
// driver is still alive.
type Owner struct {
	Token string

	Pid int

	Since time.Time
}

func New() Owner {
	return Owner{Token: token(), Pid: os.Getpid(), Since: time.Now().UTC()}
}

func token() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func (o Owner) Owns(token string) bool {
	return token != "" && token == o.Token
}

// Fields is the status patch that records this owner.
func (o Owner) Fields() map[string]any {
	return map[string]any{
		"owner":          o.Token,
		"ownerPid":       o.Pid,
		"ownerStartedAt": o.Since.Format(time.RFC3339),
	}
}

// Cleared is the status patch that removes an owner once the work settles.
func Cleared() map[string]any {
	return map[string]any{
		"owner":          nil,
		"ownerPid":       nil,
		"ownerStartedAt": nil,
	}
}

// Alive reports whether a process with this pid exists. A pid of zero or less
// is never a live owner: kill(0, 0) names the caller's own process group.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Orphaned reports whether work recorded as owned by (token, pid) is no longer
// driven by a live process: this process does not own it and its pid is not
// alive. A record with no token was written before owner tracking, so it is
// orphaned too.
func Orphaned(mine Owner, token string, pid int, alive func(int) bool) bool {
	if mine.Owns(token) {
		return false
	}
	if token == "" {
		return true
	}
	return !alive(pid)
}
