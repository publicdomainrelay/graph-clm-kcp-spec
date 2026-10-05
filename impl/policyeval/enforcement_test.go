package policyeval_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

func constraintEnforcement(t *testing.T, library policy.Library, name string) policy.Enforcement {
	t.Helper()
	constraint, ok := library.Constraint(name)
	if !ok {
		t.Fatalf("the library holds no constraint %s: %v", name, library.Imported)
	}
	return constraint.Enforcement
}

// The manifest's enforcement rules are what scripts/example-pr.sh writes for
// POLICY_ENFORCEMENT: they must reach the pack's constraints, which no file in
// the repository holds, as well as the repository's own.
func TestEnforcementRulesReachTheImportedPack(t *testing.T) {
	dir := filepath.Join(root, "examples", "policies", "deno-kcp")
	raw, err := policyeval.LoadRaw(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	unoverridden, _, err := policyeval.ResolveImports(raw, policyeval.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := constraintEnforcement(t, unoverridden, "rfp-host-reach-in"); got != policy.EnforcementDeny {
		t.Fatalf("the pack ships deny, got %q", got)
	}
	raw.Manifest.Enforcement = []policy.EnforcementRule{
		{Name: "*", Action: policy.EnforcementWarn},
		{Name: "provisioning-*", Action: policy.EnforcementDeny},
		{Name: "security-disabled-verification", Action: policy.EnforcementDeny},
	}
	resolved, _, err := policyeval.ResolveImports(raw, policyeval.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ImportedFrom("rfp-host-reach-in") == "" {
		t.Errorf("the pack's constraint is not marked imported: %v", resolved.Imported)
	}
	for name, want := range map[string]policy.Enforcement{
		"rfp-host-reach-in":              policy.EnforcementWarn,
		"provisioning-container-in-test": policy.EnforcementDeny,
		"relay-only-ssh":                 policy.EnforcementWarn,
		"security-disabled-verification": policy.EnforcementDeny,
	} {
		if got := constraintEnforcement(t, resolved, name); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	// A branch's library is resolved when it is read and resolved again when it
	// is built; the second pass must accept the pack's own constraints, whose
	// action the first pass rewrote.
	if _, _, err := policyeval.ResolveImports(resolved, policyeval.ImportOptions{}); err != nil {
		t.Fatalf("a second resolution of an already-resolved library: %v", err)
	}
}

func TestEnforcementRuleWithAnUnknownActionFailsTheLoad(t *testing.T) {
	dir := t.TempDir()
	manifest := "repository: x\nversion: \"1\"\nenforcement:\n- {name: '*', action: block}\n"
	if err := os.WriteFile(filepath.Join(dir, policy.PoliciesPath), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := policyeval.Load(dir); err == nil {
		t.Fatal("an unknown enforcement action was accepted")
	}
}
