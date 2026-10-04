package agent

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func observed() spec.ObservedFacts {
	return spec.ObservedFacts{
		Files: []string{"calc/calc.go"},
		Interfaces: []spec.ObservedInterface{
			{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go", Line: 3, CodegraphID: "function:abc"},
			{Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go", Line: 8, CodegraphID: "function:def"},
		},
		Fingerprint: "f1",
	}
}

func TestParseDraftReadsAFencedAnswer(t *testing.T) {
	raw := "Here is the spec:\n\n```json\n" + `{
  "summary": "The calc package adds and multiplies.",
  "intent": "Arithmetic over two integers.",
  "requirements": [
    {"id": "r.add", "level": "MUST", "text": "Add adds two integers.", "codeRefs": ["function:abc", "Add", "function:Add", "file:calc/calc.go"]},
    {"id": "r.multiply", "level": "SHOULD", "text": "Multiply multiplies.", "codeRefs": ["function:def"]}
  ],
  "interfaces": [
    {"name": "Add", "kind": "function", "signature": "func Add(a, b int) int", "file": "calc/calc.go"},
    {"name": "Multiply", "kind": "function", "signature": "func Multiply(a, b int) int", "file": "calc/calc.go"}
  ]
}` + "\n```\n"

	draft, err := ParseDraft(raw, observed())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Intent != "Arithmetic over two integers." {
		t.Errorf("intent = %q", draft.Intent)
	}
	if draft.Summary != "The calc package adds and multiplies." {
		t.Errorf("summary = %q", draft.Summary)
	}
	if len(draft.Requirements) != 2 || draft.Requirements[0].ID != "r.add" || draft.Requirements[0].Level != spec.LevelMust {
		t.Fatalf("requirements = %+v", draft.Requirements)
	}
	if refs := draft.Requirements[0].CodeRefs; len(refs) != 2 || refs[0] != "function:abc" || refs[1] != "file:calc/calc.go" {
		t.Errorf("codeRefs = %v, want the canonical id and the file", refs)
	}
	if len(draft.Interfaces) != 2 || draft.Interfaces[1].Name != "Multiply" {
		t.Errorf("interfaces = %+v", draft.Interfaces)
	}
	if len(draft.Dropped) != 0 {
		t.Errorf("dropped = %+v, want none", draft.Dropped)
	}
}

func TestParseDraftDropsRefsTheFactsDoNotCarry(t *testing.T) {
	raw := `{"intent": "i", "requirements": [
	  {"id": "r.add", "level": "MUST", "text": "t", "codeRefs": ["function:abc", "function:nope", "file:calc/gone.go"]}
	]}`
	draft, err := ParseDraft(raw, observed())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Requirements) != 1 {
		t.Fatalf("requirements = %+v", draft.Requirements)
	}
	if refs := draft.Requirements[0].CodeRefs; len(refs) != 1 || refs[0] != "function:abc" {
		t.Errorf("kept refs = %v, want the one that resolves", refs)
	}
	if len(draft.Dropped) != 2 {
		t.Fatalf("dropped = %+v, want two", draft.Dropped)
	}
	if draft.Dropped[0].Requirement != "r.add" || draft.Dropped[0].Ref != "function:nope" {
		t.Errorf("dropped[0] = %+v", draft.Dropped[0])
	}
}

func TestParseDraftCanonicalizesABareNameToTheObservedID(t *testing.T) {
	raw := `{"intent": "i", "requirements": [
	  {"id": "r.add", "level": "MUST", "text": "t", "codeRefs": ["Add", "function:Add", "function:abc"]}
	]}`
	draft, err := ParseDraft(raw, observed())
	if err != nil {
		t.Fatal(err)
	}
	if refs := draft.Requirements[0].CodeRefs; len(refs) != 1 || refs[0] != "function:abc" {
		t.Fatalf("codeRefs = %v, want the one canonical id", refs)
	}
	if len(draft.Dropped) != 0 {
		t.Errorf("dropped = %+v, want none: every spelling names Add", draft.Dropped)
	}
	if _, result := ValidateDraft("calc", spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf}, draft); !result.OK() {
		t.Errorf("the canonicalized draft does not validate: %v", result.Err())
	}
}

func methodObserved() spec.ObservedFacts {
	return spec.ObservedFacts{
		Files: []string{"store/store.go"},
		Interfaces: []spec.ObservedInterface{
			{Name: "Indexer.List", Kind: "method", File: "store/store.go", Line: 20, CodegraphID: "method:indexer"},
			{Name: "Set.List", Kind: "method", File: "store/store.go", Line: 10, CodegraphID: "method:set"},
			{Name: "NewStore", Kind: "function", File: "store/store.go", Line: 2, CodegraphID: "function:new"},
		},
	}
}

func TestParseDraftQualifiesAMethodNameWithItsReceiver(t *testing.T) {
	raw := `{"intent": "i", "requirements": [
	  {"id": "r.list", "level": "MUST", "text": "t", "codeRefs": ["Set.List", "method:set", "function:Set.List"]}
	], "interfaces": [
	  {"name": "Set.List", "kind": "method", "file": "store/store.go"},
	  {"name": "NewStore", "kind": "function", "file": "store/store.go"}
	]}`
	draft, err := ParseDraft(raw, methodObserved())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Interfaces) != 2 || draft.Interfaces[0].Name != "Set.List" || draft.Interfaces[1].Name != "NewStore" {
		t.Fatalf("interfaces = %+v", draft.Interfaces)
	}
	if refs := draft.Requirements[0].CodeRefs; len(refs) != 1 || refs[0] != "method:set" {
		t.Errorf("codeRefs = %v, want the one canonical id", refs)
	}
	if len(draft.Dropped) != 0 {
		t.Errorf("dropped = %+v", draft.Dropped)
	}
}

func TestParseDraftResolvesABareMethodNameOnlyWhenItIsUnique(t *testing.T) {
	raw := `{"intent": "i", "requirements": [
	  {"id": "r.list", "level": "MUST", "text": "t", "codeRefs": ["Set.List"]},
	  {"id": "r.ambiguous", "level": "MUST", "text": "t", "codeRefs": ["List"]}
	]}`
	draft, err := ParseDraft(raw, methodObserved())
	if err != nil {
		t.Fatal(err)
	}
	if refs := draft.Requirements[0].CodeRefs; len(refs) != 1 || refs[0] != "method:set" {
		t.Errorf("qualified ref = %v", refs)
	}
	if refs := draft.Requirements[1].CodeRefs; len(refs) != 0 {
		t.Errorf("an ambiguous bare name resolved to %v", refs)
	}
	if len(draft.Dropped) != 1 || draft.Dropped[0].Ref != "List" {
		t.Errorf("dropped = %+v, want the ambiguous List", draft.Dropped)
	}
}

func TestParseDraftRejectsAnUnknownLevel(t *testing.T) {
	raw := `{"intent": "i", "requirements": [{"id": "r", "level": "MUSTARD", "text": "t"}]}`
	if _, err := ParseDraft(raw, observed()); err == nil {
		t.Fatal("an unknown level was accepted")
	} else if !strings.Contains(err.Error(), "MUSTARD") {
		t.Errorf("error does not name the level: %v", err)
	}
}

func TestParseDraftRejectsAHalfReadableAnswer(t *testing.T) {
	cases := map[string]string{
		"no intent":       `{"requirements": []}`,
		"duplicate id":    `{"intent": "i", "requirements": [{"id": "r", "level": "MUST", "text": "a"}, {"id": "r", "level": "MAY", "text": "b"}]}`,
		"empty text":      `{"intent": "i", "requirements": [{"id": "r", "level": "MUST", "text": " "}]}`,
		"duplicate iface": `{"intent": "i", "interfaces": [{"name": "Add"}, {"name": "Add"}]}`,
		"not json":        `I could not read the code.`,
	}
	for name, raw := range cases {
		if _, err := ParseDraft(raw, observed()); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestParseDraftFallsBackToTheIntentForTheModelZone(t *testing.T) {
	draft, err := ParseDraft(`{"intent": "Only an intent."}`, observed())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Summary != "Only an intent." {
		t.Errorf("summary = %q, want the intent", draft.Summary)
	}
}

func TestValidateDraftKeepsTheHumanFieldsAndChecksTheRest(t *testing.T) {
	base := spec.SystemContextSpec{Repository: "calc", Upstream: spec.RefSelf, Intent: "old"}
	draft := SpecDraft{
		Intent:       "new",
		Requirements: []spec.Requirement{{ID: "r.add", Level: spec.LevelMust, Text: "t", CodeRefs: []string{"function:abc"}}},
		Interfaces:   []spec.Interface{{Name: "Add", Kind: "function"}},
	}
	merged, result := ValidateDraft("calc", base, draft)
	if !result.OK() {
		t.Fatalf("a valid draft did not validate: %v", result.Err())
	}
	if merged.Intent != "new" || merged.Repository != "calc" || len(merged.Requirements) != 1 {
		t.Errorf("merged = %+v", merged)
	}

	bad := SpecDraft{Intent: "x", Requirements: []spec.Requirement{{ID: "r", Level: "NOPE", Text: "t"}}}
	if _, result := ValidateDraft("calc", base, bad); result.OK() {
		t.Error("an invalid level passed the validator")
	}
}

func TestFitKeepsThePromptAndReportsWhatItDropped(t *testing.T) {
	sections := []Section{
		{Title: "spec", Body: strings.Repeat("s", 400), Priority: 1},
		{Title: "neighbors", Body: strings.Repeat("n", 4000), Priority: 5},
	}
	kept, dropped := Fit(sections, 200)
	if len(kept) != 1 || kept[0].Title != "spec" {
		t.Fatalf("kept = %+v", kept)
	}
	if len(dropped) != 1 || dropped[0] != "neighbors" {
		t.Errorf("dropped = %v", dropped)
	}
}

func TestFitKeepsTheFirstSectionWhateverTheBudget(t *testing.T) {
	kept, dropped := Fit([]Section{{Title: "spec", Body: strings.Repeat("x", 8000), Priority: 1}}, 10)
	if len(kept) != 1 {
		t.Fatalf("kept = %+v, want the first section even over budget", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("dropped = %v", dropped)
	}
}

func TestRenderPromptCarriesTheContractAndNamesWhatIsMissing(t *testing.T) {
	neighbors := []Neighbor{}
	for index := range 60 {
		neighbors = append(neighbors, Neighbor{
			Edge: "DECLARES", Direction: DirectionOut, Label: "SpecInterface", Name: string(rune('a'+index%26)) + "-interface",
		})
	}
	bundle := ContextBundle{
		Context:    "calc",
		Repository: "calc",
		Spec:       spec.SystemContextSpec{Repository: "calc", Intent: "old"},
		Observed:   observed(),
		Budget:     200,
		Neighbors:  neighbors,
	}
	prompt, dropped := RenderPromptWithSections(bundle)
	if !strings.Contains(prompt, "\"requirements\"") {
		t.Error("the prompt does not carry the JSON contract")
	}
	if !strings.Contains(prompt, "function:abc") {
		t.Error("the prompt does not carry the observed facts")
	}
	if len(dropped) == 0 || !strings.Contains(prompt, "did not fit the token budget") {
		t.Errorf("dropped = %v, prompt must name them", dropped)
	}
}

func TestSplitContextDocSeparatesTheModelZoneFromTheManagedOne(t *testing.T) {
	document := "# Calc\n\nMy notes.\n\n" + ManagedBegin + "\n- `function:abc` function Add\n" + ManagedEnd + "\n"
	model, managed := SplitContextDoc(document)
	if model != "# Calc\n\nMy notes." {
		t.Errorf("model = %q", model)
	}
	if !strings.Contains(managed, "function:abc") {
		t.Errorf("managed = %q", managed)
	}
}

func TestComposeContextDocRegeneratesTheManagedZoneAndKeepsTheModelZone(t *testing.T) {
	refs := []ResolvedRef{{CodegraphID: "function:abc", Kind: "function", Name: "Add", FilePath: "calc/calc.go"}}
	first := ComposeContextDoc("My notes.", refs, DefaultManagedBudget)
	if !strings.Contains(first, "My notes.") || !strings.Contains(first, "function:abc") {
		t.Fatalf("document = %q", first)
	}

	model, _ := SplitContextDoc(first)
	second := ComposeContextDoc(model, nil, DefaultManagedBudget)
	if !strings.Contains(second, "My notes.") {
		t.Error("the model zone was lost")
	}
	if strings.Contains(second, "function:abc") {
		t.Error("the managed zone was not regenerated")
	}
	if !strings.Contains(second, ManagedBegin) || !strings.Contains(second, ManagedEnd) {
		t.Error("the markers are missing")
	}
}

func TestComposeContextDocAlwaysLeavesSomewhereToWrite(t *testing.T) {
	document := ComposeContextDoc("", nil, 100)
	if !strings.Contains(document, "empty") {
		t.Errorf("an empty model zone left no placeholder: %q", document)
	}
}

func TestContextDocPathIsOutsideTheProjectTree(t *testing.T) {
	if got := ContextDocPath("/state/clm", "calc", "calc"); got != "/state/clm/calc/calc.md" {
		t.Errorf("path = %q", got)
	}
}

func TestRenderDeltaNamesEveryChangedKey(t *testing.T) {
	change := spec.Delta{
		Intent:   &spec.FieldDelta{From: "old intent", To: "new intent"},
		CodeRefs: &spec.StringSetDelta{Added: []string{"function:Subtract"}, Removed: []string{"package:calc"}},
		Requirements: []spec.RequirementDelta{{
			Op: spec.OpAdded, ID: "r.subtract",
			To: &spec.Requirement{ID: "r.subtract", Level: spec.LevelShould, Text: "Subtract returns the difference.", CodeRefs: []string{"function:Subtract"}},
		}, {
			Op: spec.OpChanged, ID: "r.add", Fields: []string{spec.FieldLevel},
			From: &spec.Requirement{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum."},
			To:   &spec.Requirement{ID: "r.add", Level: spec.LevelShould, Text: "Add returns the sum."},
		}, {
			Op: spec.OpRemoved, ID: "r.gone",
			From: &spec.Requirement{ID: "r.gone", Level: spec.LevelMay, Text: "Gone."},
		}},
		Interfaces: []spec.InterfaceDelta{{
			Op: spec.OpAdded, Name: "Subtract",
			To: &spec.Interface{Name: "Subtract", Kind: "function", Signature: "func Subtract(a, b int) int", File: "calc/calc.go"},
		}},
	}
	rendered := RenderDelta(change)
	for _, want := range []string{
		`intent: "old intent" -> "new intent"`,
		"+ codeRefs: function:Subtract",
		"- codeRefs: package:calc",
		"+ requirement r.subtract [SHOULD] Subtract returns the difference.",
		"~ requirement r.add (level)",
		"~ requirement.r.add.level: \"MUST\" -> \"SHOULD\"",
		"- requirement r.gone [MAY] Gone.",
		"+ interface Subtract (function) func Subtract(a, b int) int in calc/calc.go",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the render is missing %q:\n%s", want, rendered)
		}
	}
	if got := RenderDelta(spec.Delta{}); !strings.Contains(got, "no change") {
		t.Errorf("an empty delta rendered %q", got)
	}
}

func TestRenderDeltaCoversObservedFacts(t *testing.T) {
	change := spec.Delta{Observed: &spec.ObservedDelta{
		Files: &spec.StringSetDelta{Added: []string{"calc/subtract_test.go"}},
		Interfaces: []spec.ObservedInterfaceDelta{{
			Op: spec.OpAdded, Name: "Subtract",
			To: &spec.ObservedInterface{Name: "Subtract", Kind: "function", Signature: "(a, b int) int", File: "calc/calc.go", Line: 14, CodegraphID: "function:cc33"},
		}},
	}}
	rendered := RenderDelta(change)
	if !strings.Contains(rendered, "+ observed files: calc/subtract_test.go") {
		t.Errorf("the file set is missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "+ observed interface Subtract function:cc33") {
		t.Errorf("the added interface is missing:\n%s", rendered)
	}
}

func TestEveryCanonicalRefTheParserBuildsIsAValidCodeRef(t *testing.T) {
	observed := spec.ObservedFacts{
		Files: []string{"greet/mod.ts"},
		Interfaces: []spec.ObservedInterface{
			{Name: "Greeting", Kind: "interface", CodegraphID: "interface:49216168fba9b28b25e57da4e8d5283b", File: "greet/mod.ts"},
			{Name: "Greeter", Kind: "class", CodegraphID: "class:ee1f6d1c37cc3d6c9b15e618220f4fe2", File: "greet/mod.ts"},
			{Name: "greet", Kind: "function", CodegraphID: "function:f0c27c5a33714a68f3a51c88a09bc209", File: "greet/mod.ts"},
		},
	}
	draft, err := ParseDraft(`{
		"intent": "Greetings.",
		"requirements": [
			{"id": "r.interface", "level": "MUST", "text": "The value object exists.", "codeRefs": ["Greeting"]},
			{"id": "r.class", "level": "MUST", "text": "The class exists.", "codeRefs": ["Greeter"]},
			{"id": "r.function", "level": "MUST", "text": "The function exists.", "codeRefs": ["greet", "file:greet/mod.ts"]}
		]
	}`, observed)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Dropped) != 0 {
		t.Fatalf("the parser dropped refs it built itself: %+v", draft.Dropped)
	}
	for _, requirement := range draft.Requirements {
		for _, ref := range requirement.CodeRefs {
			if !spec.IsCodeRef(ref) {
				t.Errorf("the parser built %q, which the validator refuses", ref)
			}
		}
	}
	candidate := &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "greet"},
		Spec:       DraftSpec(spec.SystemContextSpec{Repository: "unseen", Upstream: spec.RefSelf}, draft),
	}
	if result := spec.ValidateSystemContext(candidate); !result.OK() {
		t.Errorf("the spec the draft built does not validate: %v", result.Err())
	}
}
