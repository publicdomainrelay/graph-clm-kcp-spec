package policy_test

import (
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func violationAt(constraint, file string, line int) policy.Violation {
	return policy.Violation{
		Constraint:  constraint,
		Enforcement: policy.EnforcementDeny,
		Msg:         "reaches the guest",
		Object:      policy.ObjectRef{Kind: "CodeGraph", Namespace: "default", Name: "market-mini"},
		Location:    &policy.Location{File: file, Line: line},
	}
}

func TestAnExceptionWaivesOnlyItsOwnSite(t *testing.T) {
	report := policy.Report{Violations: []policy.Violation{
		violationAt("rfp-host-reach-in", "hono-bidder/mod.ts", 43),
		violationAt("rfp-host-reach-in", "hono-bidder/other.ts", 7),
	}}
	waivers := []policy.Override{{
		Constraint: "rfp-host-reach-in",
		File:       "hono-bidder/mod.ts",
		Line:       43,
		Reason:     "the address may be a public IPv4 the client judges",
		By:         "john",
	}}

	decision := policy.Decide(report, policy.RepositoryPolicy{}, waivers)
	if len(decision.Waived) != 1 || decision.Waived[0].Location.File != "hono-bidder/mod.ts" {
		t.Fatalf("waived = %+v, want only the named site", decision.Waived)
	}
	if len(decision.Denied) != 1 || decision.Denied[0].Location.File != "hono-bidder/other.ts" {
		t.Fatalf("denied = %+v, want the other site", decision.Denied)
	}
	if !decision.Blocked {
		t.Error("a violation the waiver does not cover must still block")
	}
}

func TestAKeyWaivesExactlyTheViolationItNames(t *testing.T) {
	first := violationAt("relay-only-ssh", "lib/requester/mod.ts", 17)
	second := violationAt("relay-only-ssh", "lib/requester/mod.ts", 18)
	report := policy.Report{Violations: []policy.Violation{first, second}}

	decision := policy.Decide(report, policy.RepositoryPolicy{}, []policy.Override{{Key: policy.Key(first), Reason: "accepted"}})
	if len(decision.Waived) != 1 || policy.Key(decision.Waived[0]) != policy.Key(first) {
		t.Fatalf("waived = %+v, want only %s", decision.Waived, policy.Key(first))
	}
	if len(decision.Denied) != 1 || policy.Key(decision.Denied[0]) != policy.Key(second) {
		t.Fatalf("denied = %+v, want only %s", decision.Denied, policy.Key(second))
	}
}

func TestAConstraintWideOverrideStillWaivesEveryViolationOfIt(t *testing.T) {
	report := policy.Report{Violations: []policy.Violation{
		violationAt("relay-only-ssh", "lib/requester/mod.ts", 17),
		violationAt("relay-only-ssh", "lib/requester/mod.ts", 18),
	}}
	decision := policy.Decide(report, policy.RepositoryPolicy{}, []policy.Override{{Constraint: "relay-only-ssh", Reason: "accepted"}})
	if len(decision.Waived) != 2 || decision.Blocked {
		t.Fatalf("waived = %d, blocked = %v", len(decision.Waived), decision.Blocked)
	}
}

func TestAnExpiredExceptionIsReportedAndNotHonoured(t *testing.T) {
	exceptions := []policy.Exception{
		{Constraint: "relay-only-ssh", Reason: "expired", Expires: "2020-01-01"},
		{Constraint: "relay-only-ssh", Reason: "still valid", Expires: "2999-01-01"},
		{Constraint: "relay-only-ssh", Reason: "no expiry"},
	}
	waivers, expired := policy.Waivers(exceptions, time.Now())
	if len(waivers) != 2 {
		t.Fatalf("waivers = %+v, want the two unexpired exceptions", waivers)
	}
	if len(expired) != 1 || expired[0].Reason != "expired" {
		t.Fatalf("expired = %+v, want the one that ran out", expired)
	}

	report := policy.Report{Violations: []policy.Violation{violationAt("relay-only-ssh", "lib/requester/mod.ts", 17)}}
	decision := policy.Decide(report, policy.RepositoryPolicy{}, waivers)
	if decision.Blocked {
		t.Fatalf("the unexpired exception did not waive: %+v", decision)
	}
}

func TestAnInheritedViolationIsWarnAndNeverBlocks(t *testing.T) {
	violation := violationAt("rfp-guest-reports-network", "lib/market-bidder-compute/mod.ts", 304)
	head := policy.Report{Violations: []policy.Violation{violation}}
	base := policy.Report{Violations: []policy.Violation{violation}}

	decision := policy.DecideBaseline(head, base, policy.RepositoryPolicy{}, nil)
	if decision.Blocked {
		t.Fatal("a violation the base carried must not block")
	}
	if len(decision.Inherited) != 1 || decision.Inherited[0].Enforcement != policy.EnforcementWarn {
		t.Fatalf("inherited = %+v, want one warn", decision.Inherited)
	}

	added := violationAt("rfp-host-reach-in", "hono-bidder/probe.ts", 4)
	decision = policy.DecideBaseline(policy.Report{Violations: []policy.Violation{violation, added}}, base, policy.RepositoryPolicy{}, nil)
	if !decision.Blocked || len(decision.Denied) != 1 || decision.Denied[0].Location.File != "hono-bidder/probe.ts" {
		t.Fatalf("a new violation must block: %+v", decision)
	}
	if len(decision.Inherited) != 1 {
		t.Fatalf("inherited = %+v, want the pre-existing one", decision.Inherited)
	}
}
