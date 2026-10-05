package policy

import (
	"slices"
	"sort"
)

const (
	ArchitectureModelKind = "ArchitectureModel"

	RoleLabel = Group + "/role"

	RoleUnknown = "unknown"
)

type ModelSource string

const (
	SourceDeclared ModelSource = "declared"
	SourceObserved ModelSource = "observed"
	SourceBoth     ModelSource = "both"
)

type ModelComponent struct {
	Name string `json:"name"`

	Roles []string `json:"roles,omitempty"`

	// Globs are the binding globs of the component's roles, so a rule that
	// reads objects outside the model -- a CodeDiff, for one -- can tell whose
	// file a path is.
	Globs []string `json:"globs,omitempty"`

	Context string `json:"context,omitempty"`

	Source ModelSource `json:"source"`
}

type ModelFlow struct {
	From string `json:"from"`

	To string `json:"to"`

	Initiator string `json:"initiator,omitempty"`

	Channel string `json:"channel,omitempty"`

	Carries []string `json:"carries,omitempty"`

	Purpose string `json:"purpose,omitempty"`

	// Level is the declared MUST, SHOULD or MAY of the interaction this flow
	// came from. An observed-only flow carries none.
	Level string `json:"level,omitempty"`

	// Forbidden marks a declared "must never" flow. A matching declared or
	// observed flow is the conformance violation.
	Forbidden bool `json:"forbidden,omitempty"`

	Source ModelSource `json:"source"`

	Evidence []string `json:"evidence,omitempty"`
}

type ModelTrigger struct {
	From string `json:"from"`

	To string `json:"to"`
}

type ArchitectureModelSpec struct {
	Repository string `json:"repository,omitempty"`

	// Roles are the binding's role names: a portable rule can tell the model's
	// roles apart from a component named after its context.
	Roles []string `json:"roles,omitempty"`

	// Vocabulary is the binding's vocabulary, so a template reads the classes
	// its pack declares without reading repository names.
	Vocabulary Vocabulary `json:"vocabulary,omitempty"`

	Components []ModelComponent `json:"components"`

	Effects []Effect `json:"effects"`

	Flows []ModelFlow `json:"flows"`

	Triggers []ModelTrigger `json:"triggers"`
}

type ArchitectureModel struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata ObjectMeta `json:"metadata"`

	Spec ArchitectureModelSpec `json:"spec"`
}

func (m *ArchitectureModel) Sort() {
	sort.SliceStable(m.Spec.Components, func(left, right int) bool {
		return m.Spec.Components[left].Name < m.Spec.Components[right].Name
	})
	sort.SliceStable(m.Spec.Flows, func(left, right int) bool {
		return flowKey(m.Spec.Flows[left]) < flowKey(m.Spec.Flows[right])
	})
	sort.SliceStable(m.Spec.Triggers, func(left, right int) bool {
		a, b := m.Spec.Triggers[left], m.Spec.Triggers[right]
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	SortEffects(m.Spec.Effects)
}

func flowKey(flow ModelFlow) string {
	return flow.From + "\x00" + flow.To + "\x00" + flow.Initiator + "\x00" +
		flow.Channel + "\x00" + flow.Purpose
}

func (m ArchitectureModel) ComponentsWithRole(role string) []ModelComponent {
	out := []ModelComponent{}
	for _, component := range m.Spec.Components {
		if slices.Contains(component.Roles, role) {
			out = append(out, component)
		}
	}
	return out
}

func (m ArchitectureModel) EffectsOfComponent(component string, kind EffectKind) []Effect {
	out := []Effect{}
	for _, effect := range m.Spec.Effects {
		if effect.Component != component {
			continue
		}
		if kind != "" && effect.Kind != kind {
			continue
		}
		out = append(out, effect)
	}
	return out
}

type FlowFilter struct {
	From string `json:"from,omitempty"`

	To string `json:"to,omitempty"`

	Initiator string `json:"initiator,omitempty"`

	Channel string `json:"channel,omitempty"`

	Purpose string `json:"purpose,omitempty"`

	Carries string `json:"carries,omitempty"`

	Source ModelSource `json:"source,omitempty"`
}

func (f FlowFilter) Matches(flow ModelFlow) bool {
	if f.From != "" && flow.From != f.From {
		return false
	}
	if f.To != "" && flow.To != f.To {
		return false
	}
	if f.Initiator != "" && flow.Initiator != f.Initiator {
		return false
	}
	if f.Channel != "" && flow.Channel != f.Channel {
		return false
	}
	if f.Purpose != "" && flow.Purpose != f.Purpose {
		return false
	}
	if f.Source != "" && flow.Source != f.Source {
		return false
	}
	if f.Carries != "" && !containsString(flow.Carries, f.Carries) {
		return false
	}
	return true
}

func (m ArchitectureModel) FlowsWhere(filter FlowFilter) []ModelFlow {
	out := []ModelFlow{}
	for _, flow := range m.Spec.Flows {
		if filter.Matches(flow) {
			out = append(out, flow)
		}
	}
	return out
}

func (m ArchitectureModel) TriggeredBy(effect string) []ModelTrigger {
	out := []ModelTrigger{}
	for _, trigger := range m.Spec.Triggers {
		if trigger.To == effect {
			out = append(out, trigger)
		}
	}
	return out
}

func Declared(flow ModelFlow) bool {
	return flow.Source == SourceDeclared || flow.Source == SourceBoth
}

func Observed(flow ModelFlow) bool {
	return flow.Source == SourceObserved || flow.Source == SourceBoth
}

func Forbidden(flow ModelFlow) bool {
	return flow.Forbidden
}

// SameShape reports whether two flows are the same directed shape: the same
// initiator, the same role it acts on, the same channel and the same purpose.
// The level, the forbidden marker, the carried payloads, the source and the
// evidence are not part of it. It is what a forbidden marker is matched
// against, and it is the Go twin of lib.specd's same_flow.
func SameShape(left, right ModelFlow) bool {
	return left.Initiator == right.Initiator &&
		actedOn(left) == actedOn(right) &&
		left.Channel == right.Channel &&
		left.Purpose == right.Purpose
}

// actedOn names the endpoint a flow acts on: the one that is not the initiator.
// A flow's from is the context that declared it and its to is the peer, so the
// acted-on role is not always the same endpoint.
func actedOn(flow ModelFlow) string {
	switch flow.Initiator {
	case flow.From:
		return flow.To
	case flow.To:
		return flow.From
	}
	return ""
}
