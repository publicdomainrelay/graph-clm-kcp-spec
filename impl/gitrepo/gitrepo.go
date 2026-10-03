package gitrepo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func Head(ctx context.Context, path string) (string, error) {
	return run(ctx, path, "rev-parse", "--verify", "HEAD")
}

func Branch(ctx context.Context, path string) (string, error) {
	return run(ctx, path, "rev-parse", "--abbrev-ref", "HEAD")
}

func IsRepo(ctx context.Context, path string) bool {
	_, err := run(ctx, path, "rev-parse", "--git-dir")
	return err == nil
}

func run(ctx context.Context, path string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("gitrepo: git %s in %s: %w", strings.Join(args, " "), path, err)
	}
	return strings.TrimSpace(string(output)), nil
}
