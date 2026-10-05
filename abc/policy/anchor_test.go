package policy_test

import (
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func anchorGraph() policy.CodeGraph {
	return policy.CodeGraph{Spec: policy.CodeGraphSpec{
		Nodes: []policy.CodeGraphNode{
			{ID: "function:aaa", Kind: "function", Name: "createVmBidderCallbacks", QualifiedName: "createVmBidderCallbacks", File: "lib/market-bidder-compute/mod.ts", StartLine: 240, EndLine: 330},
			{ID: "function:bbb", Kind: "function", Name: "other", QualifiedName: "other", File: "lib/market-bidder-compute/mod.ts", StartLine: 400, EndLine: 420},
		},
	}}
}

func emissionAt(line int) policy.Violation {
	return policy.Violation{
		Constraint:  "guest-report-driven-onnetwork",
		Enforcement: policy.EnforcementDeny,
		Msg:         "createVmBidderCallbacks emits the vm.onNetwork event from the provisioning lifecycle",
		Object:      policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "atproto-market"},
		Location:    &policy.Location{File: "lib/market-bidder-compute/mod.ts", Line: line},
		Details: map[string]any{"effect": map[string]any{
			"kind":  "event.emit",
			"attrs": map[string]any{"type": "com.publicdomainrelay.temp.compute.events.vm.onNetwork"},
		}},
	}
}

func TestAnAnchorNamesTheDeclarationAndNotTheLine(t *testing.T) {
	graph := anchorGraph()
	before := policy.Anchor(emissionAt(257), graph)
	after := policy.Anchor(emissionAt(312), graph)

	if before.Location.Declaration != "createVmBidderCallbacks" {
		t.Fatalf("declaration = %q, want the enclosing function", before.Location.Declaration)
	}
	if before.Location.Kind != "event.emit" || before.Location.Attrs != "type=com.publicdomainrelay.temp.compute.events.vm.onNetwork" {
		t.Fatalf("effect site = %q %q, want the effect's kind and attributes", before.Location.Kind, before.Location.Attrs)
	}
	if policy.Key(before) != policy.Key(after) {
		t.Errorf("an edit that moved the emission inside the same declaration changed the key:\n before %s (line %d)\n after  %s (line %d)",
			policy.Key(before), before.Location.Line, policy.Key(after), after.Location.Line)
	}
	if policy.Key(before) == policy.Key(policy.Anchor(emissionAt(410), graph)) {
		t.Error("a violation in another declaration shares the key")
	}
}

func TestAWaiverWrittenFromAnAnchoredKeySurvivesTheEdit(t *testing.T) {
	graph := anchorGraph()
	base := policy.Anchor(emissionAt(257), graph)
	head := policy.Anchor(emissionAt(312), graph)

	waiver := policy.Override{Key: policy.Key(base), Reason: "the IP may be a public IPv4 the client judges", By: "john"}
	decision := policy.Decide(policy.Report{Violations: []policy.Violation{head}}, policy.RepositoryPolicy{}, []policy.Override{waiver})
	if decision.Blocked || len(decision.Waived) != 1 {
		t.Fatalf("the waiver did not follow the violation to its new line: %+v", decision)
	}
}

func declaredReachIn(channel, purpose string) policy.Violation {
	return policy.Violation{
		Constraint:  "rfp-host-reach-in",
		Enforcement: policy.EnforcementDeny,
		Msg:         "the host reaches into the guest: host -> guest (initiator host, channel " + channel + ", purpose " + purpose + ", carries [network-info])",
		Object:      policy.ObjectRef{Kind: policy.ArchitectureKind, Name: "market-mini"},
		Location:    nil,
		Details: map[string]any{
			"from":      "host",
			"to":        "guest",
			"initiator": "host",
			"channel":   channel,
			"purpose":   purpose,
			"carries":   []any{"network-info"},
			"source":    "declared",
			"evidence":  []any{},
		},
	}
}

func TestASitelessViolationKeysOnItsClauseAndNotOnNothing(t *testing.T) {
	reachIn := declaredReachIn("direct", "network-discovery")
	other := declaredReachIn("relay", "provision")
	if policy.Key(reachIn) == policy.Key(other) {
		t.Error("two declared violations without a site share a key")
	}
	same := declaredReachIn("direct", "network-discovery")
	if policy.Key(reachIn) != policy.Key(same) {
		t.Errorf("the same declared violation keys on %s then %s", policy.Key(reachIn), policy.Key(same))
	}
	if policy.Key(reachIn) == policy.Key(emissionAt(257)) {
		t.Error("a siteless violation shares a key with an unrelated one")
	}
}

func TestAnUnanchoredViolationStillKeysOnItsLine(t *testing.T) {
	graph := anchorGraph()
	outside := policy.Violation{
		Constraint:  "relay-only-ssh",
		Enforcement: policy.EnforcementDeny,
		Object:      policy.ObjectRef{Kind: policy.CodeGraphKind, Name: "atproto-market"},
		Location:    &policy.Location{File: "test/integration_test.ts", Line: 17},
	}
	anchored := policy.Anchor(outside, graph)
	if policy.Anchored(anchored.Location) {
		t.Fatalf("a location with no enclosing declaration was anchored: %+v", anchored.Location)
	}
	moved := outside
	moved.Location = &policy.Location{File: "test/integration_test.ts", Line: 18}
	if policy.Key(anchored) == policy.Key(moved) {
		t.Error("two unanchored violations on different lines share a key")
	}
}
