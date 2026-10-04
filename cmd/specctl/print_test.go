package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

func testObject(t *testing.T, value any) unstructured.Unstructured {
	t.Helper()
	object, err := kcpclient.Unstructured(value)
	if err != nil {
		t.Fatal(err)
	}
	return *object
}

func TestPrintTableForSystemContext(t *testing.T) {
	context := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec: spec.SystemContextSpec{
			Repository: "calc",
			Intent:     "the smallest complete unit of the spec loop",
			Interfaces: []spec.Interface{{Name: "Add"}, {Name: "Multiply"}},
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "add"},
			},
		},
		Status: spec.SystemContextStatus{Conditions: []metav1.Condition{
			{Type: specapi.ConditionSpecValid, Status: metav1.ConditionTrue},
		}},
	}
	context.SetDefaults()
	out := &bytes.Buffer{}
	if err := printObjects(out, []unstructured.Unstructured{testObject(t, context)}, "table"); err != nil {
		t.Fatal(err)
	}
	printed := out.String()
	for _, want := range []string{"NAME", "REPOSITORY", "VALID", "calc", "2", "1", "True"} {
		if !strings.Contains(printed, want) {
			t.Errorf("table is missing %q:\n%s", want, printed)
		}
	}
}

func TestPrintTableForRepositoryAndSpecChange(t *testing.T) {
	repository := &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       spec.RepositorySpec{Path: "/repos/calc", Branch: "main"},
		Status:     spec.RepositoryStatus{HeadCommit: "abcdef1234567890"},
	}
	repository.SetDefaults()
	change := &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-1"},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc",
			Direction:     specapi.DirectionCodeToSpec,
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	}
	change.SetDefaults()

	out := &bytes.Buffer{}
	if err := printObjects(out, []unstructured.Unstructured{testObject(t, repository)}, "table"); err != nil {
		t.Fatal(err)
	}
	printed := out.String()
	for _, want := range []string{"SOURCE", "PHASE", "CONTEXTS", "HEADCOMMIT", "/repos/calc", "abcdef123..."} {
		if !strings.Contains(printed, want) {
			t.Errorf("repository table is missing %q:\n%s", want, printed)
		}
	}

	out.Reset()
	if err := printObjects(out, []unstructured.Unstructured{testObject(t, change)}, "table"); err != nil {
		t.Fatal(err)
	}
	printed = out.String()
	for _, want := range []string{"CONTEXT", "DIRECTION", "PHASE", "calc", "CodeToSpec", "Pending"} {
		if !strings.Contains(printed, want) {
			t.Errorf("change table is missing %q:\n%s", want, printed)
		}
	}
}

func TestPrintEmptyTable(t *testing.T) {
	out := &bytes.Buffer{}
	if err := printObjects(out, nil, "table"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No resources found") {
		t.Errorf("empty table = %q", out.String())
	}
}

func TestPrintNameYAMLAndJSON(t *testing.T) {
	context := &spec.SystemContext{ObjectMeta: metav1.ObjectMeta{Name: "calc"}}
	context.SetDefaults()
	items := []unstructured.Unstructured{testObject(t, context)}

	out := &bytes.Buffer{}
	if err := printObjects(out, items, "name"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "systemcontext/calc" {
		t.Errorf("name output = %q", out.String())
	}

	out.Reset()
	if err := printObjects(out, items, "yaml"); err != nil {
		t.Fatal(err)
	}
	decoded := map[string]any{}
	if err := yaml.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("yaml output is not yaml: %v\n%s", err, out.String())
	}
	if decoded["kind"] != specapi.SystemContextKind {
		t.Errorf("yaml kind = %v", decoded["kind"])
	}

	out.Reset()
	if err := printObjects(out, items, "json"); err != nil {
		t.Fatal(err)
	}
	listed := struct {
		Kind  string `json:"kind"`
		Items []any  `json:"items"`
	}{}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("json output is not json: %v\n%s", err, out.String())
	}
	if listed.Kind != "List" || len(listed.Items) != 1 {
		t.Errorf("json list = %+v", listed)
	}
}

func TestPrintRejectsAnUnknownFormat(t *testing.T) {
	out := &bytes.Buffer{}
	err := printObjects(out, nil, "csv")
	if err == nil || !strings.Contains(err.Error(), "unknown output format") {
		t.Fatalf("err = %v", err)
	}
}

func TestTruncate(t *testing.T) {
	cases := map[string]struct {
		value string
		limit int
		want  string
	}{
		"short":     {"abc", 10, "abc"},
		"long":      {"abcdefghij", 5, "ab..."},
		"tinyLimit": {"abcdef", 3, "abc"},
	}
	for name, testCase := range cases {
		if got := truncate(testCase.value, testCase.limit); got != testCase.want {
			t.Errorf("%s: truncate(%q, %d) = %q, want %q", name, testCase.value, testCase.limit, got, testCase.want)
		}
	}
}

func TestConditionStatusOf(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: specapi.ConditionSpecValid, Status: metav1.ConditionTrue},
		{Type: specapi.ConditionDrifted, Status: metav1.ConditionFalse},
	}
	if got := conditionStatusOf(conditions, specapi.ConditionSpecValid); got != "True" {
		t.Errorf("SpecValid = %q", got)
	}
	if got := conditionStatusOf(conditions, specapi.ConditionDrifted); got != "False" {
		t.Errorf("Drifted = %q", got)
	}
	if got := conditionStatusOf(conditions, "Missing"); got != "" {
		t.Errorf("missing condition = %q", got)
	}
}

func TestDisplayKeyPrefersTheLabelKey(t *testing.T) {
	row := map[string]any{"name": "calc", "intent": "the library"}
	if got := displayKey(row, "SpecContext"); got != "calc" {
		t.Errorf("context key = %q", got)
	}
	row = map[string]any{"codegraphId": "file:calc/calc.go", "kind": "file"}
	if got := displayKey(row, "CodeRef"); got != "file:calc/calc.go" {
		t.Errorf("code ref key = %q", got)
	}
	row = map[string]any{"reqId": "r.add", "level": "MUST", "text": "add\nreturns\nthe sum"}
	if got := displayKey(row, "SpecRequirement"); got != "r.add" {
		t.Errorf("requirement key = %q", got)
	}
}

func TestOneLineCollapsesWhitespace(t *testing.T) {
	if got := oneLine("a\nb\tc", 80); got != "a b c" {
		t.Errorf("oneLine = %q", got)
	}
	if got := oneLine(strings.Repeat("x", 100), 10); got != "xxxxxxx..." {
		t.Errorf("oneLine truncation = %q", got)
	}
}

func TestPrintTableShowsTheDeltaCompactly(t *testing.T) {
	change := &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-abc"},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc",
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    strings.Repeat("a", 64),
			Delta: &spec.Delta{
				Requirements: []spec.RequirementDelta{{
					Op: spec.OpAdded, ID: "r.subtract",
					To: &spec.Requirement{ID: "r.subtract", Level: spec.LevelShould, Text: "Subtract returns the difference."},
				}},
				Interfaces: []spec.InterfaceDelta{{
					Op: spec.OpAdded, Name: "Subtract",
					To: &spec.Interface{Name: "Subtract", Kind: "function"},
				}},
			},
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhaseSucceeded, Commit: "abcdef1234567890"},
	}
	change.SetDefaults()

	out := &bytes.Buffer{}
	if err := printObjects(out, []unstructured.Unstructured{testObject(t, change)}, "table"); err != nil {
		t.Fatal(err)
	}
	printed := out.String()
	for _, want := range []string{"DELTA", "COMMIT", "+2", "abcdef123..."} {
		if !strings.Contains(printed, want) {
			t.Errorf("change table is missing %q:\n%s", want, printed)
		}
	}

	plain := &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-old"},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc", Direction: specapi.DirectionCodeToSpec,
			FromCommit: "a", ToCommit: "b",
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	}
	plain.SetDefaults()
	out.Reset()
	if err := printObjects(out, []unstructured.Unstructured{testObject(t, plain)}, "table"); err != nil {
		t.Fatal(err)
	}
	row := out.String()
	if !strings.Contains(row, "-") {
		t.Errorf("a change with no delta printed no dash:\n%s", row)
	}
}
