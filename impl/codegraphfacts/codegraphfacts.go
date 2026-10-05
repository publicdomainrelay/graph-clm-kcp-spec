package codegraphfacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	DB *codegraphsqlite.DB
}

func Build(ctx context.Context, repoPath string, opts Options) (policy.CodeGraph, error) {
	database := opts.DB
	if database == nil {
		existing, ok, err := codegraphsqlite.OpenRepo(repoPath)
		if err != nil {
			return policy.CodeGraph{}, err
		}
		if !ok {
			if _, err := codegraphsqlite.Ensure(ctx, repoPath, opts.Tool); err != nil {
				return policy.CodeGraph{}, err
			}
			existing, ok, err = codegraphsqlite.OpenRepo(repoPath)
			if err != nil {
				return policy.CodeGraph{}, err
			}
			if !ok {
				return policy.CodeGraph{}, fmt.Errorf("codegraphfacts: no index at %s", repoPath)
			}
		}
		database = existing
	}
	return FromDB(ctx, database, repoPath, opts)
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
