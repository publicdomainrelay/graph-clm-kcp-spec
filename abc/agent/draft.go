package agent

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

type draftRequirement struct {
	ID string `json:"id"`

	Level string `json:"level"`

	Text string `json:"text"`

	CodeRefs []string `json:"codeRefs"`
}

type draftInterface struct {
	Name string `json:"name"`

	Kind string `json:"kind"`

	Signature string `json:"signature"`

	File string `json:"file"`
}

type draftPayload struct {
	Summary string `json:"summary"`

	Intent string `json:"intent"`

	Requirements []draftRequirement `json:"requirements"`

	Interfaces []draftInterface `json:"interfaces"`
}

// ParseDraft reads a model answer into a draft. It tolerates the fences a model
// wraps around JSON and nothing else: an unknown level, a duplicate id, an
// empty requirement or a missing intent is an error, because a spec that half
// reads is worse than a failed attempt. Code refs the observed facts do not
// answer to are dropped and reported, since a model may name a symbol the index
// spells differently and the rest of the draft is still good.
func ParseDraft(raw string, observed spec.ObservedFacts) (SpecDraft, error) {
	payload := draftPayload{}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &payload); err != nil {
		return SpecDraft{}, fmt.Errorf("agent: the answer is not one JSON object: %w", err)
	}
	if strings.TrimSpace(payload.Intent) == "" {
		return SpecDraft{}, fmt.Errorf("agent: the answer has no intent")
	}

	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = strings.TrimSpace(payload.Intent)
	}

	draft := SpecDraft{Summary: summary, Intent: strings.TrimSpace(payload.Intent)}

	canonical := canonicalRefs(observed)
	seenRequirements := map[string]bool{}
	for index, requirement := range payload.Requirements {
		id := strings.TrimSpace(requirement.ID)
		if id == "" {
			return SpecDraft{}, fmt.Errorf("agent: requirements[%d] has no id", index)
		}
		if seenRequirements[id] {
			return SpecDraft{}, fmt.Errorf("agent: requirement id %q is not unique", id)
		}
		seenRequirements[id] = true
		level := spec.Level(strings.TrimSpace(requirement.Level))
		if !slices.Contains(spec.Levels(), level) {
			return SpecDraft{}, fmt.Errorf("agent: requirements[%d] level %q is not MUST, SHOULD or MAY", index, requirement.Level)
		}
		text := strings.TrimSpace(requirement.Text)
		if text == "" {
			return SpecDraft{}, fmt.Errorf("agent: requirement %q has no text", id)
		}
		kept := []string{}
		seenRefs := map[string]bool{}
		for _, ref := range requirement.CodeRefs {
			trimmed := strings.TrimSpace(ref)
			if trimmed == "" {
				continue
			}
			// A model names a symbol the way it reads: "Add", not
			// "function:abc". The observed facts answer for both, and the
			// canonical id is what the spec stores, so a requirement is
			// anchored to the same vertex however the model spelled it.
			resolved, ok := canonical[trimmed]
			if !ok {
				if !seenRefs[trimmed] {
					seenRefs[trimmed] = true
					draft.Dropped = append(draft.Dropped, DroppedRef{
						Requirement: id,
						Ref:         trimmed,
						Reason:      "the observed facts do not name it",
					})
				}
				continue
			}
			if seenRefs[resolved] {
				continue
			}
			seenRefs[resolved] = true
			kept = append(kept, resolved)
		}
		draft.Requirements = append(draft.Requirements, spec.Requirement{
			ID:       id,
			Level:    level,
			Text:     text,
			CodeRefs: kept,
		})
	}

	seenInterfaces := map[string]bool{}
	for index, declared := range payload.Interfaces {
		name := strings.TrimSpace(declared.Name)
		if name == "" {
			return SpecDraft{}, fmt.Errorf("agent: interfaces[%d] has no name", index)
		}
		if seenInterfaces[name] {
			return SpecDraft{}, fmt.Errorf("agent: interface %q is not unique", name)
		}
		seenInterfaces[name] = true
		draft.Interfaces = append(draft.Interfaces, spec.Interface{
			Name:      name,
			Kind:      strings.TrimSpace(declared.Kind),
			Signature: strings.TrimSpace(declared.Signature),
			File:      strings.TrimSpace(declared.File),
		})
	}
	return draft, nil
}

// canonicalRefs maps every spelling of a reference the observed facts answer to
// the one spelling the spec stores: the CodeGraph id of the interface, or its
// kind and name when the index gave no id. It uses the same resolution rule as
// the controller, so a draft and a human edit cannot disagree about what a ref
// points at.
func canonicalRefs(observed spec.ObservedFacts) map[string]string {
	out := map[string]string{}
	for _, file := range observed.Files {
		ref := spec.CodeRefPrefixFile + file
		out[ref] = ref
	}
	for _, observedInterface := range observed.Interfaces {
		canonical := observedInterface.CodegraphID
		if canonical == "" {
			kind := observedInterface.Kind
			if kind == "" {
				kind = "function"
			}
			canonical = kind + ":" + observedInterface.Name
		}
		if observedInterface.Name != "" {
			out[observedInterface.Name] = canonical
			for _, prefix := range spec.CodeRefPrefixes {
				out[prefix+observedInterface.Name] = canonical
			}
		}
		if observedInterface.CodegraphID != "" {
			out[observedInterface.CodegraphID] = canonical
		}
	}
	return out
}

// DraftSpec is the spec the draft asks the context to become: the human fields
// of the base are kept, the three the model owns are replaced.
func DraftSpec(base spec.SystemContextSpec, draft SpecDraft) spec.SystemContextSpec {
	merged := base
	merged.Intent = draft.Intent
	merged.Requirements = draft.Requirements
	merged.Interfaces = draft.Interfaces
	return merged
}

// ValidateDraft is the last gate before a draft is written: the merged spec must
// pass the same validator a human edit passes.
func ValidateDraft(name string, base spec.SystemContextSpec, draft SpecDraft) (spec.SystemContextSpec, spec.Result) {
	merged := DraftSpec(base, draft)
	candidate := spec.SystemContext{}
	candidate.Name = name
	candidate.Spec = merged
	return merged, spec.ValidateSystemContext(&candidate)
}

// extractJSON finds the JSON object in a model answer. A fenced block is
// unwrapped first, then the outermost braces are taken, so surrounding prose or
// a trailing explanation does not hide the object.
func extractJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		if newline := strings.Index(trimmed, "\n"); newline >= 0 {
			trimmed = trimmed[newline+1:]
		}
		if fence := strings.LastIndex(trimmed, "```"); fence >= 0 {
			trimmed = trimmed[:fence]
		}
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}
