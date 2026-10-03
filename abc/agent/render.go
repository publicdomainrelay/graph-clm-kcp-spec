package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

// Section is one labelled block of a prompt. Priority orders the cut: the
// lowest number is kept longest.
type Section struct {
	Title string

	Body string

	Priority int
}

func EstimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// Fit keeps whole sections in priority order until the budget is spent, and
// reports the titles it dropped. A section is never cut in half: a half a
// signature is worse than no signature, and the caller can see what is missing.
// The first section always survives, because a prompt without its contract is
// not a prompt.
func Fit(sections []Section, budgetTokens int) ([]Section, []string) {
	ordered := append([]Section{}, sections...)
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].Priority < ordered[right].Priority
	})

	kept := make([]Section, 0, len(ordered))
	dropped := []string{}
	spent := 0
	for index, section := range ordered {
		cost := EstimateTokens(renderSection(section))
		if index > 0 && budgetTokens > 0 && spent+cost > budgetTokens {
			dropped = append(dropped, section.Title)
			continue
		}
		kept = append(kept, section)
		spent += cost
	}
	return kept, dropped
}

func renderSection(section Section) string {
	return "## " + section.Title + "\n\n" + section.Body + "\n\n"
}

// RenderPrompt is the instruction block every summarize ask carries. The
// contract is the parser's contract: a model that keeps it produces a draft
// that reads back.
const RenderPrompt = `You summarize a piece of a code base into a specification.

Answer with one JSON object and nothing else. No prose around it, no markdown fence.

{
  "summary": "one paragraph of plain prose describing this context, for the context document",
  "intent": "one paragraph: what this context is and why it exists",
  "requirements": [
    {"id": "r.<kebab-case>", "level": "MUST", "text": "what must hold", "codeRefs": ["function:<codegraph id>", "file:<path>"]}
  ],
  "interfaces": [
    {"name": "Add", "kind": "function", "signature": "func Add(a, b int) int", "file": "calc/calc.go"}
  ]
}

Rules:
- level is MUST, SHOULD or MAY, nothing else.
- Every codeRef names something in the observed facts below: the CodeGraph id
  exactly as it is spelled there (function:1a2b3c), or file:<path>. A bare name
  is read as the interface of that name, and a ref that resolves to nothing is
  dropped, which leaves its requirement anchored to nothing.
- requirement ids are unique inside the context.
- Name every interface the observed facts export, with the kind and signature
  the facts carry.
- Prefer the observed facts over the prose. The spec must describe the code
  that is there, not the code you would have written.
`

// RenderPromptWithSections turns a bundle into the text a model reads: the
// contract, then the sections the budget kept. Dropped sections are named at
// the end so the model knows what it is not being shown.
func RenderPromptWithSections(bundle ContextBundle) (string, []string) {
	kept, dropped := Fit(Sections(bundle), bundle.Budget)
	builder := strings.Builder{}
	builder.WriteString(RenderPrompt)
	builder.WriteString("\n---\n\n")
	for _, section := range kept {
		builder.WriteString(renderSection(section))
	}
	if len(dropped) > 0 {
		fmt.Fprintf(&builder, "## omitted\n\nThese sections did not fit the token budget: %s\n", strings.Join(dropped, ", "))
	}
	return builder.String(), dropped
}

// Sections lays the bundle out in the order the model should read it and the
// order the budget should cut it.
func Sections(bundle ContextBundle) []Section {
	sections := []Section{
		{Title: "context", Body: bundle.Context + " in repository " + bundle.Repository, Priority: 0},
		{Title: "spec", Body: renderSpec(bundle.Spec), Priority: 1},
		{Title: "observed code facts", Body: renderObserved(bundle.Observed), Priority: 2},
	}
	if strings.TrimSpace(bundle.ContextDoc) != "" {
		sections = append(sections, Section{Title: "context document", Body: bundle.ContextDoc, Priority: 3})
	}
	for _, excerpt := range bundle.CodeExcerpts {
		sections = append(sections, Section{Title: excerpt.Source, Body: excerpt.Text, Priority: 4})
	}
	if len(bundle.Neighbors) > 0 {
		sections = append(sections, Section{Title: "graph neighbors", Body: renderNeighbors(bundle.Neighbors), Priority: 5})
	}
	return sections
}

func renderSpec(specification spec.SystemContextSpec) string {
	builder := strings.Builder{}
	fmt.Fprintf(&builder, "repository: %s\n", specification.Repository)
	if specification.Intent != "" {
		fmt.Fprintf(&builder, "intent: %s\n", specification.Intent)
	}
	if len(specification.Requirements) > 0 {
		builder.WriteString("requirements:\n")
		for _, requirement := range specification.Requirements {
			fmt.Fprintf(&builder, "- [%s] %s: %s\n", requirement.Level, requirement.ID, requirement.Text)
			if len(requirement.CodeRefs) > 0 {
				fmt.Fprintf(&builder, "  codeRefs: %s\n", strings.Join(requirement.CodeRefs, ", "))
			}
		}
	}
	if len(specification.Interfaces) > 0 {
		builder.WriteString("interfaces:\n")
		for _, declared := range specification.Interfaces {
			fmt.Fprintf(&builder, "- %s (%s) %s in %s\n",
				declared.Name, declared.Kind, declared.Signature, declared.File)
		}
	}
	return builder.String()
}

func renderObserved(observed spec.ObservedFacts) string {
	builder := strings.Builder{}
	builder.WriteString("files:\n")
	for _, file := range observed.Files {
		fmt.Fprintf(&builder, "- %s\n", file)
	}
	builder.WriteString("interfaces:\n")
	for _, declared := range observed.Interfaces {
		fmt.Fprintf(&builder, "- %s %s (%s) in %s:%d\n",
			declared.CodegraphID, declared.Name, declared.Signature, declared.File, declared.Line)
	}
	return builder.String()
}

func renderNeighbors(neighbors []Neighbor) string {
	ordered := append([]Neighbor{}, neighbors...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Edge != ordered[right].Edge {
			return ordered[left].Edge < ordered[right].Edge
		}
		return ordered[left].Name < ordered[right].Name
	})
	builder := strings.Builder{}
	for _, neighbor := range ordered {
		fmt.Fprintf(&builder, "- %s %s %s %s\n", neighbor.Direction, neighbor.Edge, neighbor.Label, neighbor.Name)
	}
	return builder.String()
}
