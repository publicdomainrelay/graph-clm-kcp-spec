package specd

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/coverage"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
)

func storedChange(t *testing.T, cluster Cluster, name string) *spec.SpecChange {
	t.Helper()
	change := &spec.SpecChange{}
	change.Name = name
	change.Namespace = specapi.DefaultNamespace
	change.Spec = spec.SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionSpecToCode}
	change.Status = spec.SpecChangeStatus{Phase: specapi.PhaseRunning}
	applied := apply(t, cluster, change)
	typed, err := kcpclient.Typed(applied)
	if err != nil {
		t.Fatal(err)
	}
	return typed.(*spec.SpecChange)
}

func TestRecordBatchSuccessRecordsRequirementCoverage(t *testing.T) {
	cluster := newFakeCluster()
	change := storedChange(t, cluster, "calc-s2c-22222222")
	controller := testController(cluster)
	repository := &spec.Repository{ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace}}
	result := realize.Result{
		Context:        "calc",
		Commit:         "c0ffee",
		VerifyExitCode: 0,
		FilesTouched:   []string{"calc/calc.go"},
		Coverage: map[string][]coverage.Verdict{
			"calc-s2c-22222222": {
				{ID: "r.add", Implemented: true, Evidence: "func Add"},
				{ID: "r.sub", Implemented: false, Evidence: "no Subtract"},
			},
		},
	}
	controller.recordBatchSuccess(context.Background(), specapi.DefaultNamespace, repository, []*spec.SpecChange{change}, result)

	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-22222222")
	if err != nil {
		t.Fatal(err)
	}
	if phase, _, _ := unstructured.NestedString(object.Object, "status", "phase"); phase != specapi.PhaseSucceeded {
		t.Fatalf("phase = %q", phase)
	}
	entries, found, _ := unstructured.NestedSlice(object.Object, "status", "requirementCoverage")
	if !found || len(entries) != 2 {
		t.Fatalf("requirementCoverage = %v", entries)
	}
	status := conditionStatus(t, object, specapi.ConditionRequirementsUnimplemented)
	if status != "True" {
		t.Fatalf("RequirementsUnimplemented = %q, want True", status)
	}
	message, _, _ := unstructured.NestedString(object.Object, "status", "message")
	if !strings.Contains(message, "r.sub") {
		t.Fatalf("message = %q, want the missing requirement", message)
	}
}

func TestRecordBatchSuccessMarksFullCoverageFalse(t *testing.T) {
	cluster := newFakeCluster()
	change := storedChange(t, cluster, "calc-s2c-33333333")
	controller := testController(cluster)
	repository := &spec.Repository{ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace}}
	result := realize.Result{
		Context:  "calc",
		Commit:   "c0ffee",
		Coverage: map[string][]coverage.Verdict{"calc-s2c-33333333": {{ID: "r.add", Implemented: true}}},
	}
	controller.recordBatchSuccess(context.Background(), specapi.DefaultNamespace, repository, []*spec.SpecChange{change}, result)

	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-33333333")
	if err != nil {
		t.Fatal(err)
	}
	if status := conditionStatus(t, object, specapi.ConditionRequirementsUnimplemented); status != "False" {
		t.Fatalf("RequirementsUnimplemented = %q, want False", status)
	}
}

func TestRecordBatchSuccessLeavesCoverageOffWhenNoJudgeRan(t *testing.T) {
	cluster := newFakeCluster()
	change := storedChange(t, cluster, "calc-s2c-44444444")
	controller := testController(cluster)
	repository := &spec.Repository{ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace}}
	controller.recordBatchSuccess(context.Background(), specapi.DefaultNamespace, repository, []*spec.SpecChange{change}, realize.Result{Commit: "c0ffee"})

	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-44444444")
	if err != nil {
		t.Fatal(err)
	}
	if entries, found, _ := unstructured.NestedSlice(object.Object, "status", "requirementCoverage"); found && len(entries) > 0 {
		t.Fatalf("a change with no judge carries coverage: %v", entries)
	}
	if conditions, found, _ := unstructured.NestedSlice(object.Object, "status", "conditions"); found && len(conditions) > 0 {
		t.Fatalf("a change with no judge carries conditions: %v", conditions)
	}
}
