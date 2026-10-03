package evalrun_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
		if len(entry.Scenarios) < 3 {
			t.Errorf("%s has %d scenarios, want at least three", entry.Name, len(entry.Scenarios))
		}
		for _, scenario := range entry.Scenarios {
			difficulties[scenario.Difficulty] = true
			if len(scenario.Acceptance) == 0 {
				t.Errorf("%s/%s carries no acceptance test", entry.Name, scenario.Name)
			}
			if scenario.ExpectedDeltaEntries <= 0 {
				t.Errorf("%s/%s does not say how many delta entries it intends", entry.Name, scenario.Name)
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
	if len(byFile) != 3 {
		t.Fatalf("by file = %d fixtures, want the easiest scenario of each", len(byFile))
	}
	for _, entry := range byFile {
		if len(entry.Scenarios) != 1 || entry.Scenarios[0].Difficulty != 1 {
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

// TestScriptedScenariosSatisfyTheirOwnAcceptanceTests is the harness's own
// proof, with no cluster in it: every scenario's realize steps are applied to a
// copy of its fixture, the fixture's verify command must pass, and the hidden
// acceptance tests must pass on top. A scenario that cannot satisfy its own
// tests would make the eval report meaningless.
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
				scripted := scriptedagent.New(&scriptedagent.Scenario{
					Realize: map[string][]scriptedagent.Step{scenario.Context: scenario.Realize},
				})
				if _, err := scripted.Realize(ctx, agent.RealizeRequest{Context: scenario.Context, Dir: dir}); err != nil {
					t.Fatal(err)
				}
				run(t, dir, entry.Config.Verify)
				for _, file := range scenario.Acceptance {
					target := filepath.Join(dir, filepath.FromSlash(file.Path))
					if err := os.WriteFile(target, []byte(file.Contents), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				run(t, dir, entry.Config.Accept)
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
