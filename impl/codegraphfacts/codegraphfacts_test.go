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
)

func copyFixture(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join("..", "..", "fixtures", name)
	target := filepath.Join(t.TempDir(), name)
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return target
}

func requireCodegraph(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
}

func TestBuildIsDeterministicAndAssignsContexts(t *testing.T) {
	requireCodegraph(t)
	repo := copyFixture(t, "calc")
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
