package policyeval_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

const templateHeader = `
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: relayonly
  annotations:
    specs.publicdomainrelay.dev/title: ssh over the relay only
    specs.publicdomainrelay.dev/level: MUST
spec:
  crd:
    spec:
      names:
        kind: RelayOnly
      validation:
        openAPIV3Schema:
          type: object
          properties:
            globs:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
`

const templateRego = `package relayonly

import data.lib.specd

violation[specd.violation(msg, specd.location(file.path, 1))] {
	file := specd.files_matching(input.parameters.globs)[_]
	not startswith(file.path, "relay/")
	msg := sprintf("%v must live under relay/", [file.path])
}
`

const constraintYAML = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: RelayOnly
metadata:
  name: relay-only
spec:
  enforcementAction: deny
  match:
    kinds:
      - apiGroups: ["specs.publicdomainrelay.dev"]
        kinds: ["CodeGraph"]
  parameters:
    globs: ["**/*.ts"]
`

const graphYAML = `
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: CodeGraph
metadata:
  name: market
  namespace: default
spec:
  repository: market
  commit: deadbeef
  files:
    - path: relay/a.ts
      language: typescript
    - path: bidder/b.ts
      language: typescript
  nodes: []
  edges: []
`

const graphAllowedYAML = `
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: CodeGraph
metadata:
  name: market
  namespace: default
spec:
  repository: market
  commit: deadbeef
  files:
    - path: relay/a.ts
      language: typescript
  nodes: []
  edges: []
`

const policiesYAML = `
repository: market
version: 1
testGlobs: ["**/*_test.ts"]
defaultEnforcement: dryrun
`

const suiteYAML = `
apiVersion: test.gatekeeper.sh/v1alpha1
kind: Suite
metadata:
  name: relayonly
tests:
  - name: relay-only
    template: template.yaml
    constraint: constraint.yaml
    cases:
      - name: allowed
        object: inventory/graph-allowed.yaml
        assertions:
          - violations: 0
      - name: denied
        object: inventory/graph.yaml
        inventory:
          - inventory/graph.yaml
        assertions:
          - violations: 1
            message: "must live under relay"
`

func libraryFS(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"policies.yaml":                                {Data: []byte(policiesYAML)},
		policy.LibPath:                                 {Data: []byte(policyeval.Lib())},
		"templates/relayonly/template.yaml":            {Data: []byte(templateHeader)},
		"templates/relayonly/src.rego":                 {Data: []byte(templateRego)},
		"constraints/relay-only.yaml":                  {Data: []byte(constraintYAML)},
		"tests/relayonly/suite.yaml":                   {Data: []byte(suiteYAML)},
		"tests/relayonly/template.yaml":                {Data: []byte(templateHeader + "      rego: |\n" + indent(templateRego, "        "))},
		"tests/relayonly/constraint.yaml":              {Data: []byte(constraintYAML)},
		"tests/relayonly/inventory/graph.yaml":         {Data: []byte(graphYAML)},
		"tests/relayonly/inventory/graph-allowed.yaml": {Data: []byte(graphAllowedYAML)},
	}
}

func indent(text, prefix string) string {
	out := ""
	for _, line := range splitLines(text) {
		out += prefix + line + "\n"
	}
	return out
}

func splitLines(text string) []string {
	lines := []string{}
	current := ""
	for _, char := range text {
		if char == '\n' {
			lines = append(lines, current)
			current = ""
			continue
		}
		current += string(char)
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func TestEvaluateReportsViolations(t *testing.T) {
	fsys := libraryFS(t)
	library, err := policyeval.LoadFS(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Templates) != 1 || len(library.Constraints) != 1 {
		t.Fatalf("library contents: %d templates, %d constraints", len(library.Templates), len(library.Constraints))
	}
	graph, err := policy.Unstructured([]byte(graphYAML))
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyeval.EvaluateLibrary(context.Background(), library, []*unstructured.Unstructured{graph}, []*unstructured.Unstructured{graph})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(report.Violations), report.Violations)
	}
	violation := report.Violations[0]
	if violation.Constraint != "relay-only" || violation.Policy != "relayonly" {
		t.Fatalf("violation identity: %+v", violation)
	}
	if violation.Enforcement != policy.EnforcementDeny || violation.Severity != policy.SeverityError {
		t.Fatalf("violation enforcement: %+v", violation)
	}
	if violation.Location == nil || violation.Location.File != "bidder/b.ts" {
		t.Fatalf("violation location: %+v", violation.Location)
	}
	if report.Totals[policy.EnforcementDeny] != 1 {
		t.Fatalf("totals: %v", report.Totals)
	}
}

func TestRunSuite(t *testing.T) {
	result, err := policyeval.RunSuite(context.Background(), libraryFS(t), "tests/relayonly/suite.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed() {
		t.Fatalf("suite failed: %+v", result)
	}
	if result.Cases() != 2 {
		t.Fatalf("cases: %d", result.Cases())
	}
}

func TestRunOpaTests(t *testing.T) {
	results, err := policyeval.RunOpaTests(context.Background(), map[string]string{
		"lib/specd.rego":      policyeval.Lib(),
		"lib/specd_test.rego": policyeval.LibTest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("the embedded library has no unit tests")
	}
	for _, result := range results {
		if !result.Passed() {
			t.Fatalf("library unit test %s failed: %s", result.Name, result.Error)
		}
	}
}

func TestWriteLibIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	changed, err := policyeval.WriteLib(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first write must report a change")
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(policy.LibPath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != policyeval.Lib() {
		t.Fatal("written lib differs from the embedded one")
	}
	changed, err = policyeval.WriteLib(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second write must be a no-op")
	}
}
