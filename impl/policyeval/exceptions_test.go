package policyeval_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

const exceptionPoliciesYAML = `
repository: market-mini
version: "1"
`

func exceptionViolationAt(constraint, file string, line int) policy.Violation {
	return policy.Violation{
		Constraint:  constraint,
		Enforcement: policy.EnforcementDeny,
		Msg:         "reaches the guest",
		Object:      policy.ObjectRef{Kind: policy.CodeGraphKind, Namespace: "default", Name: "market-mini"},
		Location:    &policy.Location{File: file, Line: line},
	}
}

func exceptionLibrary(t *testing.T, exceptions map[string]string) (policy.Library, error) {
	t.Helper()
	fsys := fstest.MapFS{"policies.yaml": {Data: []byte(exceptionPoliciesYAML)}}
	for name, data := range exceptions {
		fsys[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return policyeval.LoadFS(fsys)
}

func TestAnExceptionWithAnUnknownFieldIsRefused(t *testing.T) {
	_, err := exceptionLibrary(t, map[string]string{
		"exceptions/typo.yaml": "constraint: rfp-relay-only-guest-ssh\nkye: e8543cb5a9ecdcbc\nreason: a misspelt key\n",
	})
	if err == nil {
		t.Fatal("an exception with a misspelt key field loaded; it would waive every violation of its constraint")
	}
	if !strings.Contains(err.Error(), "kye") {
		t.Errorf("the error does not name the unknown field: %v", err)
	}
}

func TestAnExceptionWithoutAKeyIsRefused(t *testing.T) {
	_, err := exceptionLibrary(t, map[string]string{
		"exceptions/nokey.yaml": "constraint: rfp-relay-only-guest-ssh\nreason: accepted\n",
	})
	if err == nil {
		t.Fatal("an exception with no key and no scope loaded; it would waive every violation of its constraint")
	}
}

func TestARuleScopedExceptionWaivesEveryViolationOfItsConstraint(t *testing.T) {
	library, err := exceptionLibrary(t, map[string]string{
		"exceptions/rule.yaml": "constraint: rfp-relay-only-guest-ssh\nscope: rule\nreason: the constraint is accepted for this repository\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	waivers, _ := policyeval.Waivers(library, nil, time.Now())
	if len(waivers) != 1 {
		t.Fatalf("waivers = %+v, want one", waivers)
	}
	first := exceptionViolationAt("rfp-relay-only-guest-ssh", "lib/requester/mod.ts", 17)
	second := exceptionViolationAt("rfp-relay-only-guest-ssh", "test/integration_test.ts", 38)
	other := exceptionViolationAt("rfp-host-reach-in", "hono-bidder/mod.ts", 43)
	decision := policy.Decide(policy.Report{Violations: []policy.Violation{first, second, other}}, policy.RepositoryPolicy{}, waivers)
	if len(decision.Waived) != 2 {
		t.Fatalf("waived = %+v, want the two violations of the named constraint", decision.Waived)
	}
	if len(decision.Denied) != 1 || decision.Denied[0].Constraint != "rfp-host-reach-in" {
		t.Fatalf("denied = %+v, want the other constraint", decision.Denied)
	}
}

func TestAnUnknownScopeIsRefused(t *testing.T) {
	_, err := exceptionLibrary(t, map[string]string{
		"exceptions/odd.yaml": "constraint: rfp-relay-only-guest-ssh\nscope: repository\nreason: accepted\n",
	})
	if err == nil {
		t.Fatal("an exception with an unknown scope loaded")
	}
}

func TestAKeyThatMatchesNothingIsStale(t *testing.T) {
	exceptions := []policy.Exception{
		{Constraint: "rfp-relay-only-guest-ssh", Key: "aaaa", Reason: "the site is gone"},
		{Constraint: "rfp-host-reach-in", Key: policy.Key(exceptionViolationAt("rfp-host-reach-in", "hono-bidder/mod.ts", 43)), Reason: "still there"},
		{Constraint: "rfp-guest-reports-network", Scope: "rule", Reason: "rule-wide, never stale"},
	}
	violations := []policy.Violation{exceptionViolationAt("rfp-host-reach-in", "hono-bidder/mod.ts", 43)}
	stale := policy.StaleExceptions(exceptions, violations, time.Now())
	if len(stale) != 1 || stale[0].Key != "aaaa" {
		t.Fatalf("stale = %+v, want the key that matches nothing", stale)
	}
}
