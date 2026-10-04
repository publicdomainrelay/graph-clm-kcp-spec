package agentfactory

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/claudecli"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/piagent"
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

func TestARepositoryKindWinsOverTheController(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.yaml")
	if err := os.WriteFile(path, []byte("contexts:\n  calc:\n    intent: from the scenario\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The controller names no agent at all; the Repository does.
	factory, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       spec.RepositorySpec{Agent: &spec.AgentSpec{Kind: Scripted + ":" + path}},
	}
	if !factory.ConfiguredFor(repository) {
		t.Fatal("a repository that names a kind must be workable")
	}
	if factory.ConfiguredFor(nil) {
		t.Error("a factory with nothing configured must not claim a repository it was not given")
	}
	built, err := factory.Agent(repository, "/tmp/calc")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := built.(*scriptedagent.Agent); !ok {
		t.Fatalf("built a %T", built)
	}

	// A repository that names the model kind still gets the controller's
	// command when it does not name one itself.
	claudeRepo := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       spec.RepositorySpec{Agent: &spec.AgentSpec{Kind: Claude}},
	}
	built, err = factory.Agent(claudeRepo, "/tmp/calc")
	if err != nil {
		t.Fatal(err)
	}
	if claude, ok := built.(*claudecli.Agent); !ok || claude.Command() != claudecli.DefaultCommand {
		t.Fatalf("built a %T", built)
	}
	unknown := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       spec.RepositorySpec{Agent: &spec.AgentSpec{Kind: "magic"}},
	}
	if _, err := factory.Agent(unknown, "/tmp/calc"); err == nil {
		t.Error("a repository that names an unknown kind was accepted")
	}
	built, err = factory.Agent(&spec.Repository{Spec: spec.RepositorySpec{
		Agent: &spec.AgentSpec{Kind: Pi},
	}}, "/tmp/calc")
	if err != nil {
		t.Fatal(err)
	}
	if pi, ok := built.(*claudecli.Agent); !ok || pi.Command() != piagent.DefaultCommand {
		t.Fatalf("built a %T", built)
	}
	if _, err := factory.Agent(&spec.Repository{Spec: spec.RepositorySpec{
		Agent: &spec.AgentSpec{Kind: ClaudeMod},
	}}, "/tmp/calc"); err == nil {
		t.Error("the mod kind with no --clm-mod folder must be refused")
	}
}

// pi discovers an extension from a settings file, so a controller that was
// given a folder has to hand it to the command too; otherwise the CLM path
// asks the model to edit a document nothing applies.
func TestPiKindLoadsTheExtensionFolder(t *testing.T) {
	got := piArgs([]string{"--yes", piagent.Package, "-p"}, "/tmp/pi-hydradb-clm")
	want := []string{"--yes", piagent.Package, "-p", "--extension", "/tmp/pi-hydradb-clm"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	named := []string{"--yes", piagent.Package, "-p", "--extension", "/other"}
	if got := piArgs(named, "/tmp/pi-hydradb-clm"); !slices.Equal(got, named) {
		t.Errorf("a caller that named an extension was overridden: %v", got)
	}
	if got := piArgs([]string{"-p"}, ""); !slices.Equal(got, []string{"-p"}) {
		t.Errorf("no folder must add nothing: %v", got)
	}
}

// TestPiKindNamesTheExtensionAbsolutely is the guard for a path the caller
// typed where it stood: the model runs in the working tree, so a folder left
// relative is looked for inside the tree and the run fails with "extension path
// does not exist" instead of naming the caller's mistake.
func TestPiKindNamesTheExtensionAbsolutely(t *testing.T) {
	factory, err := New(Options{Kind: Pi, PiExtension: "pi-hydradb-clm"})
	if err != nil {
		t.Fatal(err)
	}
	built, err := factory.Agent(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	claude, ok := built.(*claudecli.Agent)
	if !ok {
		t.Fatalf("built a %T", built)
	}
	args := claude.Args()
	index := slices.Index(args, "--extension")
	if index < 0 || index+1 >= len(args) {
		t.Fatalf("the extension is not in %v", args)
	}
	if !filepath.IsAbs(args[index+1]) {
		t.Errorf("--extension %q is not absolute", args[index+1])
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
