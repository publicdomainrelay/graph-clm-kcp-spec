package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

type Section struct {
	Title string

	Body string

	Priority int
}

func EstimateTokens(text string) int {
	return (len(text) + 3) / 4
}

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

func RenderDelta(change spec.Delta) string {
	builder := strings.Builder{}
	renderFieldDelta(&builder, "intent", change.Intent)
	renderFieldDelta(&builder, "upstream", change.Upstream)
	renderFieldDelta(&builder, "orchestrator", change.Orchestrator)
	renderSetDelta(&builder, "overlay", change.Overlay)
	renderSetDelta(&builder, "dependsOn", change.DependsOn)
	renderSetDelta(&builder, "introduces", change.Introduces)
	renderSetDelta(&builder, "codeRefs", change.CodeRefs)
	for _, requirement := range change.Requirements {
		switch requirement.Op {
		case spec.OpAdded:
			if requirement.To != nil {
				fmt.Fprintf(&builder, "+ requirement %s [%s] %s\n", requirement.To.ID, requirement.To.Level, requirement.To.Text)
				renderSetDelta(&builder, "requirement."+requirement.To.ID+".codeRefs", setDeltaOf(requirement.To.CodeRefs, nil))
			}
		case spec.OpRemoved:
			if requirement.From != nil {
				fmt.Fprintf(&builder, "- requirement %s [%s] %s\n", requirement.From.ID, requirement.From.Level, requirement.From.Text)
			}
		default:
			fmt.Fprintf(&builder, "~ requirement %s (%s)\n", requirement.ID, strings.Join(requirement.Fields, ", "))
			if requirement.From != nil && requirement.To != nil {
				renderFieldDelta(&builder, "requirement."+requirement.ID+".level", fieldOf(requirement.From.Level != requirement.To.Level, string(requirement.From.Level), string(requirement.To.Level)))
				renderFieldDelta(&builder, "requirement."+requirement.ID+".text", fieldOf(requirement.From.Text != requirement.To.Text, requirement.From.Text, requirement.To.Text))
				renderSetDelta(&builder, "requirement."+requirement.ID+".codeRefs", setDeltaOf(requirement.To.CodeRefs, requirement.From.CodeRefs))
			}
		}
	}
	for _, declared := range change.Interfaces {
		switch declared.Op {
		case spec.OpAdded:
			if declared.To != nil {
				fmt.Fprintf(&builder, "+ interface %s %s\n", declared.Name, renderInterface(*declared.To))
			}
		case spec.OpRemoved:
			if declared.From != nil {
				fmt.Fprintf(&builder, "- interface %s %s\n", declared.Name, renderInterface(*declared.From))
			}
		default:
			fmt.Fprintf(&builder, "~ interface %s (%s)\n", declared.Name, strings.Join(declared.Fields, ", "))
			if declared.From != nil && declared.To != nil {
				renderInterfaceFields(&builder, "interface."+declared.Name+".", *declared.From, *declared.To)
			}
		}
	}
	if observed := change.Observed; observed != nil {
		renderSetDelta(&builder, "observed files", observed.Files)
		for _, entry := range observed.Interfaces {
			switch entry.Op {
			case spec.OpAdded:
				if entry.To != nil {
					fmt.Fprintf(&builder, "+ observed interface %s %s\n", entry.Name, renderObservedInterface(*entry.To))
				}
			case spec.OpRemoved:
				if entry.From != nil {
					fmt.Fprintf(&builder, "- observed interface %s %s\n", entry.Name, renderObservedInterface(*entry.From))
				}
			default:
				fmt.Fprintf(&builder, "~ observed interface %s (%s)\n", entry.Name, strings.Join(entry.Fields, ", "))
			}
		}
		renderFieldDelta(&builder, "observed fingerprint", observed.Fingerprint)
	}
	if builder.Len() == 0 {
		return "(no change)\n"
	}
	return builder.String()
}

func renderInterfaceFields(builder *strings.Builder, prefix string, from, to spec.Interface) {
	renderFieldDelta(builder, prefix+"kind", fieldOf(from.Kind != to.Kind, from.Kind, to.Kind))
	renderFieldDelta(builder, prefix+"signature", fieldOf(from.Signature != to.Signature, from.Signature, to.Signature))
	renderFieldDelta(builder, prefix+"file", fieldOf(from.File != to.File, from.File, to.File))
}

func renderInterface(declared spec.Interface) string {
	return fmt.Sprintf("(%s) %s in %s", declared.Kind, declared.Signature, declared.File)
}

func renderObservedInterface(observed spec.ObservedInterface) string {
	return fmt.Sprintf("%s (%s) in %s:%d", observed.CodegraphID, observed.Signature, observed.File, observed.Line)
}

func fieldOf(changed bool, from, to string) *spec.FieldDelta {
	if !changed {
		return nil
	}
	return &spec.FieldDelta{From: from, To: to}
}

func setDeltaOf(after, before []string) *spec.StringSetDelta {
	have := map[string]bool{}
	for _, value := range before {
		have[value] = true
	}
	want := map[string]bool{}
	for _, value := range after {
		want[value] = true
	}
	out := &spec.StringSetDelta{}
	for _, value := range spec.CanonicalSet(after) {
		if !have[value] {
			out.Added = append(out.Added, value)
		}
	}
	for _, value := range spec.CanonicalSet(before) {
		if !want[value] {
			out.Removed = append(out.Removed, value)
		}
	}
	if len(out.Added) == 0 && len(out.Removed) == 0 {
		return nil
	}
	return out
}

func renderFieldDelta(builder *strings.Builder, label string, change *spec.FieldDelta) {
	if change == nil {
		return
	}
	fmt.Fprintf(builder, "~ %s: %q -> %q\n", label, change.From, change.To)
}

func renderSetDelta(builder *strings.Builder, label string, change *spec.StringSetDelta) {
	if change == nil {
		return
	}
	for _, value := range change.Added {
		fmt.Fprintf(builder, "+ %s: %s\n", label, value)
	}
	for _, value := range change.Removed {
		fmt.Fprintf(builder, "- %s: %s\n", label, value)
	}
}

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
  the facts carry. Spell a method's name exactly as the facts do, receiver
  first: a method of a type is Type.Method, never the bare method name, so two
  types that both offer a method named List stay two describable entries.
- Prefer the observed facts over the prose. The spec must describe the code
  that is there, not the code you would have written.
- When this context is a command entrypoint (its files are under cmd/), one
  requirement must state its configuration surface: each flag, the environment
  variable behind it, and the default when the flag is absent.
- Never write an absolute machine path (/home/..., /Users/..., /tmp/...). Name
  a path inside the repository, or a path the code itself builds.
- A requirement says what must hold; a list of method or field names alone says
  nothing. Say what the code does with them.
`

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
	if specification.Arch != nil && len(specification.Arch.Node) > 0 {
		if body, err := json.Marshal(specification.Arch.Node); err == nil {
			text := string(body)
			if len(text) > archBodyLimit {
				text = text[:archBodyLimit] + "..."
			}
			fmt.Fprintf(&builder, "arch node %s:\n%s\n", specification.Arch.ID, text)
		}
	}
	return builder.String()
}

const archBodyLimit = 2000

func renderObserved(observed spec.ObservedFacts) string {
	builder := strings.Builder{}
	builder.WriteString("files:\n")
	for _, file := range observed.Files {
		fmt.Fprintf(&builder, "- %s\n", file)
	}
	if len(observed.TreeFiles) > 0 {
		builder.WriteString("tracked files the code index does not cover:\n")
		for _, file := range observed.TreeFiles {
			fmt.Fprintf(&builder, "- %s\n", file)
		}
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
