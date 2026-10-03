package spec

import "sort"

// CanonicalSet returns a sorted, deduplicated copy of a string list. An empty
// list becomes nil, so two spellings of the same set hash alike: the CRD
// declares these fields as sets (x-kubernetes-list-type: set), where order and
// repetition carry no meaning.
func CanonicalSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// CanonicalRequirement sorts the code refs of one requirement, so the same
// requirement written twice hashes the same way.
func CanonicalRequirement(requirement Requirement) Requirement {
	out := requirement
	out.CodeRefs = CanonicalSet(requirement.CodeRefs)
	return out
}

// CanonicalRequirements orders requirements by id, which is the key the CRD
// declares them under.
func CanonicalRequirements(requirements []Requirement) []Requirement {
	if len(requirements) == 0 {
		return nil
	}
	out := make([]Requirement, 0, len(requirements))
	for _, requirement := range requirements {
		out = append(out, CanonicalRequirement(requirement))
	}
	sort.Slice(out, func(left, right int) bool { return out[left].ID < out[right].ID })
	return out
}

// CanonicalInterface normalizes one declared interface. It has no list field,
// so it is the identity; it exists so the delta code has one spelling to call.
func CanonicalInterface(declared Interface) Interface {
	return declared
}

// CanonicalInterfaces orders interfaces by name, which is the key the CRD
// declares them under.
func CanonicalInterfaces(interfaces []Interface) []Interface {
	if len(interfaces) == 0 {
		return nil
	}
	out := append([]Interface{}, interfaces...)
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out
}

// CanonicalObserved orders the observed facts: files sorted and deduplicated,
// interfaces sorted by name. The fingerprint is left alone, because it is the
// digest of exactly these facts.
func CanonicalObserved(observed ObservedFacts) ObservedFacts {
	out := observed
	out.Files = CanonicalSet(observed.Files)
	out.Interfaces = CanonicalObservedInterfaces(observed.Interfaces)
	return out
}

// CanonicalObservedInterfaces orders observed interfaces by name, the key the
// CRD declares them under.
func CanonicalObservedInterfaces(interfaces []ObservedInterface) []ObservedInterface {
	if len(interfaces) == 0 {
		return nil
	}
	out := append([]ObservedInterface{}, interfaces...)
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out
}

// Canonicalize orders every keyed list and set of a spec. The hash is taken
// over the canonical form, so reordering a list in a manifest is not a spec
// edit, exactly as the CRD's list keys say.
func Canonicalize(in SystemContextSpec) SystemContextSpec {
	out := in
	out.Requirements = CanonicalRequirements(in.Requirements)
	out.Interfaces = CanonicalInterfaces(in.Interfaces)
	out.CodeRefs = CanonicalSet(in.CodeRefs)
	out.Overlay = CanonicalSet(in.Overlay)
	out.DependsOn = CanonicalSet(in.DependsOn)
	out.Introduces = CanonicalSet(in.Introduces)
	if in.Arch != nil {
		arch := *in.Arch
		arch.Code = CanonicalSet(in.Arch.Code)
		arch.Overlay = CanonicalSet(in.Arch.Overlay)
		arch.DependsOn = CanonicalSet(in.Arch.DependsOn)
		arch.Introduces = CanonicalSet(in.Arch.Introduces)
		out.Arch = &arch
	}
	return out
}
