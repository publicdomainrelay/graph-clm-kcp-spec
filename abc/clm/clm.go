package clm

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const (
	HeaderLine = "# Context: "

	SpecHeading = "## spec"

	SpecFence = "```yaml spec"

	SpecFenceClose = "```"

	EmptyIntent = "_(empty: write what this context is for)_"

	Notice = "_Write the prose above and the fields in the spec block. " +
		"`codeRefs` and the resolved references below are maintained by the tool; " +
		"an edit there is lost._"

	ManagedNotice = "_The resolved code references are regenerated on every run. " +
		"Cite the ids above rather than writing them here._"

	RemovalHint = "# removed: [r.id, ...] names the requirements this document deletes; " +
		"the tool refuses a removal that is not listed here or passed to --allow-remove"
)

type specBlock struct {
	Upstream string `json:"upstream,omitempty"`

	Overlay []string `json:"overlay,omitempty"`

	Orchestrator string `json:"orchestrator,omitempty"`

	DependsOn []string `json:"dependsOn,omitempty"`

	Introduces []string `json:"introduces,omitempty"`

	Requirements []spec.Requirement `json:"requirements,omitempty"`

	Interfaces []spec.Interface `json:"interfaces,omitempty"`

	Removed []string `json:"removed,omitempty"`
}

// Parsed is a model zone read apart from the spec it declares: the declared
// state plus the removal marker, which lists the requirement ids the document
// deletes on purpose. The marker is an instruction to the apply, not part of
// the spec, so it never lands in kcp.
type Parsed struct {
	Declared spec.SystemContextSpec

	Removed []string
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

func Declared(in spec.SystemContextSpec) spec.SystemContextSpec {
	out := spec.Canonicalize(in)
	out.Repository = ""
	out.CodeRefs = nil
	out.Arch = nil
	return out
}

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

func RenderModelZone(context, repository string, in spec.SystemContextSpec) (string, error) {
	return RenderModelZoneWithProse(context, repository, in.Intent, in)
}

// ProseZone renders the model zone without the spec block: the prose only. The
// architecture branch keeps it beside the managed reference zone, so the spec
// lives in specs/ alone.
func ProseZone(context, repository, prose string) (string, error) {
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
	builder.WriteString(ManagedNotice + "\n")
	return builder.String(), nil
}

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
	encoded, err := yaml.Marshal(blockOf(in))
	if err != nil {
		return "", fmt.Errorf("clm: encode the spec block of %s: %w", context, err)
	}
	builder.Write(encoded)
	builder.WriteString(RemovalHint + "\n")
	builder.WriteString(SpecFenceClose + "\n")
	return builder.String(), nil
}

func ParseModelZone(text string) (spec.SystemContextSpec, error) {
	parsed, err := ParseDocument(text)
	if err != nil {
		return spec.SystemContextSpec{}, err
	}
	return parsed.Declared, nil
}

func ParseDocument(text string) (Parsed, error) {
	open := strings.Index(text, SpecFence)
	if open == -1 {
		return Parsed{}, fmt.Errorf("clm: the model zone opens no %q block", SpecFence)
	}
	rest := text[open+len(SpecFence):]
	closeAt := strings.Index(rest, SpecFenceClose)
	if closeAt == -1 {
		return Parsed{}, fmt.Errorf("clm: the %q block is not closed", SpecFence)
	}
	body := rest[:closeAt]

	block := specBlock{}
	if err := yaml.Unmarshal([]byte(body), &block); err != nil {
		return Parsed{}, fmt.Errorf("clm: read the spec block: %w", err)
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
	return Parsed{Declared: Declared(out), Removed: spec.CanonicalSet(block.Removed)}, nil
}

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

func Document(modelZone string, refs []agent.ResolvedRef, managedBudget int) string {
	return agent.ComposeContextDoc(modelZone, refs, managedBudget)
}

func SplitDocument(text string) (string, string) {
	return agent.SplitContextDoc(text)
}
