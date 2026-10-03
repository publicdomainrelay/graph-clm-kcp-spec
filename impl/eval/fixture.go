// Package evalrun is the effectiveness harness: it takes a fixture directory,
// turns each fixture into a working tree, drives the whole loop over it with
// the controller the project ships, and measures what happened. The pure
// measures live in abc/eval; this package is the I/O that feeds them.
package evalrun

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

// Config is the fixture manifest: what the fixture is, the command that gates a
// spec -> code change, and the command the hidden acceptance tests run under.
type Config struct {
	Name string `json:"name"`

	// Source names a git working tree the controller clones instead of a
	// fixture copied into a temporary directory. It is how a run measures a
	// codebase this repository does not carry: a fixture with a source has no
	// drafts and no scenarios, because nobody knows its surface in advance.
	Source *GitSource `json:"source"`

	// Populate is how the tree is split, when the default (one context per
	// directory, everything included) is not what the measurement wants.
	Populate *PopulateConfig `json:"populate"`

	Verify []string `json:"verify"`

	Accept []string `json:"accept"`
}

// GitSource is where a fixture that is not in this repository is cloned from.
type GitSource struct {
	URL string `json:"url"`

	Ref string `json:"ref,omitempty"`
}

// PopulateConfig narrows what the populate step indexes.
type PopulateConfig struct {
	Partition string `json:"partition,omitempty"`

	Include []string `json:"include,omitempty"`

	Exclude []string `json:"exclude,omitempty"`
}

// AcceptanceFile is one hidden test file, copied into the tree only to grade a
// scenario and never present while the agent works.
type AcceptanceFile struct {
	Path string `json:"path"`

	Contents string `json:"contents"`
}

// Scenario is one spec edit and the test that decides whether it landed. The
// spec patch is applied to the SystemContext as a merge: the entries it names
// are added or replaced and the rest of the spec is left alone.
type Scenario struct {
	Name string `json:"name"`

	Context string `json:"context"`

	Difficulty int `json:"difficulty"`

	Description string `json:"description"`

	SpecPatch map[string]any `json:"specPatch"`

	ExpectedInterfaces []string `json:"expectedInterfaces"`

	ExpectedDeltaEntries int `json:"expectedDeltaEntries"`

	Acceptance []AcceptanceFile `json:"acceptance"`

	Realize []scriptedagent.Step `json:"realize"`

	// File is the scenario's file name without its extension. A glob may match
	// it, so `--scenarios 01-*` names the easiest scenario of every fixture
	// without knowing the names each fixture gave them.
	File string `json:"-"`
}

// Fixture is one repository under fixtures/ with its scenarios.
type Fixture struct {
	Name string

	Dir string

	Config Config

	Drafts *scriptedagent.Scenario

	Scenarios []Scenario
}

const (
	fixtureManifest = "fixture.yaml"

	fixtureDrafts = "summarize.yaml"

	scenarioDir = "scenarios"
)

// Load reads every fixture under dir: a directory with a fixture.yaml, the
// scripted code -> spec answer in summarize.yaml, and one scenario per YAML
// file in scenarios/.
func Load(dir string) ([]Fixture, error) {
	// A single fixture may be named directly, which is how a run is narrowed to
	// one repository without narrowing its scenarios.
	if _, err := os.Stat(filepath.Join(dir, fixtureManifest)); err == nil {
		fixture, err := loadFixture(dir)
		if err != nil {
			return nil, err
		}
		return []Fixture{fixture}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: read %s: %w", dir, err)
	}
	fixtures := []Fixture{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root := filepath.Join(dir, entry.Name())
		manifest := filepath.Join(root, fixtureManifest)
		if _, err := os.Stat(manifest); err != nil {
			continue
		}
		fixture, err := loadFixture(root)
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, fixture)
	}
	sort.Slice(fixtures, func(left, right int) bool { return fixtures[left].Name < fixtures[right].Name })
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("eval: %s holds no fixture with a %s", dir, fixtureManifest)
	}
	return fixtures, nil
}

func loadFixture(root string) (Fixture, error) {
	fixture := Fixture{Name: filepath.Base(root), Dir: root}
	manifest, err := os.ReadFile(filepath.Join(root, fixtureManifest))
	if err != nil {
		return fixture, fmt.Errorf("eval: read the manifest of %s: %w", fixture.Name, err)
	}
	if err := yaml.Unmarshal(manifest, &fixture.Config); err != nil {
		return fixture, fmt.Errorf("eval: parse the manifest of %s: %w", fixture.Name, err)
	}
	if fixture.Config.Name == "" {
		fixture.Config.Name = fixture.Name
	}
	if len(fixture.Config.Verify) == 0 {
		return fixture, fmt.Errorf("eval: %s names no verify command", fixture.Name)
	}
	if len(fixture.Config.Accept) == 0 {
		fixture.Config.Accept = fixture.Config.Verify
	}
	if fixture.Config.Source != nil {
		// A codebase this repository does not carry has no scripted answer and
		// no scenario: the whole measurement is what one manifest populates.
		if fixture.Config.Source.URL == "" {
			return fixture, fmt.Errorf("eval: %s names a source with no url", fixture.Name)
		}
		// The url may name the checkout through the environment and may be a
		// relative path, because where an unknown codebase sits is a fact about
		// the machine and not about this repository.
		url := os.ExpandEnv(fixture.Config.Source.URL)
		if !strings.Contains(url, "://") && !filepath.IsAbs(url) {
			absolute, err := filepath.Abs(url)
			if err != nil {
				return fixture, fmt.Errorf("eval: resolve the source of %s: %w", fixture.Name, err)
			}
			url = absolute
		}
		fixture.Config.Source.URL = url
		return fixture, nil
	}
	drafts, err := scriptedagent.Load(filepath.Join(root, fixtureDrafts))
	if err != nil {
		return fixture, fmt.Errorf("eval: load the drafts of %s: %w", fixture.Name, err)
	}
	fixture.Drafts = drafts
	scenarios, err := loadScenarios(filepath.Join(root, scenarioDir))
	if err != nil {
		return fixture, err
	}
	if len(scenarios) == 0 {
		return fixture, fmt.Errorf("eval: %s has no scenario under %s", fixture.Name, scenarioDir)
	}
	fixture.Scenarios = scenarios
	return fixture, nil
}

func loadScenarios(dir string) ([]Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("eval: read %s: %w", dir, err)
	}
	scenarios := []Scenario{}
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("eval: read %s: %w", path, err)
		}
		scenario := Scenario{}
		if err := yaml.Unmarshal(contents, &scenario); err != nil {
			return nil, fmt.Errorf("eval: parse %s: %w", path, err)
		}
		scenario.File = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if scenario.Name == "" {
			scenario.Name = scenario.File
		}
		if scenario.Context == "" {
			return nil, fmt.Errorf("eval: %s names no context", path)
		}
		if len(scenario.SpecPatch) == 0 {
			return nil, fmt.Errorf("eval: %s carries no specPatch", path)
		}
		if len(scenario.Acceptance) == 0 {
			return nil, fmt.Errorf("eval: %s carries no acceptance test", path)
		}
		scenarios = append(scenarios, scenario)
	}
	sort.Slice(scenarios, func(left, right int) bool { return scenarios[left].Name < scenarios[right].Name })
	return scenarios, nil
}

// SelectScenarios filters the scenarios of every fixture by a glob matched
// against the scenario name and against its file name. An empty pattern keeps
// them all.
func SelectScenarios(fixtures []Fixture, pattern string) ([]Fixture, error) {
	if pattern == "" {
		return fixtures, nil
	}
	selected := make([]Fixture, 0, len(fixtures))
	for _, fixture := range fixtures {
		kept := []Scenario{}
		for _, scenario := range fixture.Scenarios {
			matched, err := filepath.Match(pattern, scenario.Name)
			if err != nil {
				return nil, fmt.Errorf("eval: %q is not a glob: %w", pattern, err)
			}
			byFile, err := filepath.Match(pattern, scenario.File)
			if err != nil {
				return nil, fmt.Errorf("eval: %q is not a glob: %w", pattern, err)
			}
			if matched || byFile {
				kept = append(kept, scenario)
			}
		}
		if len(kept) == 0 {
			continue
		}
		fixture.Scenarios = kept
		selected = append(selected, fixture)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("eval: no scenario matches %q", pattern)
	}
	return selected, nil
}

func isYAML(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".yaml" || extension == ".yml"
}

// CopyTree copies a fixture into a fresh working tree. The scenario files, the
// fixture manifest and the scripted drafts are left behind on purpose: they
// carry the hidden acceptance tests and the intended edit, and an agent that
// could read them would be graded on a test it was shown.
func CopyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, 0o755)
		}
		switch entry.Name() {
		case ".git", ".codegraph":
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		switch relative {
		case scenarioDir, fixtureManifest, fixtureDrafts:
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		return copyFile(path, destination)
	})
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	return os.Chmod(target, info.Mode())
}
