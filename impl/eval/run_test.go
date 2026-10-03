package evalrun

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

func TestKindsNamesTheScriptedFilesForEachHalf(t *testing.T) {
	fixture := Fixture{Name: "calc", Dir: "/fixtures/calc", Scenarios: []Scenario{{Name: "one"}, {Name: "two"}}}
	files := map[string]string{"one": "/work/calc-one.yaml", "two": "/work/calc-two.yaml"}

	summarize, realize := fixture.kinds(Options{}, files)
	if summarize != "scripted:/fixtures/calc/"+fixtureDrafts {
		t.Errorf("summarize kind = %q, want the fixture's own drafts", summarize)
	}
	// One scripted kind is selected at a time, and the harness points the
	// Repository at each scenario's file as it goes.
	if realize != "scripted:/work/calc-one.yaml" {
		t.Errorf("realize kind = %q, want the first scenario's merged file", realize)
	}

	summarize, realize = fixture.kinds(Options{Agent: "claude-mod"}, files)
	if summarize != "claude-mod" || realize != "claude-mod" {
		t.Errorf("kinds = %q, %q, want the named agent for both halves", summarize, realize)
	}
	summarize, _ = fixture.kinds(Options{Agent: "claude-mod", SummarizeAgent: "pi"}, files)
	if summarize != "pi" {
		t.Errorf("summarize kind = %q, want the override", summarize)
	}
}

func TestWriteScenarioFilesMergesTheDraftsWithTheSteps(t *testing.T) {
	dir := t.TempDir()
	fixture := Fixture{
		Name:   "calc",
		Dir:    dir,
		Drafts: &scriptedagent.Scenario{Contexts: map[string]scriptedagent.Draft{"calc": {Intent: "arithmetic"}}},
		Scenarios: []Scenario{{
			Name:    "add-subtract",
			Context: "calc",
			Realize: []scriptedagent.Step{{Write: &scriptedagent.Write{Path: "calc/calc.go", Contents: "package calc\n"}}},
		}},
	}
	files, err := writeScenarioFiles(dir, fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := files["add-subtract"]
	if path == "" {
		t.Fatal("no scenario file was written")
	}
	// The file lives outside the working tree, so the agent under test cannot
	// read the intended edit out of the tree it is editing.
	if _, err := os.Stat(filepath.Join(dir, "calc")); !os.IsNotExist(err) {
		t.Errorf("the merged scenario landed inside the fixture directory: %v", err)
	}
	merged, err := scriptedagent.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Contexts["calc"].Intent != "arithmetic" {
		t.Errorf("drafts = %+v, want the fixture's", merged.Contexts)
	}
	if len(merged.Realize["calc"]) != 1 || merged.Realize["calc"][0].Write == nil {
		t.Errorf("realize = %+v, want the scenario's steps", merged.Realize)
	}
}
