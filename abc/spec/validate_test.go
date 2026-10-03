package spec

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func validContext() *SystemContext {
	context := &SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: "default"},
		Spec: SystemContextSpec{
			Repository: "calc",
			Upstream:   RefSelf,
			Intent:     "Add two integers.",
			Requirements: []Requirement{
				{ID: "r.add-two-ints", Level: LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"file:calc/calc.go"}},
				{ID: "r.subtract-two-ints", Level: LevelShould, Text: "Subtract returns the difference."},
				{ID: "r.reject-overflow", Level: LevelMay, Text: "Operations may report overflow."},
			},
			Interfaces: []Interface{
				{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"},
			},
			CodeRefs: []string{"file:calc/calc.go", "function:calc.Add"},
		},
	}
	context.SetDefaults()
	return context
}

func TestValidSystemContextPasses(t *testing.T) {
	context := validContext()
	result := ValidateSystemContext(context)
	if !result.OK() {
		t.Fatalf("problems = %v", result.Err())
	}
	if !specapi.IsHash(result.SpecHash) {
		t.Fatalf("spec hash = %q, want a sha256 hex digest", result.SpecHash)
	}
	if again := ValidateSystemContext(validContext()); again.SpecHash != result.SpecHash {
		t.Fatalf("hash is not stable: %q then %q", result.SpecHash, again.SpecHash)
	}
	changed := validContext()
	changed.Spec.Intent = "Something else."
	if other := ValidateSystemContext(changed); other.SpecHash == result.SpecHash {
		t.Fatal("a changed spec must change the hash")
	}
}

func TestSetDefaultsFillsIdentity(t *testing.T) {
	context := &SystemContext{ObjectMeta: metav1.ObjectMeta{Name: "calc"}}
	context.SetDefaults()
	if context.APIVersion != specapi.APIVersion || context.Kind != specapi.SystemContextKind {
		t.Fatalf("type meta = %s %s", context.APIVersion, context.Kind)
	}
	if context.Namespace != specapi.DefaultNamespace {
		t.Fatalf("namespace = %q", context.Namespace)
	}
	if context.Spec.Upstream != RefSelf {
		t.Fatalf("upstream = %q", context.Spec.Upstream)
	}
	repository := &Repository{ObjectMeta: metav1.ObjectMeta{Name: "calc"}}
	repository.SetDefaults()
	if repository.Spec.Branch != "main" {
		t.Fatalf("branch = %q", repository.Spec.Branch)
	}
	change := &SpecChange{ObjectMeta: metav1.ObjectMeta{Name: "change"}}
	change.SetDefaults()
	if change.Status.Phase != specapi.PhasePending {
		t.Fatalf("phase = %q", change.Status.Phase)
	}
}

func TestInvalidSystemContextsAreRejected(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SystemContext)
		want   string
	}{
		{"no name", func(c *SystemContext) { c.Name = "" }, "metadata.name"},
		{"bad name", func(c *SystemContext) { c.Name = "Not_A_Name" }, "DNS-1123"},
		{"no repository", func(c *SystemContext) { c.Spec.Repository = "" }, "spec.repository"},
		{"bad upstream", func(c *SystemContext) { c.Spec.Upstream = "xx.calc" }, "spec.upstream"},
		{"bad overlay", func(c *SystemContext) { c.Spec.Overlay = []string{"up.calc"} }, "spec.overlay[0]"},
		{"bad orchestrator", func(c *SystemContext) { c.Spec.Orchestrator = "calc" }, "spec.orchestrator"},
		{"duplicate requirement id", func(c *SystemContext) {
			c.Spec.Requirements[1].ID = c.Spec.Requirements[0].ID
		}, "not unique"},
		{"empty requirement id", func(c *SystemContext) { c.Spec.Requirements[0].ID = "" }, "spec.requirements[0].id"},
		{"bad level", func(c *SystemContext) { c.Spec.Requirements[0].Level = "SHALL" }, "is not MUST, SHOULD or MAY"},
		{"empty requirement text", func(c *SystemContext) { c.Spec.Requirements[0].Text = "" }, "spec.requirements[0].text"},
		{"bad code ref", func(c *SystemContext) { c.Spec.CodeRefs = []string{"calc/calc.go"} }, "spec.codeRefs[0]"},
		{"duplicate code ref", func(c *SystemContext) {
			c.Spec.CodeRefs = []string{"file:calc/calc.go", "file:calc/calc.go"}
		}, "spec.codeRefs[1]"},
		{"empty interface name", func(c *SystemContext) { c.Spec.Interfaces[0].Name = "" }, "spec.interfaces[0].name"},
		{"duplicate interface", func(c *SystemContext) {
			c.Spec.Interfaces = append(c.Spec.Interfaces, Interface{Name: "Add"})
		}, "spec.interfaces[1].name"},
		{"bad realized hash", func(c *SystemContext) { c.Status.RealizedSpecHash = "deadbeef" }, "status.realizedSpecHash"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			context := validContext()
			testCase.mutate(context)
			result := ValidateSystemContext(context)
			if result.OK() {
				t.Fatal("expected problems, got none")
			}
			if !strings.Contains(result.Err().Error(), testCase.want) {
				t.Fatalf("problems = %v, want one naming %s", result.Err(), testCase.want)
			}
			if result.SpecHash != "" {
				t.Fatalf("an invalid spec must not carry a hash, got %q", result.SpecHash)
			}
		})
	}
}

func TestNilObjectsAreRejected(t *testing.T) {
	if ValidateSystemContext(nil).OK() {
		t.Fatal("a nil system context is invalid")
	}
	if ValidateRepository(nil).OK() {
		t.Fatal("a nil repository is invalid")
	}
	if ValidateSpecChange(nil).OK() {
		t.Fatal("a nil spec change is invalid")
	}
	if ValidateAny(struct{}{}).OK() {
		t.Fatal("an unknown kind is invalid")
	}
}

func TestValidateRepository(t *testing.T) {
	repository := &Repository{ObjectMeta: metav1.ObjectMeta{Name: "calc"}, Spec: RepositorySpec{Path: "/src/calc"}}
	repository.SetDefaults()
	if result := ValidateRepository(repository); !result.OK() {
		t.Fatalf("problems = %v", result.Err())
	}
	repository.Spec.Path = ""
	if ValidateRepository(repository).OK() {
		t.Fatal("a path is required")
	}
}

func TestValidateSpecChange(t *testing.T) {
	change := &SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-to-code"},
		Spec:       SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionSpecToCode, ToSpecHash: strings.Repeat("a", 64)},
	}
	change.SetDefaults()
	if result := ValidateSpecChange(change); !result.OK() {
		t.Fatalf("problems = %v", result.Err())
	}

	badDirection := &SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       SpecChangeSpec{SystemContext: "calc", Direction: "Sideways"},
	}
	badDirection.SetDefaults()
	if ValidateSpecChange(badDirection).OK() {
		t.Fatal("an unknown direction is invalid")
	}

	missingCommit := &SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, FromCommit: "aaa"},
	}
	missingCommit.SetDefaults()
	if ValidateSpecChange(missingCommit).OK() {
		t.Fatal("CodeToSpec without toCommit is invalid")
	}

	badPhase := &SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc"},
		Spec:       SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionSpecToCode, ToSpecHash: strings.Repeat("b", 64)},
		Status:     SpecChangeStatus{Phase: "Almost"},
	}
	if ValidateSpecChange(badPhase).OK() {
		t.Fatal("an unknown phase is invalid")
	}
}

func TestIsRefAndIsCodeRef(t *testing.T) {
	for _, good := range []string{"self", "sc.calc", "up.calc", "sc.a-b-c"} {
		if !IsRef(good) {
			t.Fatalf("%q must be a ref", good)
		}
	}
	for _, bad := range []string{"", "calc", "sc.", "sc.Calc", "SC.calc", "up.calc.extra"} {
		if IsRef(bad) {
			t.Fatalf("%q must not be a ref", bad)
		}
	}
	for _, good := range []string{"file:calc/calc.go", "function:calc.Add", "method:T.M", "type:T", "package:example.com/x"} {
		if !IsCodeRef(good) {
			t.Fatalf("%q must be a code ref", good)
		}
	}
	for _, bad := range []string{"", "calc.go", "file:", "file: ", "fn:calc.Add"} {
		if IsCodeRef(bad) {
			t.Fatalf("%q must not be a code ref", bad)
		}
	}
	if name, ok := RefName("sc.calc"); !ok || name != "calc" {
		t.Fatalf("RefName = %q %v", name, ok)
	}
	if _, ok := RefName("self"); ok {
		t.Fatal("self has no ref name")
	}
}
