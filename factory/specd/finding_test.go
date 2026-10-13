package specd

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/finding"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func proposalChange(name, context string) *spec.SpecChange {
	return &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec:       spec.SpecChangeSpec{SystemContext: context},
	}
}

func TestFindingRecordProposalFindingsCreatesOneFindingPerProposal(t *testing.T) {
	cluster := newFakeCluster()
	controller := testController(cluster)
	change := proposalChange("change-7", "calc")
	proposals := []spec.Proposal{
		{Unit: "requirement/r.add", Proposed: "Add returns the sum of two integers."},
		{Unit: "interface/Add", Proposed: "Add takes two integers."},
	}

	if err := controller.recordProposalFindings(context.Background(), specapi.DefaultNamespace, "calc", change, proposals); err != nil {
		t.Fatal(err)
	}

	names := cluster.names(specapi.FindingGVR)
	if len(names) != len(proposals) {
		t.Fatalf("findings = %v, want one per proposal", names)
	}
	for _, proposal := range proposals {
		recorded := readFinding(t, cluster, finding.Name("calc", proposal.Unit))
		if recorded.Spec.Source != finding.SourceProposal || recorded.Spec.Repository != "calc" {
			t.Errorf("finding = %+v, want a proposal from calc", recorded.Spec)
		}
		if recorded.Spec.Ref != change.Name || recorded.Spec.Context != change.Spec.SystemContext {
			t.Errorf("finding = %+v, want the change and its context", recorded.Spec)
		}
		if recorded.Spec.Unit != proposal.Unit || recorded.Spec.Severity != finding.SeverityInfo {
			t.Errorf("finding = %+v, want the proposed unit at info", recorded.Spec)
		}
		if want := "the code suggests: " + proposal.Proposed; recorded.Spec.Summary != want {
			t.Errorf("summary = %q, want %q", recorded.Spec.Summary, want)
		}
	}
}

func TestFindingRecordProposalFindingsRefreshesAndKeepsTheState(t *testing.T) {
	cluster := newFakeCluster()
	controller := testController(cluster)
	change := proposalChange("change-7", "calc")
	proposal := spec.Proposal{Unit: "requirement/r.add", Proposed: "Add returns the sum of two integers."}

	if err := controller.recordProposalFindings(context.Background(), specapi.DefaultNamespace, "calc", change, []spec.Proposal{proposal}); err != nil {
		t.Fatal(err)
	}
	name := finding.Name("calc", proposal.Unit)
	apply(t, cluster, &spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec:       spec.FindingSpec{Repository: "calc", Source: finding.SourceProposal, Summary: "the old value"},
		Status:     spec.FindingStatus{State: finding.Dismissed},
	})

	second := proposalChange("change-8", "calc")
	proposal.Proposed = "Add returns a sum."
	if err := controller.recordProposalFindings(context.Background(), specapi.DefaultNamespace, "calc", second, []spec.Proposal{proposal}); err != nil {
		t.Fatal(err)
	}

	recorded := readFinding(t, cluster, name)
	if recorded.Spec.Ref != second.Name {
		t.Errorf("ref = %q, want the new change %q", recorded.Spec.Ref, second.Name)
	}
	if want := "the code suggests: " + proposal.Proposed; recorded.Spec.Summary != want {
		t.Errorf("summary = %q, want %q", recorded.Spec.Summary, want)
	}
	if recorded.Status.State != finding.Dismissed {
		t.Errorf("state = %q, want the dismissed state kept", recorded.Status.State)
	}
	if names := cluster.names(specapi.FindingGVR); len(names) != 1 {
		t.Errorf("findings = %v, want the one it already holds", names)
	}
}

func TestFindingReconcileOpensAnEmptyStateAndLeavesPlannedAlone(t *testing.T) {
	cluster := newFakeCluster()
	controller := testController(cluster)
	apply(t, cluster, &spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: "f-new", Namespace: specapi.DefaultNamespace},
		Spec:       spec.FindingSpec{Repository: "calc", Source: "manual", Summary: "the code does not say this"},
	})
	apply(t, cluster, &spec.Finding{
		ObjectMeta: metav1.ObjectMeta{Name: "f-planned", Namespace: specapi.DefaultNamespace},
		Spec:       spec.FindingSpec{Repository: "calc", Source: "manual", Summary: "on a plan"},
		Status:     spec.FindingStatus{State: finding.Planned},
	})

	for _, name := range []string{"f-new", "f-planned"} {
		if _, err := controller.reconcileFinding(context.Background(), specapi.DefaultNamespace, name); err != nil {
			t.Fatal(err)
		}
	}

	if state := readFinding(t, cluster, "f-new").Status.State; state != finding.Open {
		t.Errorf("state = %q, want Open", state)
	}
	if state := readFinding(t, cluster, "f-planned").Status.State; state != finding.Planned {
		t.Errorf("state = %q, want Planned left alone", state)
	}
}
