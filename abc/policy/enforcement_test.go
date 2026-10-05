package policy_test

import (
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func TestApplyEnforcementMatchesByNameAndLastRuleWins(t *testing.T) {
	library := policy.Library{
		Manifest: policy.PolicyLibrary{
			Enforcement: []policy.EnforcementRule{
				{Name: "*", Action: policy.EnforcementWarn},
				{Name: "provisioning-*", Action: policy.EnforcementDeny},
				{Name: "relay-only-*", Action: policy.EnforcementDryRun},
			},
		},
		Constraints: []policy.Constraint{
			{Name: "security-disabled-verification", Enforcement: policy.EnforcementDeny},
			{Name: "provisioning-container-in-test", Enforcement: policy.EnforcementWarn},
			{Name: "relay-only-ssh", Enforcement: policy.EnforcementDeny},
		},
	}
	library.ApplyEnforcement()
	want := map[string]policy.Enforcement{
		"security-disabled-verification": policy.EnforcementWarn,
		"provisioning-container-in-test": policy.EnforcementDeny,
		"relay-only-ssh":                 policy.EnforcementDryRun,
	}
	byName := map[string]policy.Constraint{}
	for _, constraint := range library.Constraints {
		byName[constraint.Name] = constraint
	}
	for name, action := range want {
		if got := byName[name].Enforcement; got != action {
			t.Errorf("%s: got %q, want %q", name, got, action)
		}
	}
}

func TestApplyEnforcementIgnoresAnUnknownAction(t *testing.T) {
	library := policy.Library{
		Manifest: policy.PolicyLibrary{
			Enforcement: []policy.EnforcementRule{{Name: "*", Action: policy.Enforcement("block")}},
		},
		Constraints: []policy.Constraint{{Name: "a", Enforcement: policy.EnforcementDeny}},
	}
	library.ApplyEnforcement()
	if got := library.Constraints[0].Enforcement; got != policy.EnforcementDeny {
		t.Errorf("an unknown action changed the constraint: got %q", got)
	}
}
