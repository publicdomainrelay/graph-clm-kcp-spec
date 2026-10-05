package gitrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

func TrackedFiles(ctx context.Context, path string) ([]string, error) {
	output, err := run(ctx, path, "ls-files", "--cached", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	if output == "" {
		return nil, nil
	}
	files := strings.Split(output, "\n")
	sort.Strings(files)
	return files, nil
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

const worktreePrefix = "specd-worktree-"

// Worktrees lists the worktrees git records for the repository.
func Worktrees(ctx context.Context, repo string) ([]string, error) {
	output, err := run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, line := range strings.Split(output, "\n") {
		path, found := strings.CutPrefix(line, "worktree ")
		if found && path != "" {
			out = append(out, path)
		}
	}
	return out, nil
}

// PruneWorktrees removes the realize worktrees a killed specd left behind: the
// ones under a specd-worktree-* temporary directory, which are the only
// worktrees this controller creates. A registered worktree that still holds the
// realize branch makes the next worktree add fail with "cannot force update the
// branch ... used by worktree", so it has to go before a change is re-driven.
// Worktrees anywhere else, another tool's or an operator's, are left alone.
func PruneWorktrees(ctx context.Context, repo string) error {
	paths, err := Worktrees(ctx, repo)
	if err != nil {
		return err
	}
	roots := map[string]bool{}
	for _, path := range paths {
		root := filepath.Dir(path)
		if !strings.HasPrefix(filepath.Base(root), worktreePrefix) {
			continue
		}
		if err := WorktreeRemove(ctx, repo, path); err != nil {
			return err
		}
		roots[root] = true
	}
	for root := range roots {
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("gitrepo: remove worktree root %s: %w", root, err)
		}
	}
	return nil
}

// DeleteBranchIfExists deletes a branch, and reports no error when there is
// nothing to delete.
func DeleteBranchIfExists(ctx context.Context, repo, branch string) error {
	if _, err := run(ctx, repo, "rev-parse", "--verify", "refs/heads/"+branch); err != nil {
		return nil
	}
	return DeleteBranch(ctx, repo, branch)
}

func CommitAll(ctx context.Context, dir, message string) (string, error) {
	if _, err := run(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := run(ctx, dir, "rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", codegraphDir); err != nil {
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

// Diff is the patch a realized change left, for a reader that has to judge
// whether the code does what the spec asked.
func Diff(ctx context.Context, dir, base, commit string) (string, error) {
	if base == "" || commit == "" || base == commit {
		return "", nil
	}
	return run(ctx, dir, "diff", base, commit)
}

// DiffWorktree is the patch between a commit and the uncommitted worktree, so
// a gate can judge what an agent has written before it commits.
func DiffWorktree(ctx context.Context, dir, base string) (string, error) {
	if base == "" {
		return "", nil
	}
	return run(ctx, dir, "diff", base)
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

func SiblingView(repoPath string) (string, string, error) {
	absolute, err := filepath.Abs(repoPath)
	if err != nil {
		return "", "", err
	}
	root, err := os.MkdirTemp("", "specd-worktree-")
	if err != nil {
		return "", "", fmt.Errorf("gitrepo: create a worktree directory: %w", err)
	}
	parent := filepath.Dir(absolute)
	entries, err := os.ReadDir(parent)
	if err != nil {
		os.RemoveAll(root)
		return "", "", fmt.Errorf("gitrepo: read %s: %w", parent, err)
	}
	name := filepath.Base(absolute)
	for _, entry := range entries {
		if entry.Name() == name {
			continue
		}
		if err := os.Symlink(filepath.Join(parent, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
			os.RemoveAll(root)
			return "", "", fmt.Errorf("gitrepo: link %s beside the worktree: %w", entry.Name(), err)
		}
	}
	return root, filepath.Join(root, name), nil
}

func RebaseOnto(ctx context.Context, dir, tip string) (string, error) {
	if _, err := run(ctx, dir,
		"-c", "user.name="+AuthorName,
		"-c", "user.email="+AuthorEmail,
		"rebase", "--quiet", tip); err != nil {
		_, _ = run(ctx, dir, "rebase", "--abort")
		return "", fmt.Errorf("gitrepo: rebase onto %s: %w", tip, err)
	}
	return Head(ctx, dir)
}
