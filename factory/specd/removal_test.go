package specd

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

const guardedRequirement = "r.add"

func removalFixture(t *testing.T, declared ...string) (*fakeCluster, *spec.SpecChange) {
	t.Helper()
	requirement := spec.Requirement{ID: guardedRequirement, Level: spec.LevelMust, Text: "Add returns the sum."}
	cluster := newFakeCluster()
	apply(t, cluster, systemContext("calc", func(systemContext *spec.SystemContext) {
		systemContext.Spec.Requirements = []spec.Requirement{}
		systemContext.Annotations = map[string]string{}
		if len(declared) > 0 {
			systemContext.Annotations[specapi.DeclaredAnnotation] = authority.Format(authority.NewSet(declared...))
		}
	}))
	change := &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-s2c-1", Namespace: specapi.DefaultNamespace},
		Spec: spec.SpecChangeSpec{
			SystemContext: "calc",
			Direction:     specapi.DirectionSpecToCode,
			Delta: &spec.Delta{Requirements: []spec.RequirementDelta{{
				Op: spec.OpRemoved, ID: guardedRequirement, From: &requirement,
			}}},
		},
		Status: spec.SpecChangeStatus{Phase: specapi.PhasePending},
	}
	apply(t, cluster, change)
	return cluster, change
}

func guardChange(t *testing.T, cluster *fakeCluster) *spec.SpecChange {
	t.Helper()
	object, err := cluster.Get(context.Background(), specapi.SpecChangeGVR, specapi.DefaultNamespace, "calc-s2c-1")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	return typed.(*spec.SpecChange)
}

func TestRemovalGuardHoldsAnUndeclaredRemoval(t *testing.T) {
	cluster, change := removalFixture(t)
	controller := testController(cluster)
	held, err := controller.holdUndeclaredRemoval(context.Background(), specapi.DefaultNamespace, change)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("an undeclared removal was not held")
	}
	readBack := guardChange(t, cluster)
	if readBack.Status.Phase != specapi.PhasePending {
		t.Fatalf("phase = %q, want Pending so a later declaration lets it run", readBack.Status.Phase)
	}
	if readBack.Status.Message != HeldRemovalPrefix+"requirement/"+guardedRequirement {
		t.Fatalf("message = %q", readBack.Status.Message)
	}
	if len(readBack.Status.Proposals) != 1 {
		t.Fatalf("proposals = %+v, want one for the removed unit", readBack.Status.Proposals)
	}
	proposal := readBack.Status.Proposals[0]
	if proposal.Unit != "requirement/"+guardedRequirement {
		t.Errorf("unit = %q", proposal.Unit)
	}
	if !strings.Contains(proposal.Current, "Add returns the sum.") || proposal.Proposed != "" {
		t.Errorf("proposal = %+v, want the value the spec held and the empty removal", proposal)
	}
}

func TestRemovalGuardLetsADeclaredRemovalRun(t *testing.T) {
	cluster, change := removalFixture(t, "requirement/"+guardedRequirement)
	controller := testController(cluster)
	held, err := controller.holdUndeclaredRemoval(context.Background(), specapi.DefaultNamespace, change)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("a declared removal was held")
	}
	if message := guardChange(t, cluster).Status.Message; message != "" {
		t.Errorf("message = %q, want the change untouched", message)
	}
}

func TestRemovalGuardLetsALaterDeclarationRun(t *testing.T) {
	cluster, change := removalFixture(t)
	controller := testController(cluster)
	if held, err := controller.holdUndeclaredRemoval(context.Background(), specapi.DefaultNamespace, change); err != nil || !held {
		t.Fatalf("held = %v err = %v", held, err)
	}

	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	systemContext := typed.(*spec.SystemContext)
	if systemContext.Annotations == nil {
		systemContext.Annotations = map[string]string{}
	}
	systemContext.Annotations[specapi.DeclaredAnnotation] = authority.Format(authority.NewSet("requirement/" + guardedRequirement))
	apply(t, cluster, systemContext)

	heldAgain, err := controller.holdUndeclaredRemoval(context.Background(), specapi.DefaultNamespace, guardChange(t, cluster))
	if err != nil {
		t.Fatal(err)
	}
	if heldAgain {
		t.Fatal("a removal the spec now declares is still held")
	}
}

func TestRemovalGuardLetsAPlanItemDeclareTheRemoval(t *testing.T) {
	cluster, change := removalFixture(t)
	object, err := cluster.Get(context.Background(), specapi.SystemContextGVR, specapi.DefaultNamespace, "calc")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		t.Fatal(err)
	}
	systemContext := typed.(*spec.SystemContext)
	systemContext.Annotations = map[string]string{specapi.PlanItemAnnotation: "plan-1/item-1"}
	apply(t, cluster, systemContext)
	apply(t, cluster, &spec.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-1", Namespace: specapi.DefaultNamespace},
		Spec: spec.PlanSpec{
			Repository: "calc",
			Items: []spec.PlanItem{{
				ID:    "item-1",
				Edits: []spec.PlanEdit{{Context: "calc", RemoveRequirements: []string{guardedRequirement}}},
			}},
		},
	})

	controller := testController(cluster)
	held, err := controller.holdUndeclaredRemoval(context.Background(), specapi.DefaultNamespace, change)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("a removal the plan item names was held")
	}
}

func TestRemovalGuardIgnoresAChangeThatRemovesNothing(t *testing.T) {
	cluster, change := removalFixture(t)
	change.Spec.Delta = &spec.Delta{}
	controller := testController(cluster)
	held, err := controller.holdUndeclaredRemoval(context.Background(), specapi.DefaultNamespace, change)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("a change that removes nothing was held")
	}
}
