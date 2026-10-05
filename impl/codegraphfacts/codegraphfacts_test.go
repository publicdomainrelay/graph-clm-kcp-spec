package codegraphfacts_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

func requireCodegraph(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
}

func TestBuildIsDeterministicAndAssignsContexts(t *testing.T) {
	requireCodegraph(t)
	repo := fixture.CopyTree(t, "calc")
	contexts := []spec.SystemContext{
		{ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: "default"}, Status: spec.SystemContextStatus{Observed: spec.ObservedFacts{Files: []string{"calc/calc.go"}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc", Namespace: "default"}, Status: spec.SystemContextStatus{Observed: spec.ObservedFacts{Files: []string{"cmd/calc/main.go"}}}},
	}
	options := codegraphfacts.Options{
		Repository: "calc",
		Branch:     "main",
		Commit:     "deadbeef",
		TestGlobs:  []string{"**/*_test.go"},
		Contexts:   codegraphfacts.ContextsByFile(contexts),
	}
	first, err := codegraphfacts.Build(context.Background(), repo, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := codegraphfacts.Build(context.Background(), repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two builds of the same tree differ")
	}
	if first.APIVersion != policy.APIVersion || first.Kind != policy.CodeGraphKind {
		t.Fatalf("unexpected identity: %s %s", first.APIVersion, first.Kind)
	}
	if first.Metadata.Labels[policy.CommitLabel] != "deadbeef" {
		t.Fatalf("commit label: %v", first.Metadata.Labels)
	}
	if len(first.Spec.Files) == 0 || len(first.Spec.Nodes) == 0 {
		t.Fatalf("empty graph: %d files, %d nodes", len(first.Spec.Files), len(first.Spec.Nodes))
	}
	contextsSeen := map[string]bool{}
	testsSeen := false
	for _, file := range first.Spec.Files {
		if file.Context != "" {
			contextsSeen[file.Context] = true
		}
		if file.Test {
			testsSeen = true
		}
		if file.Path == "calc/calc.go" && file.SHA256 == "" {
			t.Fatal("file digest missing")
		}
	}
	if !contextsSeen["calc"] || !contextsSeen["cmd-calc"] {
		t.Fatalf("context assignment: %v", contextsSeen)
	}
	if !testsSeen {
		t.Fatal("no file matched the test globs")
	}
	if _, ok := first.Spec.Texts["calc/calc.go"]; !ok {
		t.Fatal("file text missing")
	}
	found := false
	for _, node := range first.Spec.Nodes {
		if node.File == "calc/calc.go" && node.Text != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("no node carried its source text")
	}
}

func gitCommit(t *testing.T, repo string, message string) {
	t.Helper()
	run := func(args ...string) {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", "-A")
	run("commit", "-qm", message)
}

func nodeNames(graph policy.CodeGraph) map[string]bool {
	out := map[string]bool{}
	for _, node := range graph.Spec.Nodes {
		out[node.Name] = true
	}
	return out
}

func TestBuildIgnoresAStaleIndexInACheckout(t *testing.T) {
	requireCodegraph(t)
	repo := t.TempDir()
	source := filepath.Join(repo, "a.go")
	if err := os.WriteFile(source, []byte("package a\n\nfunc One() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, repo, "one")
	if _, err := codegraphfacts.Build(context.Background(), repo, codegraphfacts.Options{Repository: "a", IndexInPlace: true}); err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(repo, ".codegraph")
	if _, err := os.Stat(indexDir); err != nil {
		t.Fatalf("--index-in-place did not write the index: %v", err)
	}
	if err := os.WriteFile(source, []byte("package a\n\nfunc Two() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	graph, err := codegraphfacts.Build(context.Background(), repo, codegraphfacts.Options{Repository: "a"})
	if err != nil {
		t.Fatal(err)
	}
	names := nodeNames(graph)
	if !names["Two"] || names["One"] {
		t.Fatalf("the stale index was read: %v", names)
	}
	if _, err := os.Stat(filepath.Join(indexDir, "specd-commit")); err != nil {
		t.Fatalf("the default build removed the reader's index: %v", err)
	}
}

func TestBuildDoesNotWriteAnIndexIntoACleanCheckout(t *testing.T) {
	requireCodegraph(t)
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc One() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, repo, "one")
	graph, err := codegraphfacts.Build(context.Background(), repo, codegraphfacts.Options{Repository: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !nodeNames(graph)["One"] {
		t.Fatalf("the copy was not indexed: %v", nodeNames(graph))
	}
	if _, err := os.Stat(filepath.Join(repo, ".codegraph")); !os.IsNotExist(err) {
		t.Fatalf("the default build wrote an index into the checkout: %v", err)
	}
}

func TestCapsTruncateText(t *testing.T) {
	repo := t.TempDir()
	big := make([]byte, policy.FileTextCap+128)
	for index := range big {
		big[index] = 'a'
	}
	big[len(big)-1] = '\n'
	if err := os.WriteFile(filepath.Join(repo, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module big\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	graph, err := codegraphfacts.Build(context.Background(), repo, codegraphfacts.Options{Repository: "big"})
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := graph.Spec.Texts["big.txt"]; ok && len(text) > policy.FileTextCap {
		t.Fatalf("file text not capped: %d", len(text))
	}
}
