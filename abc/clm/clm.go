// Package clm is the pure half of the context language model loop: the shape of
// a context document's model zone (prose intent plus a fenced spec block), the
// strict reading of that zone back into a spec, and the boundary between the
// fields a model owns and the fields the tool owns.
//
// It holds no transport and no file, so the format is testable on its own, and
// it is the format the TypeScript core in clm/core mirrors: the Go side is the
// authority, because the delta it computes is abc/delta's.
package clm

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const (
	// HeaderLine names the context the document describes.
	HeaderLine = "# Context: "

	// SpecHeading separates the prose intent from the machine spec.
	SpecHeading = "## spec"

	// SpecFence opens the fenced block that carries the machine fields.
	SpecFence = "```yaml spec"

	// SpecFenceClose closes it.
	SpecFenceClose = "```"

	// EmptyIntent is the placeholder a document with no intent carries. It maps
	// back to an empty intent, so rendering and parsing an empty spec is a no-op.
	EmptyIntent = "_(empty: write what this context is for)_"

	// Notice tells the model which fields are its own and which the tool owns.
	Notice = "_Write the prose above and the fields in the spec block. " +
		"`codeRefs` and the resolved references below are maintained by the tool; " +
		"an edit there is lost._"

	// ManagedSetup explains the markers, mirroring the pi host's protocol.
	ManagedNotice = "_The resolved code references are regenerated on every run. " +
		"Cite the ids above rather than writing them here._"
)

// specBlock is the machine half of a model zone: exactly the fields a model
// owns. The repository, the code refs and the arch block are the tool's, so
// they are not in the struct at all and cannot be spelled into a document.
type specBlock struct {
	Upstream string `json:"upstream,omitempty"`

	Overlay []string `json:"overlay,omitempty"`

	Orchestrator string `json:"orchestrator,omitempty"`

	DependsOn []string `json:"dependsOn,omitempty"`

	Introduces []string `json:"introduces,omitempty"`

	Requirements []spec.Requirement `json:"requirements,omitempty"`

	Interfaces []spec.Interface `json:"interfaces,omitempty"`
}

func blockOf(in spec.SystemContextSpec) specBlock {
	declared := Declared(in)
	return specBlock{
		Upstream:     declared.Upstream,
		Overlay:      declared.Overlay,
		Orchestrator: declared.Orchestrator,
		DependsOn:    declared.DependsOn,
		Introduces:   declared.Introduces,
		Requirements: declared.Requirements,
		Interfaces:   declared.Interfaces,
	}
}

// Declared strips the fields the tool owns from a spec, leaving what a model
// may write: the intent, the refs, the requirements and the interfaces. The
// code refs are derived from the tree and the arch block is the importer's, so
// neither belongs in a model zone.
func Declared(in spec.SystemContextSpec) spec.SystemContextSpec {
	out := spec.Canonicalize(in)
	out.Repository = ""
	out.CodeRefs = nil
	out.Arch = nil
	return out
}

// MergeDeclared puts the fields a model wrote back onto the spec the cluster
// holds, keeping the fields the tool owns. A field the model left out is taken
// from the base, so a document written before a field existed does not erase it.
func MergeDeclared(base, declared spec.SystemContextSpec) spec.SystemContextSpec {
	out := base
	out.Intent = declared.Intent
	out.Upstream = declared.Upstream
	out.Overlay = declared.Overlay
	out.Orchestrator = declared.Orchestrator
	out.DependsOn = declared.DependsOn
	out.Introduces = declared.Introduces
	out.Requirements = declared.Requirements
	out.Interfaces = declared.Interfaces
	return spec.Canonicalize(out)
}

// RenderModelZone is the model zone of one context document: the prose intent,
// then the declared spec as a fenced yaml block. It is what `specctl clm render`
// writes and what `specctl clm apply` reads back, so a render followed by an
// apply with no edit is a no-op.
func RenderModelZone(context, repository string, in spec.SystemContextSpec) (string, error) {
	return RenderModelZoneWithProse(context, repository, in.Intent, in)
}

// RenderModelZoneWithProse is the same zone with the prose given separately from
// the intent. The summarize path uses it: the model's prose there is its own
// summary, and the block still carries the spec it produced, so a document
// either direction writes is read by the same parser.
func RenderModelZoneWithProse(context, repository, prose string, in spec.SystemContextSpec) (string, error) {
	builder := strings.Builder{}
	builder.WriteString(HeaderLine + context + "\n\n")
	if repository != "" {
		fmt.Fprintf(&builder, "Repository: `%s`\n\n", repository)
	}
	intent := strings.TrimSpace(prose)
	if intent == "" {
		intent = EmptyIntent
	}
	builder.WriteString(intent + "\n\n")
	builder.WriteString(Notice + "\n\n")
	builder.WriteString(SpecHeading + "\n\n")
	builder.WriteString(SpecFence + "\n")
	// The prose above is the one authority for the intent, so the block leaves
	// it out: two spellings of one field is a way for them to disagree.
	encoded, err := yaml.Marshal(blockOf(in))
	if err != nil {
		return "", fmt.Errorf("clm: encode the spec block of %s: %w", context, err)
	}
	builder.Write(encoded)
	builder.WriteString(SpecFenceClose + "\n")
	return builder.String(), nil
}

// ParseModelZone reads a model zone strictly: an intent from the prose, a spec
// from the fenced block. A zone that opens no spec block is an error, because a
// document without one says nothing the tool can act on.
func ParseModelZone(text string) (spec.SystemContextSpec, error) {
	open := strings.Index(text, SpecFence)
	if open == -1 {
		return spec.SystemContextSpec{}, fmt.Errorf("clm: the model zone opens no %q block", SpecFence)
	}
	rest := text[open+len(SpecFence):]
	closeAt := strings.Index(rest, SpecFenceClose)
	if closeAt == -1 {
		return spec.SystemContextSpec{}, fmt.Errorf("clm: the %q block is not closed", SpecFence)
	}
	body := rest[:closeAt]

	block := specBlock{}
	if err := yaml.Unmarshal([]byte(body), &block); err != nil {
		return spec.SystemContextSpec{}, fmt.Errorf("clm: read the spec block: %w", err)
	}
	out := spec.SystemContextSpec{
		Intent:       parseIntent(text[:open]),
		Upstream:     block.Upstream,
		Overlay:      block.Overlay,
		Orchestrator: block.Orchestrator,
		DependsOn:    block.DependsOn,
		Introduces:   block.Introduces,
		Requirements: block.Requirements,
		Interfaces:   block.Interfaces,
	}
	return Declared(out), nil
}

// parseIntent is the prose between the header and the spec heading. The
// repository line and the notice are the tool's, so they are not the intent.
func parseIntent(prose string) string {
	lines := strings.Split(prose, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, HeaderLine):
			continue
		case strings.HasPrefix(trimmed, "Repository: `"):
			continue
		case trimmed == Notice:
			continue
		case trimmed == strings.TrimSpace(SpecHeading):
			continue
		}
		kept = append(kept, line)
	}
	intent := strings.TrimSpace(strings.Join(kept, "\n"))
	if intent == EmptyIntent {
		return ""
	}
	return intent
}

// Document composes a whole context document: the model zone above the managed
// zone agent.ComposeContextDoc renders. One format serves both hosts, so what
// the pi extension writes and what the Claude Code mod writes are the same
// file.
func Document(modelZone string, refs []agent.ResolvedRef, managedBudget int) string {
	return agent.ComposeContextDoc(modelZone, refs, managedBudget)
}

// SplitDocument separates the model zone from the managed zone of a whole
// context document.
func SplitDocument(text string) (string, string) {
	return agent.SplitContextDoc(text)
}
