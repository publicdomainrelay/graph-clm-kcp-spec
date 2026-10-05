package specd

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/realize"
)

func TestRecordBatchSuccessFlagsFilesAnotherContextOwns(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: "/does/not/matter", Branch: "main"},
	})
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
		systemContext.Status.Observed = spec.ObservedFacts{Files: []string{"calc/calc.go"}}
	}))
	apply(t, cluster, systemContext("registry", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
		systemContext.Status.Observed = spec.ObservedFacts{Files: []string{"registry/registry.go"}}
	}))
	change := storedChange(t, cluster, "calc-s2c-55555555")
	controller := testController(cluster)
	repository := readRepository(t, cluster, "calc")

	controller.recordBatchSuccess(context.Background(), specapi.DefaultNamespace, repository,
		[]*spec.SpecChange{change},
		realize.Result{Context: "calc", Commit: "c0ffee", FilesTouched: []string{"calc/calc.go", "registry/registry.go", "calc/new.go"}})

	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-55555555")
	if err != nil {
		t.Fatal(err)
	}
	if status := conditionStatus(t, object, specapi.ConditionFilesOutsideContext); status != "True" {
		t.Fatalf("FilesOutsideContext = %q, want True", status)
	}
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, entry := range conditions {
		condition, _ := entry.(map[string]any)
		if condition["type"] != specapi.ConditionFilesOutsideContext {
			continue
		}
		message, _ := condition["message"].(string)
		if !strings.Contains(message, "registry/registry.go") || !strings.Contains(message, "registry") {
			t.Errorf("message = %q, want the file and its owner", message)
		}
		if strings.Contains(message, "calc/new.go") {
			t.Errorf("message = %q, want a new file left out", message)
		}
	}
}

func TestRecordBatchSuccessLeavesAFullyOwnedRealizeAlone(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Path: "/does/not/matter", Branch: "main"},
	})
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
		systemContext.Status.Observed = spec.ObservedFacts{Files: []string{"calc/calc.go"}}
	}))
	change := storedChange(t, cluster, "calc-s2c-66666666")
	controller := testController(cluster)
	repository := readRepository(t, cluster, "calc")

	controller.recordBatchSuccess(context.Background(), specapi.DefaultNamespace, repository,
		[]*spec.SpecChange{change},
		realize.Result{Context: "calc", Commit: "c0ffee", FilesTouched: []string{"calc/calc.go", "calc/new.go"}})

	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-66666666")
	if err != nil {
		t.Fatal(err)
	}
	if conditions, found, _ := unstructured.NestedSlice(object.Object, "status", "conditions"); found && len(conditions) > 0 {
		t.Fatalf("a change that stayed in its context carries conditions: %v", conditions)
	}
}

func TestRecordBatchSuccessRecordsAnOverriddenGateAndConsumesIt(t *testing.T) {
	cluster := newFakeCluster()
	apply(t, cluster, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   "/does/not/matter",
			Branch: "main",
			AcceptanceOverrides: []spec.AcceptanceOverride{
				{Step: "market", Reason: "relay down", By: "operator"},
				{Step: "other", Reason: "keep me", By: "operator"},
			},
		},
	})
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Repository = "calc"
	}))
	change := storedChange(t, cluster, "calc-s2c-77777777")
	controller := testController(cluster)
	repository := readRepository(t, cluster, "calc")

	controller.recordBatchSuccess(context.Background(), specapi.DefaultNamespace, repository,
		[]*spec.SpecChange{change},
		realize.Result{
			Context: "calc",
			Commit:  "c0ffee",
			Acceptance: []spec.AcceptanceResult{
				{Name: "market", Passed: false, ExitCode: 2, Overridden: true, OverrideBy: "operator", OverrideReason: "relay down"},
			},
		})

	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-77777777")
	if err != nil {
		t.Fatal(err)
	}
	if status := conditionStatus(t, object, specapi.ConditionAcceptanceOverridden); status != "True" {
		t.Fatalf("AcceptanceOverridden = %q, want True", status)
	}
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, entry := range conditions {
		condition, _ := entry.(map[string]any)
		if condition["type"] != specapi.ConditionAcceptanceOverridden {
			continue
		}
		message, _ := condition["message"].(string)
		if !strings.Contains(message, "market") || !strings.Contains(message, "operator") || !strings.Contains(message, "relay down") {
			t.Errorf("message = %q", message)
		}
	}

	updated := readRepository(t, cluster, "calc")
	if len(updated.Spec.AcceptanceOverrides) != 1 || updated.Spec.AcceptanceOverrides[0].Step != "other" {
		t.Errorf("overrides = %+v, want the consumed one gone and the unused one kept", updated.Spec.AcceptanceOverrides)
	}
}
