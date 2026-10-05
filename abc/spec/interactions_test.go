package spec_test

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func marketSpec() spec.SystemContextSpec {
	return spec.SystemContextSpec{
		Repository: "greenfield-market",
		Upstream:   spec.RefSelf,
		Intent:     "A market guest that reports its own network information.",
		Requirements: []spec.Requirement{
			{ID: "r.report", Level: spec.LevelMust, Text: "The guest reports its own network information."},
		},
		Interactions: []spec.Interaction{
			{ID: "i.report", Peer: "host", Initiator: spec.InitiatorSelf, Channel: "relay", Carries: []string{"network-info"}, Purpose: "network-discovery", Level: spec.LevelMust},
		},
	}
}

func TestInteractionsAreInTheSpecHash(t *testing.T) {
	base := marketSpec()
	hash, err := spec.HashSystemContextSpec(base)
	if err != nil {
		t.Fatal(err)
	}
	edited := marketSpec()
	edited.Interactions[0].Purpose = "telemetry"
	moved, err := spec.HashSystemContextSpec(edited)
	if err != nil {
		t.Fatal(err)
	}
	if hash == moved {
		t.Fatal("a change to the interactions left the spec hash alone; it is not a real spec change")
	}
	levelOnly := marketSpec()
	levelOnly.Interactions[0].Level = spec.LevelShould
	levelHash, err := spec.HashSystemContextSpec(levelOnly)
	if err != nil {
		t.Fatal(err)
	}
	if moved == levelHash {
		t.Fatal("two different interaction edits hashed the same")
	}
}

func TestAnUnsetInteractionLevelCanonicalizesToShould(t *testing.T) {
	unset := marketSpec()
	unset.Interactions[0].Level = ""
	explicit := marketSpec()
	explicit.Interactions[0].Level = spec.LevelShould
	unsetHash, err := spec.HashSystemContextSpec(unset)
	if err != nil {
		t.Fatal(err)
	}
	explicitHash, err := spec.HashSystemContextSpec(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if unsetHash != explicitHash {
		t.Errorf("an unset level and SHOULD must hash the same:\n%q\n%q", unsetHash, explicitHash)
	}
}

func TestCanonicalInteractionsSortByIDAndCarries(t *testing.T) {
	context := marketSpec()
	context.Interactions = append(context.Interactions, spec.Interaction{
		ID: "i.no-reach-in", Peer: "host", Initiator: spec.InitiatorPeer,
		Carries: []string{"network-info", "nodeId"}, Forbidden: true,
	})
	out := spec.Canonicalize(context)
	if out.Interactions[0].ID != "i.no-reach-in" || out.Interactions[1].ID != "i.report" {
		t.Fatalf("interactions = %+v, want them keyed by id", out.Interactions)
	}
	if !reflect.DeepEqual(out.Interactions[0].Carries, []string{"network-info", "nodeId"}) {
		t.Errorf("carries = %v", out.Interactions[0].Carries)
	}
	if !out.Interactions[0].Forbidden {
		t.Error("the forbidden marker was dropped")
	}
	if out.Interactions[1].Level != spec.LevelMust {
		t.Errorf("level = %q, want MUST untouched", out.Interactions[1].Level)
	}
}

func TestValidateSystemContextChecksInteractions(t *testing.T) {
	cases := []struct {
		name        string
		interaction spec.Interaction
		path        string
	}{
		{"no id", spec.Interaction{Peer: "host", Initiator: spec.InitiatorSelf}, "spec.interactions[0].id"},
		{"no peer", spec.Interaction{ID: "i.x", Initiator: spec.InitiatorSelf}, "spec.interactions[0].peer"},
		{"bad initiator", spec.Interaction{ID: "i.x", Peer: "host", Initiator: "them"}, "spec.interactions[0].initiator"},
		{"bad level", spec.Interaction{ID: "i.x", Peer: "host", Initiator: spec.InitiatorSelf, Level: "MUST_NOT"}, "spec.interactions[0].level"},
		{"empty carry", spec.Interaction{ID: "i.x", Peer: "host", Initiator: spec.InitiatorSelf, Carries: []string{" "}}, "spec.interactions[0].carries[0]"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			context := &spec.SystemContext{
				ObjectMeta: metav1.ObjectMeta{Name: "market", Namespace: "default"},
				Spec: spec.SystemContextSpec{
					Repository:   "greenfield-market",
					Upstream:     spec.RefSelf,
					Interactions: []spec.Interaction{testCase.interaction},
				},
			}
			result := spec.ValidateSystemContext(context)
			if result.OK() {
				t.Fatal("the validator accepted the interaction")
			}
			found := false
			for _, problem := range result.Problems {
				if problem.Path == testCase.path {
					found = true
				}
			}
			if !found {
				t.Errorf("problems = %+v, want one at %s", result.Problems, testCase.path)
			}
		})
	}
}

func TestValidateSystemContextRefusesADuplicateInteractionID(t *testing.T) {
	context := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "market", Namespace: "default"},
		Spec: spec.SystemContextSpec{
			Repository: "greenfield-market",
			Upstream:   spec.RefSelf,
			Interactions: []spec.Interaction{
				{ID: "i.x", Peer: "host", Initiator: spec.InitiatorSelf},
				{ID: "i.x", Peer: "requester", Initiator: spec.InitiatorPeer},
			},
		},
	}
	result := spec.ValidateSystemContext(context)
	if result.OK() {
		t.Fatal("a duplicate interaction id was accepted")
	}
}

func TestValidateSystemContextAcceptsAForbiddenFlow(t *testing.T) {
	context := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "market", Namespace: "default"},
		Spec: spec.SystemContextSpec{
			Repository: "greenfield-market",
			Upstream:   spec.RefSelf,
			Interactions: []spec.Interaction{
				{ID: "i.no-reach-in", Peer: "host", Initiator: spec.InitiatorPeer, Level: spec.LevelMust, Forbidden: true},
			},
		},
	}
	if result := spec.ValidateSystemContext(context); !result.OK() {
		t.Fatalf("a declared must-never flow was refused: %v", result.Err())
	}
}

func TestInteractionDeltaIsARealChange(t *testing.T) {
	base := marketSpec()
	edited := marketSpec()
	edited.Interactions = append(edited.Interactions, spec.Interaction{
		ID: "i.no-reach-in", Peer: "host", Initiator: spec.InitiatorPeer,
		Channel: "relay", Purpose: "network-discovery", Level: spec.LevelMust, Forbidden: true,
	})
	change := delta.Diff(base, edited)
	if change.Empty() {
		t.Fatal("an added interaction is not an empty delta")
	}
	if len(change.Interactions) != 1 || change.Interactions[0].Op != spec.OpAdded || change.Interactions[0].ID != "i.no-reach-in" {
		t.Fatalf("interactions = %+v", change.Interactions)
	}
	if counts := change.Count(); counts.Added != 1 {
		t.Errorf("counts = %+v, want one added", counts)
	}
	if got := delta.Apply(base, change); !reflect.DeepEqual(spec.Canonicalize(got).Interactions, spec.Canonicalize(edited).Interactions) {
		t.Errorf("apply did not reproduce the interactions:\n%+v", got.Interactions)
	}
	if removed := delta.RemovedInteractionIDs(delta.Diff(edited, base)); !reflect.DeepEqual(removed, []string{"i.no-reach-in"}) {
		t.Errorf("removed = %v, want the deleted id named", removed)
	}
}
