package policyeval_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

const root = "../.."

var (
	caseRunPattern   = regexp.MustCompile(`^\s+--- (PASS|FAIL): (\S+)`)
	testRunPattern   = regexp.MustCompile(`^=== RUN\s+(\S+)`)
	gatorBinary      = filepath.Join(root, "bin", "gator")
	gatorBinOverride = os.Getenv("SPECD_GATOR")
)

func suites(t *testing.T) []string {
	t.Helper()
	found := []string{}
	for _, pattern := range []string{
		filepath.Join(root, "examples", "policies", "*", policy.TestsDir, "*", policy.SuiteName),
		filepath.Join(root, "policies", "*", policy.TestsDir, "*", policy.SuiteName),
		filepath.Join(root, "policies", "packs", "*", policy.TestsDir, "*", policy.SuiteName),
		filepath.Join(root, "testdata", "*", policy.TestsDir, "*", policy.SuiteName),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, matches...)
	}
	sort.Strings(found)
	return found
}

func TestExampleSuitesAgreeWithGator(t *testing.T) {
	found := suites(t)
	if len(found) == 0 {
		t.Skip("no gator suites in examples/ or testdata/")
	}
	bin := gatorBinaryPath()
	requireGator := os.Getenv("SPECD_REQUIRE_GATOR") == "1"
	if bin == "" {
		if requireGator {
			t.Fatalf("SPECD_REQUIRE_GATOR=1 but no gator binary was found; run scripts/install-policy-tools.sh")
		}
		t.Skip("no gator binary; run scripts/install-policy-tools.sh for the conformance half")
	}

	for _, suite := range found {
		relative, err := filepath.Rel(root, suite)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := policyeval.RunSuite(context.Background(), os.DirFS(root), filepath.ToSlash(relative))
		if err != nil {
			t.Fatalf("%s: %v", relative, err)
		}
		theirs := gatorCases(t, bin, relative)
		if len(theirs) == 0 {
			t.Fatalf("%s: gator reported no cases", relative)
		}
		compareSuites(t, relative, ours, theirs)
	}
}

func compareSuites(t *testing.T, name string, ours policyeval.SuiteResult, theirs map[string]bool) {
	t.Helper()
	seen := map[string]bool{}
	for _, test := range ours.Tests {
		for _, item := range test.Cases {
			key := test.Name + "/" + item.Name
			seen[key] = true
			theirsPassed, ok := theirs[key]
			if !ok {
				t.Errorf("%s: gator did not run %s", name, key)
				continue
			}
			oursPassed := item.Error == ""
			if oursPassed != theirsPassed {
				t.Errorf("%s: %s: our engine says pass=%v, gator says pass=%v (our error: %s)",
					name, key, oursPassed, theirsPassed, item.Error)
			}
		}
		if test.Error != "" {
			t.Errorf("%s: %s: %s", name, test.Name, test.Error)
		}
	}
	for key := range theirs {
		if !seen[key] {
			t.Errorf("%s: gator ran %s and we did not", name, key)
		}
	}
}

func gatorCases(t *testing.T, bin, suite string) map[string]bool {
	t.Helper()
	command := exec.Command(bin, "verify", suite, "-v")
	command.Dir = root
	out, _ := command.CombinedOutput()

	cases := map[string]bool{}
	test := ""
	for _, line := range strings.Split(string(out), "\n") {
		if match := caseRunPattern.FindStringSubmatch(line); match != nil {
			cases[test+"/"+match[2]] = match[1] == "PASS"
			continue
		}
		if match := testRunPattern.FindStringSubmatch(line); match != nil {
			test = match[1]
		}
	}
	return cases
}

func gatorBinaryPath() string {
	if gatorBinOverride != "" {
		if absolute, err := filepath.Abs(gatorBinOverride); err == nil {
			if _, statErr := os.Stat(absolute); statErr == nil {
				return absolute
			}
		}
		return ""
	}
	if absolute, err := filepath.Abs(gatorBinary); err == nil {
		if _, statErr := os.Stat(absolute); statErr == nil {
			return absolute
		}
	}
	if found, err := exec.LookPath("gator"); err == nil {
		return found
	}
	return ""
}

func TestConformancePackIsCurrentAndGreen(t *testing.T) {
	dir := filepath.Join(root, "policies", "packs", "conformance")
	library, err := policyeval.Load(dir)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	if len(library.Templates) != 3 {
		t.Fatalf("%s: templates = %d, want 3", dir, len(library.Templates))
	}
	dist, err := policyeval.Dist(library)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range dist {
		onDisk, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("%s: %s is missing; run specctl policy build --dir %s", dir, path, dir)
			continue
		}
		if string(onDisk) != string(want) {
			t.Errorf("%s: %s is stale; run specctl policy build --dir %s", dir, path, dir)
		}
	}
	catalogue, err := os.ReadFile(filepath.Join(dir, policy.CataloguePath))
	if err != nil {
		t.Errorf("%s: %s is missing", dir, policy.CataloguePath)
	} else if string(catalogue) != string(policyeval.Catalogue(library)) {
		t.Errorf("%s: %s is stale", dir, policy.CataloguePath)
	}
	lib, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(policy.LibPath)))
	if err != nil {
		t.Errorf("%s: %s is missing", dir, policy.LibPath)
	} else if string(lib) != policyeval.Lib() {
		t.Errorf("%s: %s is stale; the pack must carry the shared library", dir, policy.LibPath)
	}

	suites, err := filepath.Glob(filepath.Join(dir, policy.TestsDir, "*", policy.SuiteName))
	if err != nil {
		t.Fatal(err)
	}
	if len(suites) == 0 {
		t.Fatalf("%s: no suites", dir)
	}
	for _, suite := range suites {
		relative, err := filepath.Rel(root, suite)
		if err != nil {
			t.Fatal(err)
		}
		result, err := policyeval.RunSuite(context.Background(), os.DirFS(root), filepath.ToSlash(relative))
		if err != nil {
			t.Fatalf("%s: %v", relative, err)
		}
		if !result.Passed() {
			t.Errorf("%s did not pass: %+v", relative, result)
		}
	}
}

func TestExampleDistAndCatalogueAreCurrent(t *testing.T) {
	libraries := []string{}
	for _, pattern := range []string{
		filepath.Join(root, "examples", "policies", "*"),
		filepath.Join(root, "policies", "*"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		libraries = append(libraries, matches...)
	}
	sort.Strings(libraries)
	checked := 0
	for _, dir := range libraries {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, policy.PoliciesPath)); err != nil {
			continue
		}
		library, err := policyeval.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		dist, err := policyeval.Dist(library)
		if err != nil {
			t.Fatal(err)
		}
		for path, want := range dist {
			onDisk, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
			if err != nil {
				t.Errorf("%s: %s is missing; run specctl policy build --dir %s", dir, path, dir)
				continue
			}
			if string(onDisk) != string(want) {
				t.Errorf("%s: %s is stale; run specctl policy build --dir %s", dir, path, dir)
			}
		}
		catalogue, err := os.ReadFile(filepath.Join(dir, policy.CataloguePath))
		if err != nil {
			t.Errorf("%s: %s is missing; run specctl policy build --dir %s", dir, policy.CataloguePath, dir)
		} else if string(catalogue) != string(policyeval.Catalogue(library)) {
			t.Errorf("%s: %s is stale; run specctl policy build --dir %s", dir, policy.CataloguePath, dir)
		}
		lib, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(policy.LibPath)))
		if err != nil {
			t.Errorf("%s: %s is missing; run specctl policy build --dir %s", dir, policy.LibPath, dir)
		} else if string(lib) != policyeval.Lib() {
			t.Errorf("%s: %s is stale; run specctl policy build --dir %s", dir, policy.LibPath, dir)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no example policy libraries")
	}
}
