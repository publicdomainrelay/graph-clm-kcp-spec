package delta

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func Diff(old, new spec.SystemContextSpec) spec.Delta {
	out := spec.Delta{}
	if old.Intent != new.Intent {
		out.Intent = &spec.FieldDelta{From: old.Intent, To: new.Intent}
	}
	if old.Upstream != new.Upstream {
		out.Upstream = &spec.FieldDelta{From: old.Upstream, To: new.Upstream}
	}
	if old.Orchestrator != new.Orchestrator {
		out.Orchestrator = &spec.FieldDelta{From: old.Orchestrator, To: new.Orchestrator}
	}
	out.Overlay = diffSet(old.Overlay, new.Overlay)
	out.DependsOn = diffSet(old.DependsOn, new.DependsOn)
	out.Introduces = diffSet(old.Introduces, new.Introduces)
	out.CodeRefs = diffSet(old.CodeRefs, new.CodeRefs)
	out.Requirements = diffRequirements(old.Requirements, new.Requirements)
	out.Interfaces = diffInterfaces(old.Interfaces, new.Interfaces)
	out.Interactions = diffInteractions(old.Interactions, new.Interactions)
	return out
}

func DiffObserved(old, new spec.ObservedFacts) spec.Delta {
	files := diffSet(old.Files, new.Files)
	interfaces := diffObservedInterfaces(old.Interfaces, new.Interfaces)
	fingerprint := (*spec.FieldDelta)(nil)
	if old.Fingerprint != new.Fingerprint {
		fingerprint = &spec.FieldDelta{From: old.Fingerprint, To: new.Fingerprint}
	}
	if files == nil && len(interfaces) == 0 && fingerprint == nil {
		return spec.Delta{}
	}
	return spec.Delta{Observed: &spec.ObservedDelta{
		Files:       files,
		Interfaces:  interfaces,
		Fingerprint: fingerprint,
	}}
}

func Apply(base spec.SystemContextSpec, change spec.Delta) spec.SystemContextSpec {
	out := base
	if change.Intent != nil {
		out.Intent = change.Intent.To
	}
	if change.Upstream != nil {
		out.Upstream = change.Upstream.To
	}
	if change.Orchestrator != nil {
		out.Orchestrator = change.Orchestrator.To
	}
	if change.Overlay != nil {
		out.Overlay = applySet(base.Overlay, *change.Overlay)
	}
	if change.DependsOn != nil {
		out.DependsOn = applySet(base.DependsOn, *change.DependsOn)
	}
	if change.Introduces != nil {
		out.Introduces = applySet(base.Introduces, *change.Introduces)
	}
	if change.CodeRefs != nil {
		out.CodeRefs = applySet(base.CodeRefs, *change.CodeRefs)
	}
	if len(change.Requirements) > 0 {
		out.Requirements = applyRequirements(base.Requirements, change.Requirements)
	}
	if len(change.Interfaces) > 0 {
		out.Interfaces = applyInterfaces(base.Interfaces, change.Interfaces)
	}
	if len(change.Interactions) > 0 {
		out.Interactions = applyInteractions(base.Interactions, change.Interactions)
	}
	return spec.Canonicalize(out)
}

func ApplyObserved(base spec.ObservedFacts, change spec.Delta) spec.ObservedFacts {
	if change.Observed == nil {
		return spec.CanonicalObserved(base)
	}
	out := base
	if change.Observed.Files != nil {
		out.Files = applySet(base.Files, *change.Observed.Files)
	}
	if len(change.Observed.Interfaces) > 0 {
		out.Interfaces = applyObservedInterfaces(base.Interfaces, change.Observed.Interfaces)
	}
	if change.Observed.Fingerprint != nil {
		out.Fingerprint = change.Observed.Fingerprint.To
	}
	return spec.CanonicalObserved(out)
}

func Summary(change spec.Delta) string {
	counts := change.Count()
	parts := []string{}
	if counts.Added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", counts.Added))
	}
	if counts.Removed > 0 {
		parts = append(parts, fmt.Sprintf("-%d", counts.Removed))
	}
	if counts.Changed > 0 {
		parts = append(parts, fmt.Sprintf("~%d", counts.Changed))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

// Details names every entry the delta adds, changes and removes, one line each
// and in a stable order, so an operator reads what an apply will do by id
// before it lands rather than a count that hides a removal.
func Details(change spec.Delta) []string {
	out := []string{}
	if change.Intent != nil {
		out = append(out, "~ intent")
	}
	if change.Upstream != nil {
		out = append(out, "~ upstream")
	}
	if change.Orchestrator != nil {
		out = append(out, "~ orchestrator")
	}
	sets := []struct {
		field string
		delta *spec.StringSetDelta
	}{
		{"overlay", change.Overlay},
		{"dependsOn", change.DependsOn},
		{"introduces", change.Introduces},
		{"codeRefs", change.CodeRefs},
	}
	for _, set := range sets {
		if set.delta == nil {
			continue
		}
		for _, value := range set.delta.Added {
			out = append(out, "+ "+set.field+" "+value)
		}
		for _, value := range set.delta.Removed {
			out = append(out, "- "+set.field+" "+value)
		}
	}
	for _, requirement := range change.Requirements {
		out = append(out, requirementLine(requirement))
	}
	for _, declared := range change.Interfaces {
		out = append(out, interfaceLine(declared))
	}
	for _, interaction := range change.Interactions {
		out = append(out, interactionLine(interaction))
	}
	return out
}

// RemovedInteractionIDs lists, sorted, the interaction ids the delta deletes.
// It is the set an apply has to see named explicitly before it will remove.
func RemovedInteractionIDs(change spec.Delta) []string {
	out := []string{}
	for _, interaction := range change.Interactions {
		if interaction.Op == spec.OpRemoved {
			out = append(out, interaction.ID)
		}
	}
	sort.Strings(out)
	return out
}

// RemovedRequirementIDs lists, sorted, the requirement ids the delta deletes.
// It is the set an apply has to see named explicitly before it will remove.
func RemovedRequirementIDs(change spec.Delta) []string {
	out := []string{}
	for _, requirement := range change.Requirements {
		if requirement.Op == spec.OpRemoved {
			out = append(out, requirement.ID)
		}
	}
	sort.Strings(out)
	return out
}

func requirementLine(change spec.RequirementDelta) string {
	switch change.Op {
	case spec.OpAdded:
		return "+ " + change.ID
	case spec.OpRemoved:
		return "- " + change.ID
	default:
		return "~ " + change.ID + fieldSuffix(change.Fields)
	}
}

func interactionLine(change spec.InteractionDelta) string {
	switch change.Op {
	case spec.OpAdded:
		return "+ interaction " + change.ID
	case spec.OpRemoved:
		return "- interaction " + change.ID
	default:
		return "~ interaction " + change.ID + fieldSuffix(change.Fields)
	}
}

func interfaceLine(change spec.InterfaceDelta) string {
	switch change.Op {
	case spec.OpAdded:
		return "+ interface " + change.Name
	case spec.OpRemoved:
		return "- interface " + change.Name
	default:
		return "~ interface " + change.Name + fieldSuffix(change.Fields)
	}
}

func fieldSuffix(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return " (" + strings.Join(fields, ", ") + ")"
}

func diffSet(old, new []string) *spec.StringSetDelta {
	before := spec.CanonicalSet(old)
	after := spec.CanonicalSet(new)
	if reflect.DeepEqual(before, after) {
		return nil
	}
	have := map[string]bool{}
	for _, value := range before {
		have[value] = true
	}
	want := map[string]bool{}
	for _, value := range after {
		want[value] = true
	}
	out := &spec.StringSetDelta{}
	for _, value := range after {
		if !have[value] {
			out.Added = append(out.Added, value)
		}
	}
	for _, value := range before {
		if !want[value] {
			out.Removed = append(out.Removed, value)
		}
	}
	return out
}

func applySet(base []string, change spec.StringSetDelta) []string {
	values := map[string]bool{}
	for _, value := range base {
		values[value] = true
	}
	for _, value := range change.Removed {
		delete(values, value)
	}
	for _, value := range change.Added {
		values[value] = true
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return spec.CanonicalSet(out)
}

func diffRequirements(old, new []spec.Requirement) []spec.RequirementDelta {
	before := indexRequirements(old)
	after := indexRequirements(new)
	keys := unionKeys(before, after)
	out := []spec.RequirementDelta{}
	for _, id := range keys {
		from, hadBefore := before[id]
		to, hasAfter := after[id]
		switch {
		case !hadBefore:
			entry := spec.CanonicalRequirement(to)
			out = append(out, spec.RequirementDelta{Op: spec.OpAdded, ID: id, To: &entry})
		case !hasAfter:
			entry := spec.CanonicalRequirement(from)
			out = append(out, spec.RequirementDelta{Op: spec.OpRemoved, ID: id, From: &entry})
		default:
			canonicalFrom := spec.CanonicalRequirement(from)
			canonicalTo := spec.CanonicalRequirement(to)
			if reflect.DeepEqual(canonicalFrom, canonicalTo) {
				continue
			}
			out = append(out, spec.RequirementDelta{
				Op:     spec.OpChanged,
				ID:     id,
				From:   &canonicalFrom,
				To:     &canonicalTo,
				Fields: changedRequirementFields(canonicalFrom, canonicalTo),
			})
		}
	}
	return out
}

func applyRequirements(base []spec.Requirement, change []spec.RequirementDelta) []spec.Requirement {
	entries := indexRequirements(base)
	for _, entry := range change {
		switch entry.Op {
		case spec.OpAdded, spec.OpChanged:
			if entry.To != nil {
				entries[entry.ID] = *entry.To
			}
		case spec.OpRemoved:
			delete(entries, entry.ID)
		}
	}
	return spec.CanonicalRequirements(mapValues(entries))
}

func changedRequirementFields(from, to spec.Requirement) []string {
	fields := []string{}
	if from.Level != to.Level {
		fields = append(fields, spec.FieldLevel)
	}
	if from.Text != to.Text {
		fields = append(fields, spec.FieldText)
	}
	if !reflect.DeepEqual(from.CodeRefs, to.CodeRefs) {
		fields = append(fields, spec.FieldCodeRefs)
	}
	sort.Strings(fields)
	return fields
}

func diffInterfaces(old, new []spec.Interface) []spec.InterfaceDelta {
	before := indexInterfaces(old)
	after := indexInterfaces(new)
	keys := unionKeys(before, after)
	out := []spec.InterfaceDelta{}
	for _, name := range keys {
		from, hadBefore := before[name]
		to, hasAfter := after[name]
		switch {
		case !hadBefore:
			entry := to
			out = append(out, spec.InterfaceDelta{Op: spec.OpAdded, Name: name, To: &entry})
		case !hasAfter:
			entry := from
			out = append(out, spec.InterfaceDelta{Op: spec.OpRemoved, Name: name, From: &entry})
		default:
			if reflect.DeepEqual(from, to) {
				continue
			}
			out = append(out, spec.InterfaceDelta{
				Op:     spec.OpChanged,
				Name:   name,
				From:   &from,
				To:     &to,
				Fields: changedInterfaceFields(from, to),
			})
		}
	}
	return out
}

func applyInterfaces(base []spec.Interface, change []spec.InterfaceDelta) []spec.Interface {
	entries := indexInterfaces(base)
	for _, entry := range change {
		switch entry.Op {
		case spec.OpAdded, spec.OpChanged:
			if entry.To != nil {
				entries[entry.Name] = *entry.To
			}
		case spec.OpRemoved:
			delete(entries, entry.Name)
		}
	}
	return spec.CanonicalInterfaces(mapValues(entries))
}

func changedInterfaceFields(from, to spec.Interface) []string {
	fields := []string{}
	if from.Kind != to.Kind {
		fields = append(fields, spec.FieldKind)
	}
	if from.Signature != to.Signature {
		fields = append(fields, spec.FieldSignature)
	}
	if from.File != to.File {
		fields = append(fields, spec.FieldFile)
	}
	sort.Strings(fields)
	return fields
}

func diffInteractions(old, new []spec.Interaction) []spec.InteractionDelta {
	before := indexInteractions(old)
	after := indexInteractions(new)
	keys := unionKeys(before, after)
	out := []spec.InteractionDelta{}
	for _, id := range keys {
		from, hadBefore := before[id]
		to, hasAfter := after[id]
		switch {
		case !hadBefore:
			entry := spec.CanonicalInteraction(to)
			out = append(out, spec.InteractionDelta{Op: spec.OpAdded, ID: id, To: &entry})
		case !hasAfter:
			entry := spec.CanonicalInteraction(from)
			out = append(out, spec.InteractionDelta{Op: spec.OpRemoved, ID: id, From: &entry})
		default:
			canonicalFrom := spec.CanonicalInteraction(from)
			canonicalTo := spec.CanonicalInteraction(to)
			if reflect.DeepEqual(canonicalFrom, canonicalTo) {
				continue
			}
			out = append(out, spec.InteractionDelta{
				Op:     spec.OpChanged,
				ID:     id,
				From:   &canonicalFrom,
				To:     &canonicalTo,
				Fields: changedInteractionFields(canonicalFrom, canonicalTo),
			})
		}
	}
	return out
}

func applyInteractions(base []spec.Interaction, change []spec.InteractionDelta) []spec.Interaction {
	entries := indexInteractions(base)
	for _, entry := range change {
		switch entry.Op {
		case spec.OpAdded, spec.OpChanged:
			if entry.To != nil {
				entries[entry.ID] = *entry.To
			}
		case spec.OpRemoved:
			delete(entries, entry.ID)
		}
	}
	return spec.CanonicalInteractions(mapValues(entries))
}

func changedInteractionFields(from, to spec.Interaction) []string {
	fields := []string{}
	if from.Peer != to.Peer {
		fields = append(fields, spec.FieldPeer)
	}
	if from.Initiator != to.Initiator {
		fields = append(fields, spec.FieldInitiator)
	}
	if from.Channel != to.Channel {
		fields = append(fields, spec.FieldChannel)
	}
	if !reflect.DeepEqual(from.Carries, to.Carries) {
		fields = append(fields, spec.FieldCarries)
	}
	if from.Purpose != to.Purpose {
		fields = append(fields, spec.FieldPurpose)
	}
	if from.Level != to.Level {
		fields = append(fields, spec.FieldLevel)
	}
	if from.Forbidden != to.Forbidden {
		fields = append(fields, spec.FieldForbidden)
	}
	sort.Strings(fields)
	return fields
}

func indexInteractions(interactions []spec.Interaction) map[string]spec.Interaction {
	out := make(map[string]spec.Interaction, len(interactions))
	for _, interaction := range interactions {
		out[interaction.ID] = interaction
	}
	return out
}

func diffObservedInterfaces(old, new []spec.ObservedInterface) []spec.ObservedInterfaceDelta {
	before := indexObservedInterfaces(old)
	after := indexObservedInterfaces(new)
	keys := unionKeys(before, after)
	out := []spec.ObservedInterfaceDelta{}
	for _, name := range keys {
		from, hadBefore := before[name]
		to, hasAfter := after[name]
		switch {
		case !hadBefore:
			entry := to
			out = append(out, spec.ObservedInterfaceDelta{Op: spec.OpAdded, Name: name, To: &entry})
		case !hasAfter:
			entry := from
			out = append(out, spec.ObservedInterfaceDelta{Op: spec.OpRemoved, Name: name, From: &entry})
		default:
			if reflect.DeepEqual(from, to) {
				continue
			}
			out = append(out, spec.ObservedInterfaceDelta{
				Op:     spec.OpChanged,
				Name:   name,
				From:   &from,
				To:     &to,
				Fields: changedObservedFields(from, to),
			})
		}
	}
	return out
}

func applyObservedInterfaces(base []spec.ObservedInterface, change []spec.ObservedInterfaceDelta) []spec.ObservedInterface {
	entries := indexObservedInterfaces(base)
	for _, entry := range change {
		switch entry.Op {
		case spec.OpAdded, spec.OpChanged:
			if entry.To != nil {
				entries[entry.Name] = *entry.To
			}
		case spec.OpRemoved:
			delete(entries, entry.Name)
		}
	}
	return spec.CanonicalObservedInterfaces(mapValues(entries))
}

func changedObservedFields(from, to spec.ObservedInterface) []string {
	fields := []string{}
	if from.Kind != to.Kind {
		fields = append(fields, spec.FieldKind)
	}
	if from.Signature != to.Signature {
		fields = append(fields, spec.FieldSignature)
	}
	if from.File != to.File {
		fields = append(fields, spec.FieldFile)
	}
	if from.Line != to.Line {
		fields = append(fields, spec.FieldLine)
	}
	if from.CodegraphID != to.CodegraphID {
		fields = append(fields, spec.FieldID)
	}
	sort.Strings(fields)
	return fields
}

func indexRequirements(requirements []spec.Requirement) map[string]spec.Requirement {
	out := make(map[string]spec.Requirement, len(requirements))
	for _, requirement := range requirements {
		out[requirement.ID] = requirement
	}
	return out
}

func indexInterfaces(interfaces []spec.Interface) map[string]spec.Interface {
	out := make(map[string]spec.Interface, len(interfaces))
	for _, declared := range interfaces {
		out[declared.Name] = declared
	}
	return out
}

func indexObservedInterfaces(interfaces []spec.ObservedInterface) map[string]spec.ObservedInterface {
	out := make(map[string]spec.ObservedInterface, len(interfaces))
	for _, observed := range interfaces {
		out[observed.Name] = observed
	}
	return out
}

func unionKeys[V any](left, right map[string]V) []string {
	seen := map[string]bool{}
	keys := make([]string, 0, len(left)+len(right))
	for key := range left {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range right {
		if seen[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mapValues[V any](values map[string]V) []V {
	out := make([]V, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
