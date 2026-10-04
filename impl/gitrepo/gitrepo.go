package gitrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	AuthorName  = "specd"
	AuthorEmail = "specd@localhost"

	BranchPrefix = "spec/"

	codegraphDir = ".codegraph"
)

func Head(ctx context.Context, path string) (string, error) {
	return run(ctx, path, "rev-parse", "--verify", "HEAD")
}

func EnsureCheckout(ctx context.Context, url, ref, dir string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("gitrepo: a git source needs a url")
	}
	if !strings.Contains(url, "://") && !filepath.IsAbs(url) {
		absolute, err := filepath.Abs(url)
		if err != nil {
			return "", fmt.Errorf("gitrepo: resolve %s: %w", url, err)
		}
		url = absolute
	}
	if _, err := os.Stat(dir); err == nil && !IsRepo(ctx, dir) {
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("gitrepo: clear %s: %w", dir, err)
		}
	}
	if IsRepo(ctx, dir) {
		if origin, err := remoteURL(ctx, dir); err == nil && origin != url {
			if err := os.RemoveAll(dir); err != nil {
				return "", fmt.Errorf("gitrepo: clear %s: %w", dir, err)
			}
		}
	}
	if !IsRepo(ctx, dir) {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", fmt.Errorf("gitrepo: create %s: %w", filepath.Dir(dir), err)
		}
		if _, err := run(ctx, filepath.Dir(dir), "clone", "--quiet", "--", url, dir); err != nil {
			return "", err
		}
	} else if _, err := run(ctx, dir, "fetch", "--quiet", "--all", "--prune", "--tags"); err != nil {
		return "", err
	}
	if err := checkout(ctx, dir, ref); err != nil {
		return "", err
	}
	return Head(ctx, dir)
}

func remoteURL(ctx context.Context, dir string) (string, error) {
	output, err := run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func checkout(ctx context.Context, dir, ref string) error {
	if ref != "" {
		if _, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+ref); err == nil {
			_, err := run(ctx, dir, "checkout", "--force", "--quiet", "-B", ref, "origin/"+ref)
			return err
		}
		_, err := run(ctx, dir, "checkout", "--force", "--quiet", ref)
		return err
	}
	if _, err := run(ctx, dir, "merge", "--ff-only", "--quiet", "@{u}"); err != nil {
		if _, fallbackErr := run(ctx, dir, "reset", "--hard", "--quiet", "origin/HEAD"); fallbackErr != nil {
			return err
		}
	}
	return nil
}

func Branch(ctx context.Context, path string) (string, error) {
	return run(ctx, path, "rev-parse", "--abbrev-ref", "HEAD")
}

func IsRepo(ctx context.Context, path string) bool {
	_, err := run(ctx, path, "rev-parse", "--git-dir")
	return err == nil
}

func WorktreeAdd(ctx context.Context, repo, dir, branch, base string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("gitrepo: clear %s: %w", dir, err)
	}
	_, err := run(ctx, repo, "worktree", "add", "--force", "-B", branch, dir, base)
	return err
}

func WorktreeRemove(ctx context.Context, repo, dir string) error {
	if _, err := run(ctx, repo, "worktree", "remove", "--force", dir); err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("gitrepo: remove worktree %s: %w", dir, err)
		}
	}
	_, err := run(ctx, repo, "worktree", "prune")
	return err
}

func CommitAll(ctx context.Context, dir, message string) (string, error) {
	if _, err := run(ctx, dir, "add", "-A", "--", ".", ":!"+codegraphDir); err != nil {
		return "", err
	}
	if _, err := run(ctx, dir, "diff", "--cached", "--quiet"); err == nil {
		return "", nil
	}
	if _, err := run(ctx, dir,
		"-c", "user.name="+AuthorName,
		"-c", "user.email="+AuthorEmail,
		"commit", "-q", "--author="+AuthorName+" <"+AuthorEmail+">", "-m", message); err != nil {
		return "", err
	}
	return Head(ctx, dir)
}

func ChangedFiles(ctx context.Context, dir, base, commit string) ([]string, error) {
	if base == "" || commit == "" || base == commit {
		return nil, nil
	}
	output, err := run(ctx, dir, "diff", "--name-only", base, commit)
	if err != nil {
		return nil, err
	}
	if output == "" {
		return nil, nil
	}
	return strings.Split(output, "\n"), nil
}

func ChangedLines(ctx context.Context, dir, base, commit string) (string, error) {
	if base == "" || commit == "" || base == commit {
		return "", nil
	}
	return run(ctx, dir, "diff", "--shortstat", base, commit)
}

func FastForward(ctx context.Context, repo, branch, commit string) error {
	current, err := Branch(ctx, repo)
	if err != nil {
		return err
	}
	if current == branch {
		if _, err := run(ctx, repo, "merge", "--ff-only", commit); err != nil {
			return fmt.Errorf("gitrepo: fast-forward %s to %s: %w", branch, commit, err)
		}
		return nil
	}
	if _, err := run(ctx, repo, "merge-base", "--is-ancestor", "refs/heads/"+branch, commit); err != nil {
		return fmt.Errorf("gitrepo: %s cannot fast-forward to %s", branch, commit)
	}
	_, err = run(ctx, repo, "update-ref", "refs/heads/"+branch, commit)
	return err
}

func DeleteBranch(ctx context.Context, repo, branch string) error {
	_, err := run(ctx, repo, "branch", "-D", branch)
	return err
}

func CommitOf(ctx context.Context, repo, ref string) (string, error) {
	return run(ctx, repo, "rev-parse", "--verify", ref)
}

func run(ctx context.Context, path string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gitrepo: git %s in %s: %w: %s", strings.Join(args, " "), path, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
