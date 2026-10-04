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
	if result := ValidateRepository(repository); !result.OK() {
		t.Fatalf("a spec-only repository (no source) is refused: %v", result.Err())
	}
}

func TestValidateRepositorySourceAndPopulate(t *testing.T) {
	git := &Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "unseen"},
		Spec: RepositorySpec{
			Source:   &RepositorySource{Git: &GitSource{URL: "file:///tmp/unseen.git", Ref: "main"}},
			Populate: &RepositoryPopulate{Partition: PartitionPackage, Summarize: true, Agent: &AgentSpec{Kind: "scripted:/tmp/s.yaml"}},
		},
	}
	git.SetDefaults()
	if result := ValidateRepository(git); !result.OK() {
		t.Fatalf("a git source with populate is valid: %v", result.Err())
	}
	if git.Spec.Populate.Partition != PartitionPackage {
		t.Errorf("partition = %q", git.Spec.Populate.Partition)
	}
	if git.WorkPath() != "" || git.Source().Git.URL != "file:///tmp/unseen.git" {
		t.Errorf("source = %+v workPath = %q", git.Source(), git.WorkPath())
	}

	cases := map[string]*Repository{
		"both sources": {
			ObjectMeta: metav1.ObjectMeta{Name: "both"},
			Spec: RepositorySpec{
				Source: &RepositorySource{Path: "/src", Git: &GitSource{URL: "file:///tmp/x.git"}},
			},
		},
		"git without a url": {
			ObjectMeta: metav1.ObjectMeta{Name: "nourl"},
			Spec:       RepositorySpec{Source: &RepositorySource{Git: &GitSource{}}},
		},
		"unknown partition": {
			ObjectMeta: metav1.ObjectMeta{Name: "partition"},
			Spec: RepositorySpec{
				Path:     "/src",
				Populate: &RepositoryPopulate{Partition: "module"},
			},
		},
		"bad populate agent": {
			ObjectMeta: metav1.ObjectMeta{Name: "agent"},
			Spec: RepositorySpec{
				Path:     "/src",
				Populate: &RepositoryPopulate{Agent: &AgentSpec{Kind: "magic"}},
			},
		},
	}
	for name, repository := range cases {
		if ValidateRepository(repository).OK() {
			t.Errorf("%s: the repository was accepted", name)
		}
	}
}

func TestRepositoryHelpers(t *testing.T) {
	repository := &Repository{
		Spec: RepositorySpec{
			Source:   &RepositorySource{Git: &GitSource{URL: "file:///tmp/unseen.git"}},
			Populate: &RepositoryPopulate{Summarize: true, Agent: &AgentSpec{Kind: "claude"}},
		},
		Status: RepositoryStatus{ResolvedPath: "/cache/unseen"},
	}
	repository.SetDefaults()
	if repository.Partition() != PartitionDirectory {
		t.Errorf("a missing partition must default to directory, got %q", repository.Partition())
	}
	if !repository.Summarize() || repository.PopulateAgent().Kind != "claude" {
		t.Errorf("populate = %+v", repository.Spec.Populate)
	}
	if repository.WorkPath() != "/cache/unseen" {
		t.Errorf("workPath = %q, want the resolved path", repository.WorkPath())
	}
	repository.Status.ResolvedPath = ""
	if repository.WorkPath() != "/cache/unseen" && repository.WorkPath() != "" {
		t.Errorf("workPath = %q, want empty: a git source has no local path", repository.WorkPath())
	}
	plain := &Repository{Spec: RepositorySpec{Path: "/src/calc"}}
	if plain.WorkPath() != "/src/calc" || plain.Source().Path != "/src/calc" {
		t.Errorf("the phase 1 path field must read as a path source: %q %+v", plain.WorkPath(), plain.Source())
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
	for _, good := range []string{"self", "sc.calc", "up.calc", "sc.a-b-c", "ov.baopki", "orch.demo-deno-runtime", "sc.kind.denopod", "up.hono-pds"} {
		if !IsRef(good) {
			t.Fatalf("%q must be a ref", good)
		}
	}
	for _, bad := range []string{"", "calc", "sc.", "sc.Calc", "SC.calc", "zz.calc", "ov."} {
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
	for id, want := range map[string]string{
		"sc.kind.denopod":        "sc-kind-denopod",
		"type.DenoPermissions":   "type-denopermissions",
		"orch.demo-deno-runtime": "orch-demo-deno-runtime",
		"up.kcp":                 "up-kcp",
		"tb.openbao-root":        "tb-openbao-root",
		"sc.deno-kcp":            "sc-deno-kcp",
	} {
		if got := ArchName(id); got != want {
			t.Errorf("ArchName(%q) = %q, want %q", id, got, want)
		}
		if !IsArchID(id) {
			t.Errorf("%q must be an arch id", id)
		}
	}
	for id, want := range map[string]string{
		"type.DenoPermissions": "type-denopermissions",
		"tb.openbao-root":      "tb-openbao-root",
		"x.1":                  "x-1",
	} {
		if got := ArchName(id); got != want {
			t.Errorf("ArchName(%q) = %q, want %q", id, got, want)
		}
		if !IsArchID(id) {
			t.Errorf("%q must be an arch id", id)
		}
	}
	if IsArchID("self") || IsArchID("") || IsArchID("calc") {
		t.Fatal("self and a bare name are not arch ids")
	}
}

func TestValidateSystemContextArch(t *testing.T) {
	context := &SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "sc-kind-denopod"},
		Spec: SystemContextSpec{
			Repository:   "deno-kcp",
			Upstream:     "sc.deno-kcp",
			Overlay:      []string{"ov.kcp-local-config", "sc.kcp.workspaces"},
			Orchestrator: "orch.demo-deno-runtime",
			DependsOn:    []string{"sc.deno-kcp-provider"},
			Introduces:   []string{"sc.kind.denopod"},
			CodeRefs:     []string{"file:api/v1alpha1/types.go"},
			Arch: &ArchSpec{
				ID:           "sc.kind.denopod",
				Kind:         ArchKindNode,
				Section:      "system_contexts",
				Form:         "list",
				Parent:       "sc.deno-kcp",
				Slot:         "overlay[3]",
				Upstream:     "sc.deno-kcp",
				Overlay:      []string{"ov.kcp-local-config"},
				Orchestrator: "orch.demo-deno-runtime",
				DependsOn:    []string{"sc.deno-kcp-provider"},
				Code:         []string{"file:api/v1alpha1/types.go"},
				Node:         map[string]any{"id": "sc.kind.denopod"},
			},
		},
	}
	context.SetDefaults()
	if result := ValidateSystemContext(context); !result.OK() {
		t.Fatalf("an imported context must validate: %v", result.Err())
	}

	bad := *context
	badArch := *context.Spec.Arch
	badArch.Kind = "banana"
	bad.Spec.Arch = &badArch
	if result := ValidateSystemContext(&bad); result.OK() {
		t.Fatal("an unknown arch kind must fail")
	}

	badOverlay := *context
	badOverlay.Spec.Overlay = []string{"up.kcp"}
	if result := ValidateSystemContext(&badOverlay); result.OK() {
		t.Fatal("an overlay that is not sc. or ov. must fail")
	}

	document := &SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "arch-document-deno-kcp"},
		Spec: SystemContextSpec{
			Repository: "deno-kcp",
			Arch: &ArchSpec{
				ID:       "document",
				Kind:     ArchKindDocument,
				Document: map[string]any{"kind": "OpenArchitecture"},
				Sections: []ArchSection{{Key: "system_contexts", Form: "list"}},
			},
		},
	}
	document.SetDefaults()
	if result := ValidateSystemContext(document); !result.OK() {
		t.Fatalf("the document object must validate: %v", result.Err())
	}
}

func TestNextChangeName(t *testing.T) {
	base := ChangeNameCodeToSpec("calc", "aaaa", "bbbb")
	if got := NextChangeName(nil, base); got != base {
		t.Errorf("first attempt = %q, want the bare name %q", got, base)
	}
	if got := NextChangeName([]string{"other"}, base); got != base {
		t.Errorf("an unrelated change does not take the name: %q", got)
	}
	first := NextChangeName([]string{base}, base)
	if first != base+"-a2" {
		t.Errorf("second attempt = %q, want %q", first, base+"-a2")
	}
	third := NextChangeName([]string{base, first}, base)
	if third != base+"-a3" {
		t.Errorf("third attempt = %q, want %q", third, base+"-a3")
	}
	other := ChangeNameCodeToSpec("calc", "aaaa", "cccc")
	if got := NextChangeName([]string{base, first}, other); got != other {
		t.Errorf("another episode = %q, want %q", got, other)
	}
}

func TestValidateSpecChangeAllowsATreeWithoutCommits(t *testing.T) {
	without := &SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-none-none"},
		Spec:       SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionCodeToSpec},
	}
	without.SetDefaults()
	if result := ValidateSpecChange(without); !result.OK() {
		t.Errorf("an empty commit pair must be allowed: %v", result.Err())
	}
	half := &SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-c2s-none-c1"},
		Spec:       SpecChangeSpec{SystemContext: "calc", Direction: specapi.DirectionCodeToSpec, ToCommit: "c1"},
	}
	half.SetDefaults()
	if ValidateSpecChange(half).OK() {
		t.Error("half a commit pair was accepted")
	}
}
