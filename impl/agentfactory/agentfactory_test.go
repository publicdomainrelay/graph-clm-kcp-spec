package agentfactory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

func TestNoKindIsNoAgent(t *testing.T) {
	factory, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if factory.Configured() {
		t.Error("an empty kind is configured")
	}
	if _, err := factory.Agent(nil, "."); err == nil {
		t.Error("an unconfigured factory built an agent")
	}
}

func TestClaudeKindDefaultsToTheHeadlessCommand(t *testing.T) {
	factory, err := New(Options{Kind: Claude})
	if err != nil {
		t.Fatal(err)
	}
	built, err := factory.Agent(nil, "/tmp/calc")
	if err != nil {
		t.Fatal(err)
	}
	claude, ok := built.(*claudecli.Agent)
	if !ok {
		t.Fatalf("built a %T", built)
	}
	if claude.Command() != claudecli.DefaultCommand {
		t.Errorf("command = %q, want %q", claude.Command(), claudecli.DefaultCommand)
	}
}

func TestARepositoryCommandOverridesTheDefault(t *testing.T) {
	factory, err := New(Options{Kind: Claude, Command: "the-default"})
	if err != nil {
		t.Fatal(err)
	}
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       spec.RepositorySpec{Agent: &spec.AgentSpec{Command: "per-repo", Args: []string{"-p"}}},
	}
	built, err := factory.Agent(repository, "/tmp/calc")
	if err != nil {
		t.Fatal(err)
	}
	if claude := built.(*claudecli.Agent); claude.Command() != "per-repo" {
		t.Errorf("command = %q, want the repository's", claude.Command())
	}
}

func TestScriptedKindReadsTheScenarioOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(path, []byte("contexts:\n  calc:\n    intent: from the scenario\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	factory, err := New(Options{Kind: Scripted + ":" + path})
	if err != nil {
		t.Fatal(err)
	}
	if !factory.Configured() || factory.Kind() != Scripted {
		t.Fatalf("factory = %+v", factory)
	}
	first, err := factory.Agent(nil, ".")
	if err != nil {
		t.Fatal(err)
	}
	second, err := factory.Agent(nil, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := first.(*scriptedagent.Agent); !ok {
		t.Fatalf("built a %T", first)
	}
	if first == second {
		t.Error("the two agents share one value; a scenario must be read once and handed out")
	}
}

func TestBadKindsAreRefused(t *testing.T) {
	if _, err := New(Options{Kind: "magic"}); err == nil {
		t.Error("an unknown kind was accepted")
	}
	if _, err := New(Options{Kind: Scripted + ":"}); err == nil {
		t.Error("a scripted kind with no file was accepted")
	}
	if _, err := New(Options{Kind: Scripted + ":/does/not/exist.yaml"}); err == nil {
		t.Error("a missing scenario was accepted")
	} else if !strings.Contains(err.Error(), "/does/not/exist.yaml") {
		t.Errorf("error does not name the file: %v", err)
	}
}
