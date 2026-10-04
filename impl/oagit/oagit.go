package oagit

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
)

const (
	AuthorName = "specd"

	AuthorEmail = "specd@localhost"

	zeroCommit = "0000000000000000000000000000000000000000"
)

var ErrRaced = errors.New("oagit: the branch moved while the commit was being built")

type Store struct {
	Repo string
}

func (s Store) Tip(ctx context.Context, ref string) (string, error) {
	out, _, err := s.git(ctx, nil, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s Store) Blobs(ctx context.Context, commit string) (map[string]string, error) {
	blobs := map[string]string{}
	if commit == "" {
		return blobs, nil
	}
	out, _, err := s.git(ctx, nil, nil, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	for _, entry := range bytes.Split(out, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		meta, path, ok := bytes.Cut(entry, []byte{'\t'})
		if !ok {
			return nil, fmt.Errorf("oagit: unreadable ls-tree entry %q", entry)
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		blobs[string(path)] = fields[2]
	}
	return blobs, nil
}

func (s Store) ReadFiles(ctx context.Context, commit string) (map[string][]byte, error) {
	blobs, err := s.Blobs(ctx, commit)
	if err != nil {
		return nil, err
	}
	return s.ReadFilesAt(ctx, commit, sortedPaths(blobs))
}

// ReadFilesAt reads only the named paths of a commit. A path the commit does
// not hold is absent from the result.
func (s Store) ReadFilesAt(ctx context.Context, commit string, paths []string) (map[string][]byte, error) {
	files := map[string][]byte{}
	if commit == "" || len(paths) == 0 {
		return files, nil
	}
	blobs, err := s.Blobs(ctx, commit)
	if err != nil {
		return nil, err
	}
	wanted := map[string]string{}
	for _, path := range paths {
		if blob, ok := blobs[path]; ok {
			wanted[path] = blob
		}
	}
	if len(wanted) == 0 {
		return files, nil
	}
	request := bytes.Buffer{}
	ordered := make([]string, 0, len(wanted))
	for _, path := range sortedPaths(wanted) {
		request.WriteString(wanted[path] + "\n")
		ordered = append(ordered, path)
	}
	out, _, err := s.git(ctx, &request, nil, "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(bytes.NewReader(out))
	byBlob := map[string][]byte{}
	for range ordered {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("oagit: cat-file header: %w", err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("oagit: cat-file header %q", header)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("oagit: cat-file size %q: %w", header, err)
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, fmt.Errorf("oagit: cat-file body: %w", err)
		}
		if _, err := reader.ReadByte(); err != nil {
			return nil, fmt.Errorf("oagit: cat-file trailer: %w", err)
		}
		byBlob[fields[0]] = data
	}
	for path, blob := range wanted {
		data, ok := byBlob[blob]
		if !ok {
			return nil, fmt.Errorf("oagit: cat-file returned no blob for %s", path)
		}
		files[path] = data
	}
	return files, nil
}

func sortedPaths(blobs map[string]string) []string {
	paths := make([]string, 0, len(blobs))
	for path := range blobs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (s Store) Commit(ctx context.Context, ref, parent string, plan oabranch.Plan, message string) (string, error) {
	scratch, err := os.MkdirTemp("", "oagit-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(scratch, "index")}

	if parent == "" {
		if _, _, err := s.git(ctx, nil, env, "read-tree", "--empty"); err != nil {
			return "", err
		}
	} else if _, _, err := s.git(ctx, nil, env, "read-tree", parent); err != nil {
		return "", err
	}

	index := bytes.Buffer{}
	if len(plan.Write) > 0 {
		paths := make([]string, 0, len(plan.Write))
		list := bytes.Buffer{}
		position := 0
		for path, data := range plan.Write {
			file := filepath.Join(scratch, "blob-"+strconv.Itoa(position))
			position++
			if err := os.WriteFile(file, data, 0o600); err != nil {
				return "", err
			}
			paths = append(paths, path)
			list.WriteString(file + "\n")
		}
		out, _, err := s.git(ctx, &list, nil, "hash-object", "-w", "--no-filters", "--stdin-paths")
		if err != nil {
			return "", err
		}
		ids := strings.Fields(string(out))
		if len(ids) != len(paths) {
			return "", fmt.Errorf("oagit: hash-object wrote %d blobs for %d files", len(ids), len(paths))
		}
		for position, path := range paths {
			if want := oabranch.BlobID(plan.Write[path]); ids[position] != want {
				return "", fmt.Errorf("oagit: blob of %s is %s, want %s", path, ids[position], want)
			}
			fmt.Fprintf(&index, "100644 %s\t%s\n", ids[position], path)
		}
	}
	for _, path := range plan.Remove {
		fmt.Fprintf(&index, "0 %s\t%s\n", zeroCommit, path)
	}
	if index.Len() > 0 {
		if _, _, err := s.git(ctx, &index, env, "update-index", "--index-info"); err != nil {
			return "", err
		}
	}
	treeOut, _, err := s.git(ctx, nil, env, "write-tree")
	if err != nil {
		return "", err
	}
	tree := strings.TrimSpace(string(treeOut))

	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, "-F", "-")
	identity := []string{
		"GIT_AUTHOR_NAME=" + AuthorName, "GIT_AUTHOR_EMAIL=" + AuthorEmail,
		"GIT_COMMITTER_NAME=" + AuthorName, "GIT_COMMITTER_EMAIL=" + AuthorEmail,
	}
	commitOut, _, err := s.git(ctx, bytes.NewBufferString(message), identity, args...)
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(commitOut))

	old := parent
	if old == "" {
		old = zeroCommit
	}
	if _, _, err := s.git(ctx, nil, nil, "update-ref", "-m", "specd: persist", ref, commit, old); err != nil {
		tip, tipErr := s.Tip(ctx, ref)
		if tipErr == nil && tip != parent {
			return "", ErrRaced
		}
		return "", err
	}
	return commit, nil
}

type CommitInfo struct {
	AuthorEmail string

	Parents []string

	Subject string
}

func (s Store) Info(ctx context.Context, commit string) (CommitInfo, error) {
	out, _, err := s.git(ctx, nil, nil, "log", "-1", "--format=%ae%x00%P%x00%s", commit)
	if err != nil {
		return CommitInfo{}, err
	}
	parts := strings.SplitN(strings.TrimRight(string(out), "\n"), "\x00", 3)
	if len(parts) != 3 {
		return CommitInfo{}, fmt.Errorf("oagit: log of %s: %q", commit, out)
	}
	return CommitInfo{AuthorEmail: parts[0], Parents: strings.Fields(parts[1]), Subject: parts[2]}, nil
}

func (s Store) IsAncestor(ctx context.Context, ancestor, commit string) (bool, error) {
	_, _, err := s.git(ctx, nil, nil, "merge-base", "--is-ancestor", ancestor, commit)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func (s Store) Fetch(ctx context.Context, remote, branch string) (string, error) {
	tracking := "refs/remotes/" + remote + "/" + branch
	_, _, err := s.git(ctx, nil, nil, "fetch", "--quiet", "--no-tags", remote, "+refs/heads/"+branch+":"+tracking)
	if err != nil {
		if strings.Contains(err.Error(), "couldn't find remote ref") {
			return "", nil
		}
		return "", err
	}
	return s.Tip(ctx, tracking)
}

func (s Store) Push(ctx context.Context, remote, ref string) error {
	_, _, err := s.git(ctx, nil, nil, "push", "--quiet", remote, ref+":"+ref)
	return err
}

func (s Store) SetRef(ctx context.Context, ref, commit, old string) error {
	if old == "" {
		old = zeroCommit
	}
	_, _, err := s.git(ctx, nil, nil, "update-ref", "-m", "specd: restore", ref, commit, old)
	return err
}

func (s Store) git(ctx context.Context, stdin *bytes.Buffer, env []string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", s.Repo}, args...)...)
	command.Env = append(os.Environ(), env...)
	if stdin != nil {
		command.Stdin = stdin
	}
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("oagit: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func (s Store) IsTopLevel(ctx context.Context) (bool, error) {
	out, _, err := s.git(ctx, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return false, err
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return false, err
	}
	here, err := filepath.Abs(s.Repo)
	if err != nil {
		return false, err
	}
	here, err = filepath.EvalSymlinks(here)
	if err != nil {
		return false, err
	}
	return top == here, nil
}

func (s Store) DefaultBranch(ctx context.Context) string {
	out, _, err := s.git(ctx, nil, nil, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		if name := strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/"); name != "" {
			return name
		}
	}
	for _, candidate := range []string{"main", "master"} {
		if tip, err := s.Tip(ctx, "refs/heads/"+candidate); err == nil && tip != "" {
			return candidate
		}
	}
	return "main"
}

func (s Store) CurrentBranch(ctx context.Context) string {
	out, _, err := s.git(ctx, nil, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
