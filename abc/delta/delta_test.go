package delta_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func calcSpec() spec.SystemContextSpec {
	return spec.SystemContextSpec{
		Repository: "calc",
		Upstream:   spec.RefSelf,
		Intent:     "Arithmetic on two integers.",
		Requirements: []spec.Requirement{
			{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum.", CodeRefs: []string{"file:calc/calc.go"}},
			{ID: "r.multiply", Level: spec.LevelMust, Text: "Multiply returns the product.", CodeRefs: []string{"function:Multiply"}},
		},
		Interfaces: []spec.Interface{
			{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"},
			{Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go"},
		},
		CodeRefs: []string{"file:calc/calc.go"},
	}
}

func subtractSpec() spec.SystemContextSpec {
	next := calcSpec()
	next.Requirements = append(next.Requirements, spec.Requirement{
		ID:       "r.subtract",
		Level:    spec.LevelShould,
		Text:     "Subtract returns the difference.",
		CodeRefs: []string{"function:Subtract"},
	})
	next.Interfaces = append(next.Interfaces, spec.Interface{
		Name: "Subtract", Kind: "function", Signature: "func Subtract(a, b int) int", File: "calc/calc.go",
	})
	next.CodeRefs = append(next.CodeRefs, "function:Subtract")
	return next
}

func calcObserved() spec.ObservedFacts {
	return spec.ObservedFacts{
		Files: []string{"calc/calc.go", "calc/calc_test.go"},
		Interfaces: []spec.ObservedInterface{
			{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go", Line: 3, CodegraphID: "function:aa11"},
			{Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go", Line: 8, CodegraphID: "function:bb22"},
		},
		Fingerprint: "1111111111111111111111111111111111111111111111111111111111111111",
	}
}

func subtractObserved() spec.ObservedFacts {
	next := calcObserved()
	next.Interfaces = append(next.Interfaces, spec.ObservedInterface{
		Name: "Subtract", Kind: "function", Signature: "func Subtract(a, b int) int", File: "calc/calc.go", Line: 13, CodegraphID: "function:cc33",
	})
	next.Fingerprint = "2222222222222222222222222222222222222222222222222222222222222222"
	return next
}

// methodSpec describes a store whose two types both offer a method named List.
// The keys carry the receiver, which is what lets a delta tell one List from
// the other; a surface keyed by bare name could not.
func methodSpec() spec.SystemContextSpec {
	return spec.SystemContextSpec{
		Repository: "store",
		Upstream:   spec.RefSelf,
		Intent:     "A key store indexed two ways.",
		Requirements: []spec.Requirement{
			{ID: "r.list", Level: spec.LevelMust, Text: "List returns the keys.", CodeRefs: []string{"function:Indexer.List"}},
		},
		Interfaces: []spec.Interface{
			{Name: "Indexer.List", Kind: "method", Signature: "() []string", File: "store/store.go"},
			{Name: "Set.List", Kind: "method", Signature: "() []string", File: "store/store.go"},
		},
		CodeRefs: []string{"file:store/store.go"},
	}
}

func methodSpecNext() spec.SystemContextSpec {
	next := methodSpec()
	next.Interfaces = []spec.Interface{
		{Name: "Indexer.List", Kind: "method", Signature: "(limit int) []string", File: "store/store.go"},
		{Name: "Set.List", Kind: "method", Signature: "() []string", File: "store/store.go"},
		{Name: "Set.Add", Kind: "method", Signature: "(item string)", File: "store/store.go"},
	}
	return next
}

func methodObserved() spec.ObservedFacts {
	return spec.ObservedFacts{
		Files: []string{"store/store.go"},
		Interfaces: []spec.ObservedInterface{
			{Name: "Indexer.List", Kind: "method", Signature: "() []string", File: "store/store.go", Line: 5, CodegraphID: "method:ii11"},
			{Name: "Set.List", Kind: "method", Signature: "() []string", File: "store/store.go", Line: 15, CodegraphID: "method:ss22"},
		},
		Fingerprint: "3333333333333333333333333333333333333333333333333333333333333333",
	}
}

func methodObservedNext() spec.ObservedFacts {
	next := methodObserved()
	next.Interfaces = []spec.ObservedInterface{
		{Name: "Set.List", Kind: "method", Signature: "() []string", File: "store/store.go", Line: 15, CodegraphID: "method:ss22"},
		{Name: "Set.Add", Kind: "method", Signature: "(item string)", File: "store/store.go", Line: 22, CodegraphID: "method:aa44"},
	}
	next.Fingerprint = "4444444444444444444444444444444444444444444444444444444444444444"
	return next
}

// TestDeltaGolden pins the JSON form. It is the shape phase 8 mirrors in
// TypeScript, so a change to it is a change to a shared contract.
func TestDeltaGolden(t *testing.T) {
	cases := []struct {
		name  string
		delta spec.Delta
	}{
		{"spec-edit", delta.Diff(calcSpec(), subtractSpec())},
		{"observed-edit", delta.DiffObserved(calcObserved(), subtractObserved())},
		{"method-edit", delta.Diff(methodSpec(), methodSpecNext())},
		{"method-observed-edit", delta.DiffObserved(methodObserved(), methodObservedNext())},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.MarshalIndent(testCase.delta, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			encoded = append(encoded, '\n')
			path := filepath.Join("..", "..", "testdata", "delta", testCase.name+".json")
			if os.Getenv("SPECD_UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, encoded, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the golden file (run SPECD_UPDATE_GOLDEN=1 go test ./abc/delta): %v", err)
			}
			if string(encoded) != string(want) {
				t.Errorf("the delta JSON moved:\n got %s\nwant %s", encoded, want)
			}
		})
	}
}

func TestDiffOfOneRequirementAndOneInterface(t *testing.T) {
	change := delta.Diff(calcSpec(), subtractSpec())
	if change.Intent != nil {
		t.Errorf("intent changed: %+v", change.Intent)
	}
	if len(change.Requirements) != 1 {
		t.Fatalf("requirements = %+v, want one added entry", change.Requirements)
	}
	added := change.Requirements[0]
	if added.Op != spec.OpAdded || added.ID != "r.subtract" || added.To == nil {
		t.Errorf("requirement = %+v", added)
	}
	if len(change.Interfaces) != 1 {
		t.Fatalf("interfaces = %+v, want one added entry", change.Interfaces)
	}
	if change.Interfaces[0].Op != spec.OpAdded || change.Interfaces[0].Name != "Subtract" {
		t.Errorf("interface = %+v", change.Interfaces[0])
	}
	if change.CodeRefs == nil || len(change.CodeRefs.Added) != 1 || change.CodeRefs.Added[0] != "function:Subtract" {
		t.Errorf("codeRefs = %+v, want one added ref", change.CodeRefs)
	}
	if counts := change.Count(); counts != (spec.Counts{Added: 3}) {
		t.Errorf("counts = %+v, want three added", counts)
	}
	if got := delta.Summary(change); got != "+3" {
		t.Errorf("summary = %q, want +3", got)
	}
}

func TestDiffAtFieldLevel(t *testing.T) {
	edited := calcSpec()
	edited.Intent = "Arithmetic on two integers, and a promise about overflow."
	edited.Requirements[0].Level = spec.LevelShould
	edited.Interfaces[1].Signature = "func Multiply(a, b int) (int, error)"
	change := delta.Diff(calcSpec(), edited)

	if change.Intent == nil || change.Intent.To != edited.Intent {
		t.Errorf("intent = %+v", change.Intent)
	}
	if len(change.Requirements) != 1 || change.Requirements[0].ID != "r.add" {
		t.Fatalf("requirements = %+v", change.Requirements)
	}
	if got := change.Requirements[0].Fields; !reflect.DeepEqual(got, []string{spec.FieldLevel}) {
		t.Errorf("requirement fields = %v, want [level]", got)
	}
	if len(change.Interfaces) != 1 || change.Interfaces[0].Name != "Multiply" {
		t.Fatalf("interfaces = %+v", change.Interfaces)
	}
	if got := change.Interfaces[0].Fields; !reflect.DeepEqual(got, []string{spec.FieldSignature}) {
		t.Errorf("interface fields = %v, want [signature]", got)
	}
	if counts := change.Count(); counts != (spec.Counts{Changed: 3}) {
		t.Errorf("counts = %+v, want three changed", counts)
	}
}

func TestDiffSetReportsAddedAndRemoved(t *testing.T) {
	edited := calcSpec()
	edited.CodeRefs = []string{"file:calc/calc.go", "package:calc"}
	edited.Overlay = []string{"sc.base"}
	change := delta.Diff(calcSpec(), edited)
	if change.CodeRefs == nil || !reflect.DeepEqual(change.CodeRefs.Added, []string{"package:calc"}) {
		t.Errorf("codeRefs = %+v", change.CodeRefs)
	}
	if change.Overlay == nil || !reflect.DeepEqual(change.Overlay.Added, []string{"sc.base"}) {
		t.Errorf("overlay = %+v", change.Overlay)
	}
	if change.DependsOn != nil || change.Introduces != nil {
		t.Errorf("an unchanged set produced a delta: %+v", change)
	}
}

func TestDiffIgnoresOrderOfKeyedLists(t *testing.T) {
	reordered := calcSpec()
	reordered.Requirements = []spec.Requirement{reordered.Requirements[1], reordered.Requirements[0]}
	reordered.Interfaces = []spec.Interface{reordered.Interfaces[1], reordered.Interfaces[0]}
	reordered.CodeRefs = []string{"file:calc/calc.go", "file:calc/calc.go"}
	if change := delta.Diff(calcSpec(), reordered); !change.Empty() {
		t.Errorf("a reorder is not an edit: %+v", change)
	}
}

func TestDiffAndApplyRoundTrip(t *testing.T) {
	old, next := calcSpec(), subtractSpec()
	if got := delta.Apply(old, delta.Diff(old, next)); !reflect.DeepEqual(got, spec.Canonicalize(next)) {
		t.Errorf("Apply(old, Diff(old, new)) = %+v, want %+v", got, spec.Canonicalize(next))
	}
	if applied := delta.Apply(old, spec.Delta{}); !reflect.DeepEqual(applied, spec.Canonicalize(old)) {
		t.Errorf("Apply with an empty delta moved the spec: %+v", applied)
	}
	observed := delta.ApplyObserved(calcObserved(), delta.DiffObserved(calcObserved(), subtractObserved()))
	if !reflect.DeepEqual(observed, spec.CanonicalObserved(subtractObserved())) {
		t.Errorf("ApplyObserved(DiffObserved) = %+v", observed)
	}
}

// TestDiffApplyProperty is the property the plan asks for, over randomly
// generated specs: Apply(old, Diff(old, new)) equals new.
func TestDiffApplyProperty(t *testing.T) {
	random := rand.New(rand.NewSource(20261003))
	for round := 0; round < 500; round++ {
		old := randomSpec(random)
		next := randomSpec(random)
		applied := delta.Apply(old, delta.Diff(old, next))
		if want := spec.Canonicalize(next); !reflect.DeepEqual(applied, want) {
			t.Fatalf("round %d:\nold     %+v\nnew     %+v\napplied %+v\nwant    %+v", round, old, next, applied, want)
		}
		if change := delta.Diff(old, old); !change.Empty() {
			t.Fatalf("round %d: Diff(x, x) = %+v", round, change)
		}
		// Diff of the canonical forms is the same delta, so the hash and the
		// delta agree about what an edit is.
		canonical := delta.Diff(spec.Canonicalize(old), spec.Canonicalize(next))
		plain := delta.Diff(old, next)
		if !reflect.DeepEqual(canonical, plain) {
			t.Fatalf("round %d: canonical diff %+v != %+v", round, canonical, plain)
		}
	}
}

func randomSpec(random *rand.Rand) spec.SystemContextSpec {
	out := spec.SystemContextSpec{
		Repository: "calc",
		Intent:     randomWord(random),
		Upstream:   spec.RefSelf,
	}
	for index := 0; index < random.Intn(4); index++ {
		out.Requirements = append(out.Requirements, spec.Requirement{
			ID:       fmt.Sprintf("r.%s-%d", randomWord(random), index),
			Level:    spec.Levels()[random.Intn(3)],
			Text:     randomWord(random),
			CodeRefs: randomSet(random),
		})
	}
	for index := 0; index < random.Intn(4); index++ {
		out.Interfaces = append(out.Interfaces, spec.Interface{
			Name:      fmt.Sprintf("%s%d", randomWord(random), index),
			Kind:      "function",
			Signature: randomWord(random),
			File:      "calc/calc.go",
		})
	}
	out.CodeRefs = randomSet(random)
	out.Overlay = randomSet(random)
	out.DependsOn = randomSet(random)
	out.Introduces = randomSet(random)
	return out
}

func randomWord(random *rand.Rand) string {
	words := []string{"add", "multiply", "subtract", "sum", "product", "difference", "calc", "operation"}
	return words[random.Intn(len(words))]
}

func randomSet(random *rand.Rand) []string {
	values := []string{"file:calc/calc.go", "function:Add", "function:Multiply", "package:calc", "sc.base"}
	out := []string{}
	for _, value := range values {
		if random.Intn(2) == 0 {
			out = append(out, value)
		}
	}
	return out
}
