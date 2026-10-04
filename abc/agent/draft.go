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
		if path, found := spec.AbsoluteMachinePath(text); found {
			return SpecDraft{}, fmt.Errorf("agent: requirement %q names the machine path %s; name a path inside the repository", id, path)
		}
		if spec.EnumeratesNames(text) {
			draft.Warnings = append(draft.Warnings, fmt.Sprintf("requirement %s only enumerates names; say what must hold", id))
		}
		kept := []string{}
		seenRefs := map[string]bool{}
		for _, ref := range requirement.CodeRefs {
			trimmed := strings.TrimSpace(ref)
			if trimmed == "" {
				continue
			}
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

	keys := observedInterfaceKeys(observed)
	seenInterfaces := map[string]bool{}
	for index, declared := range payload.Interfaces {
		name := strings.TrimSpace(declared.Name)
		if name == "" {
			return SpecDraft{}, fmt.Errorf("agent: interfaces[%d] has no name", index)
		}
		key, ok := keys[name]
		if !ok {
			draft.Warnings = append(draft.Warnings, fmt.Sprintf("interface %s is not in the observed facts; dropped", name))
			continue
		}
		name = key
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

func observedInterfaceKeys(observed spec.ObservedFacts) map[string]string {
	out := map[string]string{}
	exact := map[string]bool{}
	for _, entry := range observed.Interfaces {
		exact[entry.Name] = true
	}
	counts := map[string]int{}
	for _, entry := range observed.Interfaces {
		index := strings.LastIndex(entry.Name, ".")
		if index < 0 || index == len(entry.Name)-1 {
			continue
		}
		bare := entry.Name[index+1:]
		counts[bare]++
		out[bare] = entry.Name
	}
	for bare, count := range counts {
		if count != 1 || exact[bare] {
			delete(out, bare)
		}
	}
	for _, entry := range observed.Interfaces {
		out[entry.Name] = entry.Name
	}
	return out
}

func canonicalRefs(observed spec.ObservedFacts) map[string]string {
	out := map[string]string{}
	for _, file := range append(append([]string{}, observed.Files...), observed.TreeFiles...) {
		ref := spec.CodeRefPrefixFile + file
		out[ref] = ref
	}
	bare := map[string]int{}
	exact := map[string]bool{}
	for _, entry := range observed.Interfaces {
		exact[entry.Name] = true
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
		if index := strings.LastIndex(observedInterface.Name, "."); index >= 0 && index < len(observedInterface.Name)-1 {
			bareName := observedInterface.Name[index+1:]
			bare[bareName]++
			if bare[bareName] > 1 || exact[bareName] {
				delete(out, bareName)
				for _, prefix := range spec.CodeRefPrefixes {
					delete(out, prefix+bareName)
				}
				continue
			}
			out[bareName] = canonical
			for _, prefix := range spec.CodeRefPrefixes {
				out[prefix+bareName] = canonical
			}
		}
	}
	return out
}

func DraftSpec(base spec.SystemContextSpec, draft SpecDraft) spec.SystemContextSpec {
	merged := base
	merged.Intent = draft.Intent
	merged.Requirements = draft.Requirements
	merged.Interfaces = draft.Interfaces
	return merged
}

func ValidateDraft(name string, base spec.SystemContextSpec, observed spec.ObservedFacts, draft SpecDraft) (spec.SystemContextSpec, spec.Result) {
	merged := DraftSpec(base, draft)
	candidate := spec.SystemContext{}
	candidate.Name = name
	candidate.Spec = merged
	result := spec.ValidateSystemContext(&candidate)
	if spec.CommandContext(append(append([]string{}, observed.Files...), observed.TreeFiles...)) &&
		!spec.DeclaresConfigSurface(merged.Requirements) {
		result.Problems = append(result.Problems, spec.Problem{
			Path:    "spec.requirements",
			Message: "a command entrypoint must declare its configuration surface: its flags, the environment variables behind them and their defaults",
		})
	}
	return merged, result
}

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
