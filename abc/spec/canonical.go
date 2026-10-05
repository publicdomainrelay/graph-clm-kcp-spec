package spec

import (
	"reflect"
	"sort"
)

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

func CanonicalRequirement(requirement Requirement) Requirement {
	out := requirement
	out.CodeRefs = CanonicalSet(requirement.CodeRefs)
	return out
}

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

func CanonicalInterface(declared Interface) Interface {
	return declared
}

func CanonicalInterfaces(interfaces []Interface) []Interface {
	if len(interfaces) == 0 {
		return nil
	}
	out := append([]Interface{}, interfaces...)
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out
}

func CanonicalInteraction(interaction Interaction) Interaction {
	out := interaction
	out.Carries = CanonicalSet(interaction.Carries)
	if out.Level == "" {
		out.Level = DefaultInteractionLevel
	}
	return out
}

func CanonicalInteractions(interactions []Interaction) []Interaction {
	if len(interactions) == 0 {
		return nil
	}
	out := make([]Interaction, 0, len(interactions))
	for _, interaction := range interactions {
		out = append(out, CanonicalInteraction(interaction))
	}
	sort.Slice(out, func(left, right int) bool { return out[left].ID < out[right].ID })
	return out
}

func CanonicalObserved(observed ObservedFacts) ObservedFacts {
	out := observed
	out.Files = CanonicalSet(observed.Files)
	out.Interfaces = CanonicalObservedInterfaces(observed.Interfaces)
	return out
}

func CanonicalObservedInterfaces(interfaces []ObservedInterface) []ObservedInterface {
	if len(interfaces) == 0 {
		return nil
	}
	out := append([]ObservedInterface{}, interfaces...)
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out
}

func SameDeclaredState(left, right SystemContextSpec) bool {
	left.CodeRefs = nil
	right.CodeRefs = nil
	return reflect.DeepEqual(Canonicalize(left), Canonicalize(right))
}

func Canonicalize(in SystemContextSpec) SystemContextSpec {
	out := in
	out.Requirements = CanonicalRequirements(in.Requirements)
	out.Interfaces = CanonicalInterfaces(in.Interfaces)
	out.Interactions = CanonicalInteractions(in.Interactions)
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
