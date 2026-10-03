// Package gitrepo is the only place this repository runs git against a managed
// working tree. Reading is a rev-parse; the spec -> code direction needs more:
// a worktree on its own branch, a commit the tool signs, the diff it touched,
// and a fast-forward that lands it.
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
	// AuthorName and AuthorEmail sign every commit the tool makes, so a commit
	// an agent wrote is never mistaken for a human's.
	AuthorName  = "specd"
	AuthorEmail = "specd@localhost"

	// BranchPrefix names the branch one SpecToCode change works on.
	BranchPrefix = "spec/"

	// codegraphDir is the index ingest writes beside the code. It is not part
	// of the spec, so it never joins a realize commit.
	codegraphDir = ".codegraph"
)

func Head(ctx context.Context, path string) (string, error) {
	return run(ctx, path, "rev-parse", "--verify", "HEAD")
}

// EnsureCheckout makes dir a checkout of url at ref and returns its commit. A
// missing directory is cloned; an existing one is fetched and moved to the ref
// (or to the remote's default branch when ref is empty), so a Repository whose
// source moves forward is indexed at its new HEAD. It is idempotent, so a
// controller restart re-runs it without harm.
func EnsureCheckout(ctx context.Context, url, ref, dir string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("gitrepo: a git source needs a url")
	}
	// A relative local url has to be resolved against the caller's working
	// directory: the clone runs with -C in the cache's parent, so git would
	// otherwise resolve it there.
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

func checkout(ctx context.Context, dir, ref string) error {
	if ref != "" {
		if _, err := run(ctx, dir, "checkout", "--force", "--quiet", ref); err != nil {
			if _, remoteErr := run(ctx, dir, "checkout", "--force", "--quiet", "origin/"+ref); remoteErr != nil {
				return err
			}
		}
		return nil
	}
	// An empty ref is the remote's default branch. A clone has an upstream, so
	// the fast-forward is what moves a cache forward; a detached checkout has
	// none and is left where the caller put it.
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

// WorktreeAdd checks out a fresh worktree for one change. The branch is reset
// to base, so a retry of an episode starts from the code as it is now and not
// from the half finished tree the failed attempt left behind.
func WorktreeAdd(ctx context.Context, repo, dir, branch, base string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("gitrepo: clear %s: %w", dir, err)
	}
	_, err := run(ctx, repo, "worktree", "add", "--force", "-B", branch, dir, base)
	return err
}

// WorktreeRemove takes a worktree away. The branch stays: a failed change keeps
// it for a human to read, and the next attempt resets it.
func WorktreeRemove(ctx context.Context, repo, dir string) error {
	if _, err := run(ctx, repo, "worktree", "remove", "--force", dir); err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("gitrepo: remove worktree %s: %w", dir, err)
		}
	}
	_, err := run(ctx, repo, "worktree", "prune")
	return err
}

// CommitAll stages everything in a worktree except the codegraph index and
// commits it as specd. It returns the empty string when the agent changed
// nothing, which the caller reads as "there was nothing to do".
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

// ChangedFiles is the diff stat a SpecChange reports: the files one commit
// touched relative to the commit it was built on.
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

// FastForward lands a realize commit on the branch the Repository manages. The
// merge is --ff-only, so it can never rewrite what a human pushed; a branch
// that moved under the change is a failure the caller reports, not a merge it
// resolves.
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

// DeleteBranch drops the branch of a landed change, so a workspace does not
// fill with branches whose work is already in the managed branch.
func DeleteBranch(ctx context.Context, repo, branch string) error {
	_, err := run(ctx, repo, "branch", "-D", branch)
	return err
}

// CommitOf is the commit one branch points at, so a caller can name the base a
// worktree was built on.
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
