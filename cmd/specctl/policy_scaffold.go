package main

import (
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

type scaffoldOptions struct {
	Slug string

	TemplateName string

	Kind string

	Title string

	Level string

	Pattern string

	Sample string

	Path string

	Globs []string

	Enforcement string
}

func scaffold(opts scaffoldOptions) ([]byte, []byte, []byte, []byte, []byte, map[string][]byte) {
	header := fmt.Sprintf(`apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: %s
  annotations:
    %s: %s
    %s: %s
spec:
  crd:
    spec:
      names:
        kind: %s
      validation:
        openAPIV3Schema:
          type: object
          properties:
            globs:
              type: array
              items:
                type: string
            pattern:
              type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
`, opts.TemplateName, policy.AnnotationTitle, strconv.Quote(opts.Title),
		policy.AnnotationLevel, opts.Level, opts.Kind)

	globsLiteral := "[" + strings.Join(quoted(opts.Globs), ", ") + "]"
	format := fmt.Sprintf("%s: %%s matches %%q", opts.Title)
	source := fmt.Sprintf(`package %s

import data.lib.specd

violation[specd.violation(msg, specd.location(file.path, 1))] {
	file := specd.files_matching(input.parameters.globs)[_]
	re_match(input.parameters.pattern, specd.code_graph.spec.texts[file.path])
	msg := sprintf(%s, [file.path, input.parameters.pattern])
}
`, opts.TemplateName, strconv.Quote(format))

	test := fmt.Sprintf(`package %s

inventory := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"CodeGraph": {"x": {
	"metadata": {"name": "x", "namespace": "default"},
	"spec": {
		"repository": "x",
		"files": [{"path": %s}],
		"nodes": [],
		"edges": [],
		"texts": {%s: %s},
	},
}}}}}}

review := {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "x", "namespace": "default"}, "spec": {"repository": "x"}}}

test_violation_when_the_pattern_matches {
	call := {"parameters": {"globs": %s, "pattern": %s}, "review": review}
	violations := violation with input as call with data.inventory as inventory
	count(violations) == 1
}

test_no_violation_when_the_pattern_is_absent {
	call := {"parameters": {"globs": %s, "pattern": "a pattern that is not there"}, "review": review}
	violations := violation with input as call with data.inventory as inventory
	count(violations) == 0
}
`, opts.TemplateName, strconv.Quote(opts.Path), strconv.Quote(opts.Path), strconv.Quote(sampleText(opts.Sample)), globsLiteral, strconv.Quote(opts.Pattern), globsLiteral)

	constraint := policy.Constraint{
		Name:        opts.Slug,
		Kind:        opts.Kind,
		Enforcement: policy.Enforcement(opts.Enforcement),
		Match: policy.Match{
			Kinds: []policy.MatchKind{{APIGroups: []string{policy.Group}, Kinds: []string{policy.CodeGraphKind}}},
		},
		Parameters: map[string]any{
			"globs":   toAnySlice(opts.Globs),
			"pattern": opts.Pattern,
		},
	}
	constraintDoc, err := constraint.Document()
	if err != nil {
		constraintDoc = nil
	}

	allowed := scaffoldGraph(opts.Slug, allowedPath(opts.Path), "const clean = 1\n")
	denied := scaffoldGraph(opts.Slug, opts.Path, sampleText(opts.Sample))

	suite := fmt.Sprintf(`apiVersion: test.gatekeeper.sh/v1alpha1
kind: Suite
metadata:
  name: %s
tests:
  - name: %s
    template: ../../dist/%s.yaml
    constraint: ../../constraints/%s.yaml
    cases:
      - name: allowed
        object: inventory/codegraph-allowed.yaml
        inventory:
          - inventory/codegraph-allowed.yaml
        assertions:
          - violations: 0
      - name: denied
        object: inventory/codegraph-denied.yaml
        inventory:
          - inventory/codegraph-denied.yaml
        assertions:
          - violations: 1
`, opts.Slug, opts.Slug, opts.Slug, opts.Slug)

	cases := map[string][]byte{
		policy.SuiteCasePath(opts.Slug, "inventory/codegraph-allowed.yaml"): allowed,
		policy.SuiteCasePath(opts.Slug, "inventory/codegraph-denied.yaml"):  denied,
	}
	return []byte(header), constraintDoc, []byte(source), []byte(test), []byte(suite), cases
}

func quoted(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.Quote(value))
	}
	return out
}

func samplePath(glob string) string {
	cleaned := strings.TrimSuffix(glob, "/**")
	cleaned = strings.TrimSuffix(cleaned, "/*")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" || strings.ContainsAny(cleaned, "*?[") {
		return "src/denied.ts"
	}
	return cleaned + "/denied.ts"
}

func allowedPath(path string) string {
	return strings.TrimSuffix(path, "denied.ts") + "allowed.ts"
}

func sampleText(sample string) string {
	return "const x = " + sample + "\n"
}

func scaffoldGraph(repository, file, text string) []byte {
	graph := policy.CodeGraph{
		APIVersion: policy.APIVersion,
		Kind:       policy.CodeGraphKind,
		Metadata:   policy.ObjectMeta{Name: repository, Namespace: "default"},
		Spec: policy.CodeGraphSpec{
			Repository: repository,
			Commit:     strings.Repeat("0", 40),
			Files:      []policy.CodeGraphFile{{Path: file, Language: "typescript"}},
			Nodes:      []policy.CodeGraphNode{},
			Edges:      []policy.CodeGraphEdge{},
			Texts:      map[string]string{file: text},
		},
	}
	encoded, err := yaml.Marshal(graph)
	if err != nil {
		return nil
	}
	return encoded
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
