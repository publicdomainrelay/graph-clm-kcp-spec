package policy_test

import (
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func baselineViolation(constraint, file string, line int) policy.Violation {
	return policy.Violation{
		Constraint:  constraint,
		Msg:         constraint + " at " + file,
		Enforcement: policy.EnforcementDeny,
		Object:      policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "market-mini"},
		Location:    &policy.Location{File: file, Line: line},
	}
}

func baselineReport(violations ...policy.Violation) policy.Report {
	return policy.Report{Violations: violations}
}

func TestDecideBaselineInheritsAndBlocksOnlyTheNew(t *testing.T) {
	old := baselineViolation("relay-only", "test/a.ts", 10)
	fresh := baselineViolation("relay-only", "test/b.ts", 20)
	head := baselineReport(old, fresh)
	base := baselineReport(old)

	decision := policy.DecideBaseline(head, base, policy.RepositoryPolicy{}, nil)
	if !decision.Blocked {
		t.Fatal("the new violation did not block")
	}
	if len(decision.Denied) != 1 || decision.Denied[0].Location.File != "test/b.ts" {
		t.Errorf("denied = %+v, want only the new site", decision.Denied)
	}
	if len(decision.Inherited) != 1 || decision.Inherited[0].Location.File != "test/a.ts" {
		t.Fatalf("inherited = %+v, want the pre-existing site", decision.Inherited)
	}
	if decision.Inherited[0].Enforcement != policy.EnforcementWarn {
		t.Errorf("the inherited violation is not reported as warn: %s", decision.Inherited[0].Enforcement)
	}
}

func TestDecideBaselineIgnoresARewordedMessage(t *testing.T) {
	site := baselineViolation("relay-only", "test/a.ts", 10)
	head := site
	head.Msg = "a different message about the same site"
	decision := policy.DecideBaseline(baselineReport(head), baselineReport(site), policy.RepositoryPolicy{}, nil)
	if decision.Blocked {
		t.Fatalf("a reworded message blocked: %+v", decision.Denied)
	}
	if len(decision.Inherited) != 1 {
		t.Fatalf("inherited = %+v", decision.Inherited)
	}
}

func TestDecideBaselineNoneGatesTheWholeRepository(t *testing.T) {
	old := baselineViolation("relay-only", "test/a.ts", 10)
	decision := policy.DecideBaseline(baselineReport(old), baselineReport(old),
		policy.RepositoryPolicy{Baseline: "none"}, nil)
	if !decision.Blocked {
		t.Fatal("baseline none did not block a pre-existing violation")
	}
	if len(decision.Inherited) != 0 {
		t.Errorf("baseline none reported inherited violations: %+v", decision.Inherited)
	}
}

func TestDecideBaselineKeepsTheCapAndTheOverride(t *testing.T) {
	fresh := baselineViolation("relay-only", "test/b.ts", 20)
	capped := policy.DecideBaseline(baselineReport(fresh), baselineReport(),
		policy.RepositoryPolicy{Enforcement: policy.EnforcementWarn}, nil)
	if capped.Blocked {
		t.Fatalf("the cap did not soften the new violation: %+v", capped)
	}
	waived := policy.DecideBaseline(baselineReport(fresh), baselineReport(), policy.RepositoryPolicy{},
		[]policy.Override{{Constraint: "relay-only", Reason: "migration", By: "operator"}})
	if waived.Blocked || len(waived.Waived) != 1 {
		t.Fatalf("the override did not waive the new violation: %+v", waived)
	}
}
