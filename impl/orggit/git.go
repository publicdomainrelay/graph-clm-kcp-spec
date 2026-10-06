package orggit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// run executes git in dir and returns trimmed stdout.
func (r *Root) run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := r.output(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

func (r *Root) output(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	command.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), r.Env...)
	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return stdout.String(), &gitError{args: args, dir: dir, err: err, stderr: strings.TrimSpace(stderr.String())}
	}
	return stdout.String(), nil
}

type gitError struct {
	args   []string
	dir    string
	err    error
	stderr string
}

func (e *gitError) Error() string {
	return fmt.Sprintf("orggit: git %s in %s: %v: %s", strings.Join(e.args, " "), e.dir, e.err, e.stderr)
}

func (e *gitError) Unwrap() error { return e.err }

// exitCode reports a git exit status and whether err was an exit at all.
func exitCode(err error) (int, bool) {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), true
	}
	return 0, false
}

// Git runs git in dir (the root, or a member's checkout) with the root's
// environment and returns trimmed output.
func (r *Root) Git(ctx context.Context, dir string, args ...string) (string, error) {
	return r.run(ctx, dir, args...)
}
