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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

// fixtureObjects indexes a fixture, classifies its effects and builds the
// ArchitectureModel, so the fixture exercises the portable pack as well as the
// concrete library.
func fixtureObjects(t *testing.T, dir, repository string, library policy.Library) (*unstructured.Unstructured, *unstructured.Unstructured) {
	t.Helper()
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs %s: %v", dir, err)
	}
	graph, err := codegraphfacts.Build(context.Background(), absolute, codegraphfacts.Options{
		Repository: repository,
		TestGlobs:  library.Manifest.TestGlobs,
	})
	if err != nil {
		t.Fatalf("build %s: %v", dir, err)
	}
	computed, err := effects.Apply(&graph, effects.Options{IncludeExtras: true})
	if err != nil {
		t.Fatalf("effects %s: %v", dir, err)
	}
	model, err := policy.BuildModel(policy.ModelInput{
		Repository: repository,
		Graph:      graph,
		Effects:    computed,
		Binding:    library.Manifest.Binding(),
	})
	if err != nil {
		t.Fatalf("model %s: %v", dir, err)
	}
	graphObject, err := objectOf(t, policy.CodeGraphObject(graph))
	if err != nil {
		t.Fatal(err)
	}
	modelObject, err := objectOf(t, model)
	if err != nil {
		t.Fatal(err)
	}
	return graphObject, modelObject
}

func objectOf(t *testing.T, value any) (*unstructured.Unstructured, error) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return policyeval.Unstructured(encoded)
}

func TestExamplePoliciesOverFixtures(t *testing.T) {
	if _, err := exec.LookPath("codegraph"); err != nil {
		t.Skip("codegraph is not on PATH")
	}
	library, err := policyeval.Load(filepath.Join(root, "examples", "policies", "market-mini"))
	if err != nil {
		t.Fatal(err)
	}
	// Four of the repository's own templates, five of the pack it imports.
	if len(library.Templates) != 9 || len(library.Constraints) != 10 {
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
			graph, model := fixtureObjects(t, dir, "market-mini", library)
			reviewed := []*unstructured.Unstructured{graph, model}
			report, err := policyeval.EvaluateLibrary(context.Background(), library, reviewed, reviewed)
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
				"rfp-host-reach-in",
				"rfp-guest-reports-network",
			} {
				if !constraints[want] {
					t.Errorf("violating fixture does not trigger %s: %v", want, constraints)
				}
			}
			if !policies["rfp-host-reach-in"] || !policies["rfp-guest-reports-network"] {
				t.Errorf("the imported pack did not run: %v", policies)
			}
		})
	}
}
