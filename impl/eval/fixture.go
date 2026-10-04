package evalrun

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/eval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/scriptedagent"
)

type Config struct {
	Name string `json:"name"`

	Source *GitSource `json:"source"`

	Populate *PopulateConfig `json:"populate"`

	Verify []string `json:"verify"`

	Accept []string `json:"accept"`
}

type GitSource struct {
	URL string `json:"url"`

	Ref string `json:"ref,omitempty"`
}

type PopulateConfig struct {
	Partition string `json:"partition,omitempty"`

	Include []string `json:"include,omitempty"`

	Exclude []string `json:"exclude,omitempty"`
}

type AcceptanceFile struct {
	Path string `json:"path"`

	Contents string `json:"contents"`
}

type Scenario struct {
	Name string `json:"name"`

	Context string `json:"context"`

	Difficulty int `json:"difficulty"`

	Description string `json:"description"`

	Via string `json:"via,omitempty"`

	Request string `json:"request,omitempty"`

	SpecPatch map[string]any `json:"specPatch"`

	ExpectedInterfaces []string `json:"expectedInterfaces"`

	ExpectedRemovedInterfaces []string `json:"expectedRemovedInterfaces"`

	ExpectedDeltaEntries int `json:"expectedDeltaEntries"`

	Acceptance []AcceptanceFile `json:"acceptance"`

	Realize []scriptedagent.Step `json:"realize"`

	Targets []Target `json:"targets,omitempty"`

	File string `json:"-"`
}

type Target struct {
	Context string `json:"context"`

	SpecPatch map[string]any `json:"specPatch"`

	ExpectedInterfaces []string `json:"expectedInterfaces"`

	ExpectedRemovedInterfaces []string `json:"expectedRemovedInterfaces"`

	ExpectedDeltaEntries int `json:"expectedDeltaEntries"`

	Acceptance []AcceptanceFile `json:"acceptance"`

	Realize []scriptedagent.Step `json:"realize"`
}

func (s Scenario) Resolved() []Target {
	if len(s.Targets) > 0 {
		return s.Targets
	}
	return []Target{{
		Context:                   s.Context,
		SpecPatch:                 s.SpecPatch,
		ExpectedInterfaces:        s.ExpectedInterfaces,
		ExpectedRemovedInterfaces: s.ExpectedRemovedInterfaces,
		ExpectedDeltaEntries:      s.ExpectedDeltaEntries,
		Acceptance:                s.Acceptance,
		Realize:                   s.Realize,
	}}
}

type Expectations struct {
	Contexts map[string]ContextExpectation `json:"contexts"`
}

type ContextExpectation struct {
	Facts []eval.Fact `json:"facts"`
}

type DriftScenario struct {
	Name string `json:"name"`

	Context string `json:"context"`

	Difficulty int `json:"difficulty"`

	Description string `json:"description"`

	Commit []scriptedagent.Step `json:"commit"`

	ExpectedInterfacesAdded []string `json:"expectedInterfacesAdded"`

	ExpectedInterfacesRemoved []string `json:"expectedInterfacesRemoved"`

	ExpectedDeltaEntries int `json:"expectedDeltaEntries"`

	Prose []eval.Fact `json:"prose"`

	Draft scriptedagent.Draft `json:"draft"`

	File string `json:"-"`
}

type Fixture struct {
	Name string

	Dir string

	Config Config

	Drafts *scriptedagent.Scenario

	Expectations Expectations

	Scenarios []Scenario

	Drift []DriftScenario
}

const (
	fixtureManifest = "fixture.yaml"

	fixtureDrafts = "summarize.yaml"

	fixtureExpectations = "expected.yaml"

	scenarioDir = "scenarios"

	driftDir = "drift"
)

func Load(dir string) ([]Fixture, error) {
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
		if fixture.Config.Source.URL == "" {
			return fixture, fmt.Errorf("eval: %s names a source with no url", fixture.Name)
		}
		url := expandDefault(fixture.Config.Source.URL)
		if url == "" {
			return fixture, fmt.Errorf("eval: the source of %s expands to nothing", fixture.Name)
		}
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
	expectations, err := loadExpectations(filepath.Join(root, fixtureExpectations))
	if err != nil {
		return fixture, fmt.Errorf("eval: load the expectations of %s: %w", fixture.Name, err)
	}
	fixture.Expectations = expectations
	scenarios, err := loadScenarios(filepath.Join(root, scenarioDir))
	if err != nil {
		return fixture, err
	}
	if len(scenarios) == 0 {
		return fixture, fmt.Errorf("eval: %s has no scenario under %s", fixture.Name, scenarioDir)
	}
	fixture.Scenarios = scenarios
	drift, err := loadDrift(filepath.Join(root, driftDir))
	if err != nil {
		return fixture, err
	}
	fixture.Drift = drift
	return fixture, nil
}

func loadExpectations(path string) (Expectations, error) {
	expectations := Expectations{}
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return expectations, nil
		}
		return expectations, err
	}
	if err := yaml.Unmarshal(contents, &expectations); err != nil {
		return expectations, err
	}
	return expectations, nil
}

func loadDrift(dir string) ([]DriftScenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("eval: read %s: %w", dir, err)
	}
	scenarios := []DriftScenario{}
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("eval: read %s: %w", path, err)
		}
		scenario := DriftScenario{}
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
		if len(scenario.Commit) == 0 {
			return nil, fmt.Errorf("eval: %s commits no code change", path)
		}
		scenarios = append(scenarios, scenario)
	}
	sort.Slice(scenarios, func(left, right int) bool { return scenarios[left].Name < scenarios[right].Name })
	return scenarios, nil
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
		switch scenario.Via {
		case "":
			for index := range scenario.Targets {
				if scenario.Targets[index].Context == "" {
					scenario.Targets[index].Context = scenario.Context
				}
			}
			for _, target := range scenario.Resolved() {
				if len(target.SpecPatch) == 0 {
					return nil, fmt.Errorf("eval: %s carries no specPatch for %s", path, target.Context)
				}
				if len(target.Acceptance) == 0 {
					return nil, fmt.Errorf("eval: %s carries no acceptance test for %s", path, target.Context)
				}
			}
		case "clm":
			if strings.TrimSpace(scenario.Request) == "" {
				return nil, fmt.Errorf("eval: %s is a CLM scenario with no request", path)
			}
			if len(scenario.Targets) > 1 {
				return nil, fmt.Errorf("eval: %s is a CLM scenario with more than one target", path)
			}
		default:
			return nil, fmt.Errorf("eval: %s names an unknown via %q", path, scenario.Via)
		}
		scenarios = append(scenarios, scenario)
	}
	sort.Slice(scenarios, func(left, right int) bool { return scenarios[left].Name < scenarios[right].Name })
	return scenarios, nil
}

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

var defaultPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}]*)\}`)

func expandDefault(value string) string {
	expanded := defaultPattern.ReplaceAllStringFunc(value, func(match string) string {
		groups := defaultPattern.FindStringSubmatch(match)
		if fromEnv := os.Getenv(groups[1]); fromEnv != "" {
			return fromEnv
		}
		return groups[2]
	})
	return os.ExpandEnv(expanded)
}

func isYAML(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".yaml" || extension == ".yml"
}

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
		case scenarioDir, driftDir, fixtureManifest, fixtureDrafts, fixtureExpectations:
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
