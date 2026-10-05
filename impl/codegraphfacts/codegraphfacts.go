package codegraphfacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
)

type Options struct {
	Repository string

	Branch string

	Commit string

	Namespace string

	TestGlobs []string

	Contexts map[string]string

	Tool string

	// IndexInPlace allows the index to be written into a git checkout. Without
	// it a checkout is never written to: a stale index is replaced by one over
	// a copy, so a reader's tree stays theirs and no evaluation sees facts
	// older than the code (review 0003 G5).
	IndexInPlace bool

	DB *codegraphsqlite.DB
}

const commitStamp = "specd-commit"

func Build(ctx context.Context, repoPath string, opts Options) (policy.CodeGraph, error) {
	if opts.DB != nil {
		return FromDB(ctx, opts.DB, repoPath, opts)
	}
	root, cleanup, err := indexRoot(ctx, repoPath, opts)
	if err != nil {
		return policy.CodeGraph{}, err
	}
	defer cleanup()
	database, ok, err := codegraphsqlite.OpenRepo(root)
	if err != nil {
		return policy.CodeGraph{}, err
	}
	if !ok {
		return policy.CodeGraph{}, fmt.Errorf("codegraphfacts: no index at %s", root)
	}
	defer database.Close()
	return FromDB(ctx, database, repoPath, opts)
}

// indexRoot names the tree the index is read from. A git checkout is indexed
// in place only when its recorded index is fresh, or when IndexInPlace asks for
// it; otherwise a copy is indexed, so the reader's tree is never written and no
// stale index is ever read.
func indexRoot(ctx context.Context, repoPath string, opts Options) (string, func(), error) {
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return "", nil, fmt.Errorf("codegraphfacts: resolve %s: %w", repoPath, err)
	}
	noop := func() {}
	if !insideCheckout(root) {
		return root, noop, ensureIndex(ctx, root, opts.Tool)
	}
	if indexFresh(ctx, root) {
		return root, noop, nil
	}
	if opts.IndexInPlace {
		return root, noop, ensureIndex(ctx, root, opts.Tool)
	}
	copyDir, err := os.MkdirTemp("", "specctl-codegraph-")
	if err != nil {
		return "", nil, fmt.Errorf("codegraphfacts: make the index copy: %w", err)
	}
	cleanup := func() { os.RemoveAll(copyDir) }
	if err := copyTree(root, copyDir); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := ensureIndex(ctx, copyDir, opts.Tool); err != nil {
		cleanup()
		return "", nil, err
	}
	return copyDir, cleanup, nil
}

// insideCheckout reports whether a tree is a checkout, or a directory of one.
// The index of a checkout is the reader's own file: it is read when it is fresh
// and replaced by a copy of the tree when it is not, never written.
func insideCheckout(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return true
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// indexFresh reports whether the index in root describes the tree as it is
// now. The stamp records what was indexed: the commit of a checkout, which
// also has to be clean, or the newest source mtime of a plain tree.
func indexFresh(ctx context.Context, root string) bool {
	if _, ok, err := codegraphsqlite.OpenRepo(root); err != nil || !ok {
		return false
	}
	stamp, err := os.ReadFile(filepath.Join(root, codegraphsqlite.Directory, commitStamp))
	if err != nil {
		return false
	}
	value, ok := indexStamp(ctx, root)
	return ok && strings.TrimSpace(string(stamp)) == value
}

func indexStamp(ctx context.Context, root string) (string, bool) {
	if insideCheckout(root) {
		head := GitCommit(ctx, root)
		if head == "" {
			return "", false
		}
		out, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain").Output()
		if err != nil || strings.TrimSpace(string(out)) != "" {
			return "", false
		}
		return head, true
	}
	newest := int64(0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); path != root && (name == ".git" || name == codegraphsqlite.Directory || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if modified := info.ModTime().UnixNano(); modified > newest {
			newest = modified
		}
		return nil
	})
	if err != nil {
		return "", false
	}
	return strconv.FormatInt(newest, 10), true
}

func ensureIndex(ctx context.Context, root, tool string) error {
	if _, err := codegraphsqlite.Ensure(ctx, root, tool); err != nil {
		return err
	}
	value, ok := indexStamp(ctx, root)
	if !ok {
		value = "unknown"
	}
	data := []byte(value + "\n")
	return os.WriteFile(filepath.Join(root, codegraphsqlite.Directory, commitStamp), data, 0o644)
}

// copyTree copies a checkout without its index or its git directory, so the
// index over the copy describes the same code and no artefact lands in the
// original.
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() && (name == ".git" || name == codegraphsqlite.Directory || name == "node_modules") {
			return filepath.SkipDir
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
}

func FromDB(ctx context.Context, database *codegraphsqlite.DB, repoPath string, opts Options) (policy.CodeGraph, error) {
	files, err := database.Files(ctx)
	if err != nil {
		return policy.CodeGraph{}, err
	}
	nodes, err := database.Nodes(ctx)
	if err != nil {
		return policy.CodeGraph{}, err
	}
	edges, err := database.Edges(ctx)
	if err != nil {
		return policy.CodeGraph{}, err
	}

	texts := map[string]string{}
	graph := policy.CodeGraph{
		APIVersion: policy.APIVersion,
		Kind:       policy.CodeGraphKind,
		Metadata: policy.ObjectMeta{
			Name:      opts.Repository,
			Namespace: namespaceOf(opts.Namespace),
			Labels:    labelsOf(opts),
		},
		Spec: policy.CodeGraphSpec{
			Repository: opts.Repository,
			Branch:     opts.Branch,
			Commit:     opts.Commit,
			Files:      []policy.CodeGraphFile{},
			Nodes:      []policy.CodeGraphNode{},
			Edges:      []policy.CodeGraphEdge{},
			Texts:      texts,
		},
	}

	for _, file := range files {
		text, ok := readText(repoPath, file.Path)
		if !ok {
			continue
		}
		sum := sha256.Sum256(text)
		capped := capText(text, policy.FileTextCap)
		texts[file.Path] = capped
		graph.Spec.Files = append(graph.Spec.Files, policy.CodeGraphFile{
			Path:     file.Path,
			Language: file.Language,
			Context:  opts.Contexts[file.Path],
			Test:     isTest(file.Path, opts.TestGlobs),
			SHA256:   hex.EncodeToString(sum[:]),
			Size:     len(text),
		})
	}

	for _, node := range nodes {
		text := ""
		if source, ok := texts[node.FilePath]; ok {
			text = nodeText(source, node.StartLine, node.EndLine)
		}
		out := policy.CodeGraphNode{
			ID:            node.ID,
			Kind:          node.Kind,
			Name:          node.Name,
			QualifiedName: node.QualifiedName,
			File:          node.FilePath,
			StartLine:     node.StartLine,
			EndLine:       node.EndLine,
			Exported:      node.IsExported,
			Context:       opts.Contexts[node.FilePath],
			Text:          text,
		}
		if node.QualifiedName == "" {
			out.QualifiedName = node.Name
		}
		graph.Spec.Nodes = append(graph.Spec.Nodes, out)
	}

	for _, edge := range edges {
		graph.Spec.Edges = append(graph.Spec.Edges, policy.CodeGraphEdge{
			Source: edge.Source,
			Target: edge.Target,
			Kind:   edge.Kind,
			Line:   edge.Line,
		})
	}

	graph.Sort()
	return graph, nil
}

func ContextsByFile(contexts []spec.SystemContext) map[string]string {
	out := map[string]string{}
	for _, context := range contexts {
		for _, file := range context.Status.Observed.Files {
			out[file] = context.Name
		}
		for _, file := range context.Status.Observed.TreeFiles {
			if _, ok := out[file]; !ok {
				out[file] = context.Name
			}
		}
	}
	return out
}

func GitCommit(ctx context.Context, repoPath string) string {
	return git(ctx, repoPath, "rev-parse", "HEAD")
}

func GitBranch(ctx context.Context, repoPath string) string {
	return git(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
}

func git(ctx context.Context, repoPath string, args ...string) string {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...)
	out, err := command.Output()
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(out))
	if value == "HEAD" {
		return ""
	}
	return value
}

func namespaceOf(namespace string) string {
	if namespace == "" {
		return specapi.DefaultNamespace
	}
	return namespace
}

func labelsOf(opts Options) map[string]string {
	labels := map[string]string{policy.RepositoryLabel: opts.Repository}
	if opts.Commit != "" {
		labels[policy.CommitLabel] = opts.Commit
	}
	if opts.Branch != "" {
		labels[policy.BranchLabel] = opts.Branch
	}
	return labels
}

func readText(repoPath, relative string) ([]byte, bool) {
	data, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(relative)))
	if err != nil {
		return nil, false
	}
	return data, true
}

func capText(data []byte, limit int) string {
	if len(data) > limit {
		data = data[:limit]
	}
	return string(data)
}

func nodeText(source string, startLine, endLine int) string {
	if startLine <= 0 || endLine < startLine {
		return ""
	}
	lines := strings.SplitAfter(source, "\n")
	if startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return capText([]byte(strings.Join(lines[startLine-1:endLine], "")), policy.NodeTextCap)
}

func isTest(path string, globs []string) bool {
	for _, glob := range globs {
		if specsync.MatchGlob(glob, path) {
			return true
		}
	}
	return false
}
