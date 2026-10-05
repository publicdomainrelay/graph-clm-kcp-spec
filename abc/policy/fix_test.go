package policy_test

import (
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const fixHash = "2f7d1c" + "0000000000000000000000000000000000000000000000000000000000"

func TestAFixChangeTargetsTheContextAndCarriesThePrompt(t *testing.T) {
	request := policy.FixRequest{
		Repository: "market-mini",
		Constraint: "relay-only-ssh",
		Site:       "lib/requester/mod.ts:17",
		Message:    "ssh from requester reaches the guest directly",
		Prompt:     "A policy violation must be fixed in market-mini.",
	}
	change := policy.FixChange(request, spec.ChangeNameSpecToCode("market-mini", fixHash), "market-mini", fixHash)
	if result := spec.ValidateSpecChange(change); !result.OK() {
		t.Fatalf("the change is invalid: %v", result.Err())
	}
	if change.Spec.SystemContext != "market-mini" || change.Spec.Direction != "SpecToCode" {
		t.Fatalf("the change does not target the context: %+v", change.Spec)
	}
	if change.Annotations[policy.AnnotationFixPrompt] != request.Prompt {
		t.Errorf("the prompt rides on the change: %v", change.Annotations)
	}
	if change.Annotations[policy.AnnotationFixConstraint] != request.Constraint ||
		change.Annotations[policy.AnnotationFixSite] != request.Site {
		t.Errorf("the change does not name the violation: %v", change.Annotations)
	}
	if !strings.Contains(change.Name, "market-mini-s2c-") {
		t.Errorf("change name = %q", change.Name)
	}
}
