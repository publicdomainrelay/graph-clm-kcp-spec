package policyeval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

const generatedSource = `package relayonly

import data.lib.specd

host_role := object.get(input.parameters, "hostRole", "host")

guest_role := object.get(input.parameters, "guestRole", "guest")

violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	specd.initiator_in_role(flow, host_role)
	specd.acted_on_in_role(flow, guest_role)
	msg := sprintf("the host acts on the guest: %s -> %s", [flow.from, flow.to])
	details := {"from": flow.from, "to": flow.to}
}
`

const generatedTest = `package relayonly

parameters := {"hostRole": "host", "guestRole": "guest"}

review := {"kind": {"kind": "ArchitectureModel"}, "object": {"metadata": {"name": "fixture", "namespace": "default"}, "spec": {"repository": "fixture"}}}

model(flows) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"ArchitectureModel": {"fixture": {
	"metadata": {"name": "fixture", "namespace": "default"},
	"spec": {
		"repository": "fixture",
		"roles": ["guest", "host"],
		"components": [{"name": "host", "roles": ["host"], "source": "observed"}, {"name": "guest", "roles": ["guest"], "source": "observed"}],
		"effects": [{"id": "e1", "kind": "event.emit", "component": "host", "file": "bidder/mod.ts", "line": 1}],
		"flows": flows,
		"triggers": [],
	},
}}}}}}

test_no_violation_when_the_guest_initiates {
	flow := {"from": "guest", "to": "host", "initiator": "guest", "carries": ["network-info"], "source": "observed", "evidence": ["e1"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model([flow])
	count(violations) == 0
}

test_violation_when_the_host_acts_on_the_guest {
	flow := {"from": "host", "to": "guest", "initiator": "host", "source": "observed", "evidence": ["e1"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model([flow])
	count(violations) == 1
}
`

const generatedHeader = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: relayonly
  annotations:
    specs.publicdomainrelay.dev/title: the host never acts on the guest
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
            hostRole:
              type: string
            guestRole:
              type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: ""
      libs: []
`

const generatedConstraint = `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: RelayOnly
metadata:
  name: relay-only
spec:
  enforcementAction: deny
  match:
    kinds:
      - apiGroups: [specs.publicdomainrelay.dev]
        kinds: [ArchitectureModel]
  parameters:
    hostRole: host
    guestRole: guest
`

const generatedSuite = `apiVersion: test.gatekeeper.sh/v1alpha1
kind: Suite
metadata:
  name: relay-only
tests:
  - name: relay-only
    template: ../../dist/relay-only.yaml
    constraint: ../../constraints/relay-only.yaml
    cases:
      - name: allowed
        object: inventory/allowed.yaml
        inventory:
          - inventory/allowed.yaml
        assertions:
          - violations: 0
      - name: denied
        object: inventory/denied.yaml
        inventory:
          - inventory/denied.yaml
        assertions:
          - violations: 1
`

const allowedModel = `apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: ArchitectureModel
metadata:
  name: fixture
  namespace: default
spec:
  repository: fixture
  roles: [guest, host]
  components:
    - {name: host, roles: [host], source: observed}
    - {name: guest, roles: [guest], source: observed}
  effects:
    - {id: e1, kind: event.emit, component: host, file: bidder/mod.ts, line: 1}
  flows:
    - {from: guest, to: host, initiator: guest, carries: [network-info], source: observed, evidence: [e1]}
  triggers: []
`

const deniedModel = `apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: ArchitectureModel
metadata:
  name: fixture
  namespace: default
spec:
  repository: fixture
  roles: [guest, host]
  components:
    - {name: host, roles: [host], source: observed}
    - {name: guest, roles: [guest], source: observed}
  effects:
    - {id: e1, kind: container.exec, component: host, file: bidder/mod.ts, line: 1}
  flows:
    - {from: host, to: guest, initiator: host, source: observed, evidence: [e1]}
  triggers: []
`

type treeOptions struct {
	source     string
	constraint string
	denied     string
}

func writeGeneratedTree(t *testing.T, options treeOptions) string {
	t.Helper()
	dir := t.TempDir()
	if options.source == "" {
		options.source = generatedSource
	}
	if options.constraint == "" {
		options.constraint = generatedConstraint
	}
	if options.denied == "" {
		options.denied = deniedModel
	}
	files := map[string]string{
		"templates/relay-only/template.yaml":      generatedHeader,
		"templates/relay-only/src.rego":           options.source,
		"templates/relay-only/src_test.rego":      generatedTest,
		"constraints/relay-only.yaml":             options.constraint,
		"tests/relay-only/suite.yaml":             generatedSuite,
		"tests/relay-only/inventory/allowed.yaml": allowedModel,
		"tests/relay-only/inventory/denied.yaml":  options.denied,
	}
	for name, contents := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func generatedInput(t *testing.T, dir string) GeneratedInput {
	t.Helper()
	return GeneratedInput{
		Dir:        dir,
		Slug:       "relay-only",
		Repository: "fixture",
		Contexts:   []string{"lib-bidder"},
		Binding: policy.Binding{Roles: map[string]policy.RoleBinding{
			"guest": {Targets: &policy.RoleTargets{Attrs: []string{"getNodeId"}}},
		}},
		Classifiers:  []string{"compute-provider.yaml"},
		Vocabulary:   policy.MutationVocabulary{HostRole: "host", GuestRole: "guest"},
		GeneratedBy:  "relay-only-abc",
		Requirements: []string{"lib-bidder#r.relay"},
	}
}

func TestCheckGeneratedAcceptsAModelPolicy(t *testing.T) {
	dir := writeGeneratedTree(t, treeOptions{})
	result, err := CheckGenerated(context.Background(), generatedInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed() {
		t.Fatalf("the generated policy fails its checks: %v", result.Failures())
	}
	if names := checkNames(result); strings.Join(names, ",") != "annotations,shape,portable,compile,units,suite,mutation,head evaluation" {
		t.Errorf("the checks ran as %v", names)
	}
	template, ok := result.Template()
	if !ok {
		t.Fatal("the result carries no template")
	}
	if template.Requirements[0] != "lib-bidder#r.relay" || template.GeneratedBy != "relay-only-abc" {
		t.Errorf("the annotations are %+v", template.Annotations)
	}
	if template.Severity != policy.SeverityError {
		t.Errorf("the severity is %q, want error", template.Severity)
	}
}

func TestCheckGeneratedRefusesAVacuousRule(t *testing.T) {
	source := `package relayonly

import data.lib.specd

violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	flow.purpose == "a-purpose-no-model-has"
	msg := "vacuous"
	details := {"from": flow.from}
}
`
	dir := writeGeneratedTree(t, treeOptions{source: source})
	result, err := CheckGenerated(context.Background(), generatedInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() {
		t.Fatal("a rule that denies no derived case passed")
	}
	mutation := checkByName(t, result, "mutation")
	if !strings.Contains(mutation.Message, "denies none of the derived cases") {
		t.Errorf("the mutation failure reads %q", mutation.Message)
	}
	if !strings.Contains(mutation.Message, "host-reaches-in") {
		t.Errorf("the mutation failure does not name the derived cases: %q", mutation.Message)
	}
}

func TestCheckGeneratedRefusesANonPortableRule(t *testing.T) {
	source := `package relayonly

import data.lib.specd

violation[specd.violation(msg, details)] {
	input.review.object.spec.repository == "fixture"
	flow := specd.model_flows[_]
	msg := "the fixture repository is named"
	details := {"from": flow.from}
}
`
	dir := writeGeneratedTree(t, treeOptions{source: source})
	result, err := CheckGenerated(context.Background(), generatedInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() {
		t.Fatal("a rule that names its repository passed")
	}
	portable := checkByName(t, result, "portable")
	if !strings.Contains(portable.Message, "fixture") {
		t.Errorf("the portability failure reads %q", portable.Message)
	}
}

func TestCheckGeneratedRefusesASourceThatDoesNotCompile(t *testing.T) {
	dir := writeGeneratedTree(t, treeOptions{source: "package relayonly\n\nviolation {\n"})
	result, err := CheckGenerated(context.Background(), generatedInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() {
		t.Fatal("a source that does not compile passed")
	}
	if check := checkByName(t, result, "compile"); check.Message == "" {
		t.Error("the compile failure carries no message")
	}
}

func TestCheckGeneratedRefusesARuleThatNeverReviewsTheModel(t *testing.T) {
	constraint := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: RelayOnly
metadata:
  name: relay-only
spec:
  enforcementAction: deny
  match:
    kinds:
      - apiGroups: [specs.publicdomainrelay.dev]
        kinds: [CodeGraph]
  parameters:
    hostRole: host
    guestRole: guest
`
	dir := writeGeneratedTree(t, treeOptions{constraint: constraint})
	result, err := CheckGenerated(context.Background(), generatedInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() {
		t.Fatal("a constraint that reviews no ArchitectureModel passed")
	}
	if check := checkByName(t, result, "shape"); !strings.Contains(check.Message, "ArchitectureModel") {
		t.Errorf("the shape failure reads %q", check.Message)
	}
}

// TestCheckGeneratedRefusesARuleThatReadsAHint pins plan 0010 D2: a portable
// rule may not depend on a repository-specific target hint -- the getNodeId
// verb, or a repository's own classifier pack -- because a second repository
// has neither.
func TestCheckGeneratedRefusesARuleThatReadsAHint(t *testing.T) {
	hint := `package relayonly

import data.lib.specd

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	effect.attrs.getNodeId
	msg := "the rule reads the repository's own hint"
	details := {"effect": effect.id}
}
`
	dir := writeGeneratedTree(t, treeOptions{source: hint})
	result, err := CheckGenerated(context.Background(), generatedInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() {
		t.Fatal("a rule that reads getNodeId passed")
	}
	portable := checkByName(t, result, "portable")
	if !strings.Contains(portable.Message, "getNodeId") {
		t.Errorf("the portability failure reads %q", portable.Message)
	}

	classifier := `package relayonly

import data.lib.specd

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	effect.file == "compute-provider.yaml"
	msg := "the rule names a classifier pack"
	details := {"effect": effect.id}
}
`
	dir = writeGeneratedTree(t, treeOptions{source: classifier})
	result, err = CheckGenerated(context.Background(), generatedInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() {
		t.Fatal("a rule that names a classifier pack passed")
	}
	if message := checkByName(t, result, "portable").Message; !strings.Contains(message, "compute-provider") {
		t.Errorf("the portability failure reads %q", message)
	}
}

func checkNames(result GeneratedResult) []string {
	out := []string{}
	for _, check := range result.Checks {
		out = append(out, check.Name)
	}
	return out
}

func checkByName(t *testing.T, result GeneratedResult, name string) policy.Check {
	t.Helper()
	for _, check := range result.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("no check is named %q; the checks are %v", name, checkNames(result))
	return policy.Check{}
}
