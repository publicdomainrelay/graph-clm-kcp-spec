package policyeval_test

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

const anchorTemplateHeader = `
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: emissionanchor
  annotations:
    specs.publicdomainrelay.dev/title: the emission site
    specs.publicdomainrelay.dev/level: MUST
spec:
  crd:
    spec:
      names:
        kind: EmissionAnchor
      validation:
        openAPIV3Schema:
          type: object
          properties:
            line:
              type: integer
  targets:
    - target: admission.k8s.gatekeeper.sh
`

const anchorTemplateRego = `package emissionanchor

import data.lib.specd

violation[specd.violation(msg, specd.location("lib/bidder.ts", input.parameters.line))] {
	msg := "the host emits vm.onNetwork from the provisioning lifecycle"
}
`

const anchorConstraintYAML = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: EmissionAnchor
metadata:
  name: emission-anchor
spec:
  enforcementAction: deny
  match:
    kinds:
      - apiGroups: ["specs.publicdomainrelay.dev"]
        kinds: ["CodeGraph"]
  parameters:
    line: %d
`

const anchorPoliciesYAML = `
repository: market
version: 1
defaultEnforcement: deny
`

func anchorGraphYAML(start, end int) string {
	return fmt.Sprintf(`
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: CodeGraph
metadata:
  name: market
  namespace: default
spec:
  repository: market
  commit: deadbeef
  files:
    - path: lib/bidder.ts
      language: typescript
  nodes:
    - id: function:aaa
      kind: function
      name: createVmBidderCallbacks
      qualifiedName: createVmBidderCallbacks
      file: lib/bidder.ts
      startLine: %d
      endLine: %d
  edges: []
`, start, end)
}

func anchorReport(t *testing.T, line, start, end int) policy.Report {
	t.Helper()
	library, err := policyeval.LoadFS(fstest.MapFS{
		"policies.yaml":                          {Data: []byte(anchorPoliciesYAML)},
		policy.LibPath:                           {Data: []byte(policyeval.Lib())},
		"templates/emissionanchor/template.yaml": {Data: []byte(anchorTemplateHeader)},
		"templates/emissionanchor/src.rego":      {Data: []byte(anchorTemplateRego)},
		"constraints/emission-anchor.yaml":       {Data: []byte(fmt.Sprintf(anchorConstraintYAML, line))},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := policy.Unstructured([]byte(anchorGraphYAML(start, end)))
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyeval.EvaluateLibrary(context.Background(), library,
		[]*unstructured.Unstructured{graph}, []*unstructured.Unstructured{graph})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestEvaluationAnchorsAViolationToItsDeclaration(t *testing.T) {
	before := anchorReport(t, 20, 10, 40)
	if len(before.Violations) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(before.Violations), before.Violations)
	}
	location := before.Violations[0].Location
	if location == nil || location.Declaration != "createVmBidderCallbacks" {
		t.Fatalf("location = %+v, want the enclosing declaration", location)
	}

	shifted := anchorReport(t, 25, 12, 42)
	if policy.Key(before.Violations[0]) != policy.Key(shifted.Violations[0]) {
		t.Errorf("an edit that moved the site inside the same declaration changed the key: %s at line %d, %s at line %d",
			policy.Key(before.Violations[0]), location.Line,
			policy.Key(shifted.Violations[0]), shifted.Violations[0].Location.Line)
	}
}
