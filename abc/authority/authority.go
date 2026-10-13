// Package authority decides who may write which part of a spec. A unit (the
// intent, one requirement, one interface, one interaction) a person, a model
// or a plan wrote is declared: the code to spec writers (summarize, the arch
// seed) keep it as it is and record what they would have written as a
// proposal. Everything else is derived and the code to spec writers own it.
package authority

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

const (
	UnitIntent = "intent"

	PrefixRequirement = "requirement/"
	PrefixInterface   = "interface/"
	PrefixInteraction = "interaction/"
)

func RequirementUnit(id string) string { return PrefixRequirement + id }

func InterfaceUnit(name string) string { return PrefixInterface + name }

func InteractionUnit(id string) string { return PrefixInteraction + id }

func ValidUnit(unit string) bool {
	if unit == UnitIntent {
		return true
	}
	for _, prefix := range []string{PrefixRequirement, PrefixInterface, PrefixInteraction} {
		if strings.HasPrefix(unit, prefix) && len(unit) > len(prefix) {
			return true
		}
	}
	return false
}

type Set map[string]bool

func NewSet(units ...string) Set {
	out := Set{}
	for _, unit := range units {
		out[unit] = true
	}
	return out
}

func (s Set) Sorted() []string {
	out := make([]string, 0, len(s))
	for unit := range s {
		out = append(out, unit)
	}
	sort.Strings(out)
	return out
}

func (s Set) Union(other Set) Set {
	out := Set{}
	for unit := range s {
		out[unit] = true
	}
	for unit := range other {
		out[unit] = true
	}
	return out
}

func (s Set) Without(other Set) Set {
	out := Set{}
	for unit := range s {
		if !other[unit] {
			out[unit] = true
		}
	}
	return out
}

func (s Set) Intersect(other Set) Set {
	out := Set{}
	for unit := range s {
		if other[unit] {
			out[unit] = true
		}
	}
	return out
}

func (s Set) Equal(other Set) bool {
	return reflect.DeepEqual(s.Sorted(), other.Sorted())
}

// Parse reads the declared annotation: a JSON array of units. An empty value
// is the empty set.
func Parse(annotation string) (Set, error) {
	trimmed := strings.TrimSpace(annotation)
	if trimmed == "" {
		return Set{}, nil
	}
	units := []string{}
	if err := json.Unmarshal([]byte(trimmed), &units); err != nil {
		return Set{}, fmt.Errorf("authority: the declared annotation is not a JSON array of units: %w", err)
	}
	out := Set{}
	for _, unit := range units {
		if !ValidUnit(unit) {
			return Set{}, fmt.Errorf("authority: %q is not a spec unit", unit)
		}
		out[unit] = true
	}
	return out, nil
}

func Format(units Set) string {
	if len(units) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(units.Sorted())
	return string(encoded)
}

// Changed names the units that differ between two specs. A requirement whose
// only change is its code refs is not changed: refs are anchors the ingest
// re-derives from the code.
func Changed(from, to spec.SystemContextSpec) Set {
	change := delta.Diff(from, to)
	out := Set{}
	if change.Intent != nil {
		out[UnitIntent] = true
	}
	for _, entry := range change.Requirements {
		if entry.Op == spec.OpChanged && onlyCodeRefs(entry.Fields) {
			continue
		}
		out[RequirementUnit(entry.ID)] = true
	}
	for _, entry := range change.Interfaces {
		out[InterfaceUnit(entry.Name)] = true
	}
	for _, entry := range change.Interactions {
		out[InteractionUnit(entry.ID)] = true
	}
	return out
}

// Removed names the units a spec no longer holds: the requirements, interfaces
// and interactions an edit takes away. A removal is never declared by the edit
// that makes it, so the writer of a removal has to say so on purpose.
func Removed(from, to spec.SystemContextSpec) Set {
	change := delta.Diff(from, to)
	out := Set{}
	for _, entry := range change.Requirements {
		if entry.Op == spec.OpRemoved {
			out[RequirementUnit(entry.ID)] = true
		}
	}
	for _, entry := range change.Interfaces {
		if entry.Op == spec.OpRemoved {
			out[InterfaceUnit(entry.Name)] = true
		}
	}
	for _, entry := range change.Interactions {
		if entry.Op == spec.OpRemoved {
			out[InteractionUnit(entry.ID)] = true
		}
	}
	return out
}

func onlyCodeRefs(fields []string) bool {
	return len(fields) == 1 && fields[0] == spec.FieldCodeRefs
}

// Declared is the set a code to spec writer must keep: the recorded units plus
// every unit of an edit not realized yet, which closes the window between a
// person's edit and specd recording it.
func Declared(recorded Set, current spec.SystemContextSpec, realized *spec.SystemContextSpec) Set {
	out := recorded.Union(nil)
	if realized != nil {
		out = out.Union(Changed(*realized, current))
	}
	return out
}

// Substitute returns target with each unit in units taken from source: its
// value when source has it, its absence when source does not.
func Substitute(target, source spec.SystemContextSpec, units Set) spec.SystemContextSpec {
	out := target
	if units[UnitIntent] {
		out.Intent = source.Intent
	}
	out.Requirements = substituteList(target.Requirements, source.Requirements, units,
		func(entry spec.Requirement) string { return RequirementUnit(entry.ID) })
	out.Interfaces = substituteList(target.Interfaces, source.Interfaces, units,
		func(entry spec.Interface) string { return InterfaceUnit(entry.Name) })
	out.Interactions = substituteList(target.Interactions, source.Interactions, units,
		func(entry spec.Interaction) string { return InteractionUnit(entry.ID) })
	return out
}

func substituteList[T any](target, source []T, units Set, unitOf func(T) string) []T {
	if len(units) == 0 {
		return target
	}
	fromSource := map[string]T{}
	for _, entry := range source {
		fromSource[unitOf(entry)] = entry
	}
	out := []T{}
	placed := Set{}
	for _, entry := range target {
		unit := unitOf(entry)
		if !units[unit] {
			out = append(out, entry)
			continue
		}
		if kept, ok := fromSource[unit]; ok {
			out = append(out, kept)
			placed[unit] = true
		}
	}
	for _, entry := range source {
		unit := unitOf(entry)
		if units[unit] && !placed[unit] {
			out = append(out, entry)
			placed[unit] = true
		}
	}
	if len(out) == 0 && target == nil {
		return nil
	}
	return out
}

// Guard merges a code to spec draft into the current spec: every declared
// unit keeps its current value, and each declared unit the draft would have
// changed becomes a proposal.
func Guard(current, drafted spec.SystemContextSpec, declared Set) (spec.SystemContextSpec, []spec.Proposal) {
	merged := Substitute(drafted, current, declared)
	return merged, Proposals(current, drafted, declared)
}

// Write is what a code to spec writer stores: the spec, the spec it may mark
// realized, and the proposals for the declared units it did not write.
type Write struct {
	Declared Set

	Spec spec.SystemContextSpec

	Realized spec.SystemContextSpec

	Proposals []spec.Proposal
}

// Merge plans a code to spec write. The realized spec takes the realized side
// of every declared unit, so a person's edit that is not realized yet stays a
// spec to code change instead of being marked done by the summary.
func Merge(recorded Set, current spec.SystemContextSpec, realized *spec.SystemContextSpec, drafted spec.SystemContextSpec) Write {
	declared := Declared(recorded, current, realized)
	merged, proposals := Guard(current, drafted, declared)
	base := current
	if realized != nil {
		base = *realized
	}
	return Write{
		Declared:  declared,
		Spec:      merged,
		Realized:  Substitute(merged, base, declared),
		Proposals: proposals,
	}
}

// Proposals is the readable, deduplicated difference between a current and a
// drafted spec for the units a code to spec writer must keep: one proposal per
// declared unit, with the rendered value of the unit on each side. A unit whose
// two values say the same thing (equal, or different only in whitespace)
// raises no proposal.
func Proposals(current, drafted spec.SystemContextSpec, declared Set) []spec.Proposal {
	wanted := Changed(current, drafted)
	out := []spec.Proposal{}
	for _, unit := range wanted.Sorted() {
		if !declared[unit] {
			continue
		}
		if sameValue(current, drafted, unit) {
			continue
		}
		out = append(out, spec.Proposal{
			Unit:     unit,
			Current:  Render(current, unit),
			Proposed: Render(drafted, unit),
		})
	}
	return out
}

// MergeProposals folds a batch of proposals into the ones already recorded:
// one proposal exists per unit and the newest wins, so a unit the writer
// proposes again replaces what the earlier summary said instead of piling up.
func MergeProposals(existing, proposed []spec.Proposal) []spec.Proposal {
	byUnit := make(map[string]spec.Proposal, len(existing)+len(proposed))
	for _, proposal := range existing {
		byUnit[proposal.Unit] = proposal
	}
	for _, proposal := range proposed {
		byUnit[proposal.Unit] = proposal
	}
	units := make([]string, 0, len(byUnit))
	for unit := range byUnit {
		units = append(units, unit)
	}
	sort.Strings(units)
	out := make([]spec.Proposal, 0, len(units))
	for _, unit := range units {
		out = append(out, byUnit[unit])
	}
	return out
}

// sameValue reports whether two specs say the same thing about a unit: the
// value is equal, or it differs only in the whitespace of its text.
func sameValue(current, drafted spec.SystemContextSpec, unit string) bool {
	left, hadLeft := Value(current, unit)
	right, hadRight := Value(drafted, unit)
	if hadLeft != hadRight {
		return false
	}
	if !hadLeft {
		return true
	}
	return reflect.DeepEqual(collapseValue(left), collapseValue(right))
}

// collapseValue removes the whitespace differences that carry no meaning: a
// text that was re-wrapped or re-indented is the text it was.
func collapseValue(value any) any {
	switch typed := value.(type) {
	case string:
		return collapseSpace(typed)
	case spec.Requirement:
		typed.Text = collapseSpace(typed.Text)
		return typed
	case spec.Interface:
		typed.Signature = collapseSpace(typed.Signature)
		typed.File = collapseSpace(typed.File)
		return typed
	case spec.Interaction:
		typed.Purpose = collapseSpace(typed.Purpose)
		typed.Channel = collapseSpace(typed.Channel)
		return typed
	}
	return value
}

func collapseSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// Render is the readable value of one unit of a spec: the intent paragraph,
// the text of a requirement, a one line interface, a one line interaction. A
// unit the spec does not hold renders empty, so a removal reads as the value it
// had on one side and nothing on the other.
func Render(contextSpec spec.SystemContextSpec, unit string) string {
	value, ok := Value(contextSpec, unit)
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return collapseSpace(typed)
	case spec.Requirement:
		return renderRequirement(typed)
	case spec.Interface:
		return renderInterface(typed)
	case spec.Interaction:
		return renderInteraction(typed)
	}
	return ""
}

func renderRequirement(requirement spec.Requirement) string {
	if requirement.Level == "" {
		return collapseSpace(requirement.Text)
	}
	return string(requirement.Level) + ": " + collapseSpace(requirement.Text)
}

func renderInterface(declared spec.Interface) string {
	line := declared.Name
	if declared.Kind != "" {
		line += " " + declared.Kind
	}
	if declared.Signature != "" {
		line += " " + collapseSpace(declared.Signature)
	}
	if declared.File != "" {
		line += " in " + declared.File
	}
	return line
}

func renderInteraction(interaction spec.Interaction) string {
	line := interaction.ID + ": " + interaction.Initiator + " -> " + interaction.Peer
	if interaction.Channel != "" {
		line += " over " + interaction.Channel
	}
	if len(interaction.Carries) > 0 {
		line += " carrying " + strings.Join(interaction.Carries, ", ")
	}
	if interaction.Purpose != "" {
		line += " for " + collapseSpace(interaction.Purpose)
	}
	if interaction.Forbidden {
		line += " (forbidden)"
	}
	return line
}

// Value returns the content of one unit of a spec, and whether the spec has it.
func Value(contextSpec spec.SystemContextSpec, unit string) (any, bool) {
	switch {
	case unit == UnitIntent:
		return contextSpec.Intent, contextSpec.Intent != ""
	case strings.HasPrefix(unit, PrefixRequirement):
		id := strings.TrimPrefix(unit, PrefixRequirement)
		for _, entry := range contextSpec.Requirements {
			if entry.ID == id {
				return spec.CanonicalRequirement(entry), true
			}
		}
	case strings.HasPrefix(unit, PrefixInterface):
		name := strings.TrimPrefix(unit, PrefixInterface)
		for _, entry := range contextSpec.Interfaces {
			if entry.Name == name {
				return spec.CanonicalInterface(entry), true
			}
		}
	case strings.HasPrefix(unit, PrefixInteraction):
		id := strings.TrimPrefix(unit, PrefixInteraction)
		for _, entry := range contextSpec.Interactions {
			if entry.ID == id {
				return spec.CanonicalInteraction(entry), true
			}
		}
	}
	return nil, false
}
