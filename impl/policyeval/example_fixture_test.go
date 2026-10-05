package policyeval_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

func fixtureGraph(t *testing.T, dir, repository string, testGlobs []string) *unstructured.Unstructured {
	t.Helper()
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs %s: %v", dir, err)
	}
	graph, err := codegraphfacts.Build(context.Background(), absolute, codegraphfacts.Options{
		Repository: repository,
		TestGlobs:  testGlobs,
	})
	if err != nil {
		t.Fatalf("build %s: %v", dir, err)
	}
	encoded, err := json.Marshal(policy.CodeGraphObject(graph))
	if err != nil {
		t.Fatal(err)
	}
	object, err := policyeval.Unstructured(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func TestExamplePoliciesOverFixtures(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	library, err := policyeval.Load(filepath.Join(root, "examples", "policies", "market-mini"))
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Templates) != 4 || len(library.Constraints) != 5 {
		t.Fatalf("market-mini library: %d templates, %d constraints", len(library.Templates), len(library.Constraints))
	}

	for _, variant := range []struct {
		name     string
		wantDeny bool
	}{
		{name: "compliant", wantDeny: false},
		{name: "violating", wantDeny: true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			dir := fixture.CopyTree(t, filepath.Join("market-mini", variant.name))
			graph := fixtureGraph(t, dir, "market-mini", library.Manifest.TestGlobs)
			report, err := policyeval.EvaluateLibrary(context.Background(), library,
				[]*unstructured.Unstructured{graph}, []*unstructured.Unstructured{graph})
			if err != nil {
				t.Fatal(err)
			}
			denies := 0
			policies := map[string]bool{}
			constraints := map[string]bool{}
			for _, violation := range report.Violations {
				if violation.Enforcement != policy.EnforcementDeny {
					continue
				}
				denies++
				policies[violation.Policy] = true
				constraints[violation.Constraint] = true
			}
			if !variant.wantDeny {
				if denies != 0 {
					t.Fatalf("compliant fixture has %d deny violations: %+v", denies, report.Violations)
				}
				return
			}
			if denies == 0 {
				t.Fatal("violating fixture has no deny violations")
			}
			if !policies["relay-only-ssh"] {
				t.Errorf("violating fixture does not trigger relay-only-ssh: %v", policies)
			}
			for _, want := range []string{
				"guest-report-reach-in",
				"guest-report-driven-emission",
				"guest-report-driven-onnetwork",
				"guest-report-cloud-init",
			} {
				if !constraints[want] {
					t.Errorf("violating fixture does not trigger %s: %v", want, constraints)
				}
			}
		})
	}
}
