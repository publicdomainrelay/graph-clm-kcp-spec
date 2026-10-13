package main

import (
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func TestTheOwnerOfAPathIsItsDeepestMatch(t *testing.T) {
	contexts := []observedContext{
		{name: "hydradb", files: []string{"cmd", "impl"}},
		{name: "cmd-specctl", files: []string{"cmd/specctl/main.go", "cmd/specctl/owner.go"}},
		{name: "impl-clm", files: []string{"impl/clm/clm.go"}},
	}
	for _, testCase := range []struct {
		path string
		want string
	}{
		{"cmd/specctl/owner.go", "cmd-specctl"},
		{"cmd/specctl", "cmd-specctl"},
		{"cmd/other/main.go", "hydradb"},
		{"docs/plan.md", ""},
		{"", ""},
	} {
		if got := ownerOfPath(testCase.path, contexts); got != testCase.want {
			t.Errorf("ownerOfPath(%q) = %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

func TestTheSmallestOfTheEquallyDeepContextsOwnsThePath(t *testing.T) {
	contexts := []observedContext{
		{name: "calc", files: []string{"calc/calc.go", "calc/mul.go"}},
		{name: "calc-test", files: []string{"calc/calc.go"}},
	}
	if got := ownerOfPath("calc/calc.go", contexts); got != "calc-test" {
		t.Errorf("ownerOfPath = %q, want the smallest context calc-test", got)
	}
}

func TestAPathIsReadAsTheCheckoutHoldsIt(t *testing.T) {
	if got := checkoutPath("./calc/calc.go"); got != "calc/calc.go" {
		t.Errorf("checkoutPath = %q, want calc/calc.go", got)
	}
	if got := checkoutPath("."); got != "" {
		t.Errorf("checkoutPath(.) = %q, want none", got)
	}
}

func TestAPathInsideTheCheckoutIsReadRelativeToIt(t *testing.T) {
	repository := &spec.Repository{Spec: spec.RepositorySpec{Path: "/tmp/tree"}}
	if got := relativeToCheckout(repository, "/tmp/tree/calc/calc.go"); got != "calc/calc.go" {
		t.Errorf("relativeToCheckout = %q, want calc/calc.go", got)
	}
	if got := relativeToCheckout(repository, "calc/calc.go"); got != "calc/calc.go" {
		t.Errorf("relativeToCheckout = %q, want the path as given", got)
	}
	if got := relativeToCheckout(repository, "/elsewhere/calc.go"); got != "" {
		t.Errorf("relativeToCheckout = %q, want none: the path is outside the checkout", got)
	}
}
