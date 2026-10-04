package evalrun_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	eval "github.com/publicdomainrelay/graph-clm-kcp-spec/impl/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

func loadFixtures(t *testing.T) []eval.Fixture {
	t.Helper()
	fixtures, err := eval.Load(filepath.Join(fixture.Root(t), "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func TestLoadReadsEveryFixtureWithItsScenarios(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 3 {
		t.Fatalf("fixtures = %d, want at least three", len(fixtures))
	}
	seen := map[string]bool{}
	difficulties := map[int]bool{}
	for _, entry := range fixtures {
		seen[entry.Name] = true
		if entry.Drafts == nil {
			t.Errorf("%s has no scripted drafts", entry.Name)
		}
		if len(entry.Config.Verify) == 0 || len(entry.Config.Accept) == 0 {
			t.Errorf("%s names no verify or accept command", entry.Name)
		}
		want := 1
		for _, name := range []string{"calc", "greet", "todo"} {
			if entry.Name == name {
				want = 3
			}
		}
		if len(entry.Scenarios) < want {
			t.Errorf("%s has %d scenarios, want at least %d", entry.Name, len(entry.Scenarios), want)
		}
		for _, scenario := range entry.Scenarios {
			difficulties[scenario.Difficulty] = true
			for _, target := range scenario.Resolved() {
				if len(target.Acceptance) == 0 {
					t.Errorf("%s/%s carries no acceptance test for %s", entry.Name, scenario.Name, target.Context)
				}
				if target.ExpectedDeltaEntries < 0 {
					t.Errorf("%s/%s does not say how many delta entries it intends for %s", entry.Name, scenario.Name, target.Context)
				}
			}
		}
	}
	for _, want := range []string{"calc", "greet", "todo"} {
		if !seen[want] {
			t.Errorf("the fixture %s is missing", want)
		}
	}
	for _, level := range []int{1, 2, 3} {
		if !difficulties[level] {
			t.Errorf("no scenario of difficulty %d", level)
		}
	}
}

func TestSelectScenariosMatchesTheNameAndTheFile(t *testing.T) {
	fixtures := loadFixtures(t)
	byName, err := eval.SelectScenarios(fixtures, "add-subtract")
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) != 1 || len(byName[0].Scenarios) != 1 {
		t.Fatalf("by name = %+v", byName)
	}
	byFile, err := eval.SelectScenarios(fixtures, "01-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(byFile) != 4 {
		t.Fatalf("by file = %d fixtures, want the first scenario of each", len(byFile))
	}
	for _, entry := range byFile {
		if len(entry.Scenarios) != 1 || !strings.HasPrefix(entry.Scenarios[0].File, "01-") {
			t.Errorf("%s kept %+v, want its first scenario", entry.Name, entry.Scenarios)
		}
	}
	if _, err := eval.SelectScenarios(fixtures, "nothing-matches"); err == nil {
		t.Error("a glob that matches nothing was accepted")
	}
}

func TestCopyTreeLeavesTheHiddenFilesBehind(t *testing.T) {
	fixtures := loadFixtures(t)
	target := filepath.Join(t.TempDir(), "calc")
	if err := eval.CopyTree(fixtures[0].Dir, target); err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"scenarios", "fixture.yaml", "summarize.yaml"} {
		if _, err := os.Stat(filepath.Join(target, hidden)); !os.IsNotExist(err) {
			t.Errorf("%s reached the working tree: %v", hidden, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "calc", "calc.go")); err != nil {
		t.Errorf("the source did not reach the working tree: %v", err)
	}
}

func TestScriptedScenariosSatisfyTheirOwnAcceptanceTests(t *testing.T) {
	fixture.Require(t, "git", "go", "deno")
	ctx := context.Background()
	for _, entry := range loadFixtures(t) {
		for _, scenario := range entry.Scenarios {
			t.Run(entry.Name+"/"+scenario.Name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), entry.Name)
				if err := eval.CopyTree(entry.Dir, dir); err != nil {
					t.Fatal(err)
				}
				if scenario.Via == "clm" {
					t.Skip("the scripted baseline does not drive a CLM scenario")
				}
				realize := map[string][]scriptedagent.Step{}
				for _, target := range scenario.Resolved() {
					if len(target.Realize) > 0 {
						realize[target.Context] = target.Realize
					}
				}
				scripted := scriptedagent.New(&scriptedagent.Scenario{Realize: realize})
				for _, target := range scenario.Resolved() {
					if _, err := scripted.Realize(ctx, agent.RealizeRequest{Context: target.Context, Dir: dir}); err != nil {
						t.Fatal(err)
					}
					run(t, dir, entry.Config.Verify)
					for _, file := range target.Acceptance {
						path := filepath.Join(dir, filepath.FromSlash(file.Path))
						if err := os.WriteFile(path, []byte(file.Contents), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					run(t, dir, entry.Config.Accept)
					for _, file := range target.Acceptance {
						if err := os.Remove(filepath.Join(dir, filepath.FromSlash(file.Path))); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func run(t *testing.T, dir string, command []string) {
	t.Helper()
	process := exec.Command(command[0], command[1:]...)
	process.Dir = dir
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("%v in %s: %v\n%s", command, dir, err, output)
	}
}

func TestLoadExpandsTheSourceThroughTheEnvironment(t *testing.T) {
	t.Setenv("SPECD_EVAL_UNKNOWN_REPO", "/tmp/somewhere")
	dir := t.TempDir()
	manifest := "name: unseen\nsource:\n  url: ${SPECD_EVAL_UNKNOWN_REPO:-../nowhere}\nverify: [\"true\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	fixtures, err := eval.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtures[0].Config.Source.URL; got != "/tmp/somewhere" {
		t.Errorf("url = %q, want the environment's value", got)
	}

	t.Setenv("SPECD_EVAL_UNKNOWN_REPO", "")
	if err := os.WriteFile(filepath.Join(dir, "fixture.yaml"),
		[]byte("name: unseen\nsource:\n  url: ${SPECD_EVAL_UNKNOWN_REPO:-/tmp/fallback}\nverify: [\"true\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixtures, err = eval.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtures[0].Config.Source.URL; got != "/tmp/fallback" {
		t.Errorf("url = %q, want the fallback: os.ExpandEnv alone would make it empty", got)
	}
}
