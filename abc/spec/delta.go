package spec

const (
	OpAdded   = "added"
	OpRemoved = "removed"
	OpChanged = "changed"

	FieldLevel     = "level"
	FieldText      = "text"
	FieldCodeRefs  = "codeRefs"
	FieldKind      = "kind"
	FieldSignature = "signature"
	FieldFile      = "file"
	FieldLine      = "line"
	FieldID        = "codegraphId"

	FieldPeer      = "peer"
	FieldInitiator = "initiator"
	FieldChannel   = "channel"
	FieldCarries   = "carries"
	FieldPurpose   = "purpose"
	FieldForbidden = "forbidden"
)

type FieldDelta struct {
	From string `json:"from"`

	To string `json:"to"`
}

type StringSetDelta struct {
	Added []string `json:"added,omitempty"`

	Removed []string `json:"removed,omitempty"`
}

type RequirementDelta struct {
	Op string `json:"op"`

	ID string `json:"id"`

	From *Requirement `json:"from,omitempty"`

	To *Requirement `json:"to,omitempty"`

	Fields []string `json:"fields,omitempty"`
}

type InterfaceDelta struct {
	Op string `json:"op"`

	Name string `json:"name"`

	From *Interface `json:"from,omitempty"`

	To *Interface `json:"to,omitempty"`

	Fields []string `json:"fields,omitempty"`
}

type InteractionDelta struct {
	Op string `json:"op"`

	ID string `json:"id"`

	From *Interaction `json:"from,omitempty"`

	To *Interaction `json:"to,omitempty"`

	Fields []string `json:"fields,omitempty"`
}

type ObservedInterfaceDelta struct {
	Op string `json:"op"`

	Name string `json:"name"`

	From *ObservedInterface `json:"from,omitempty"`

	To *ObservedInterface `json:"to,omitempty"`

	Fields []string `json:"fields,omitempty"`
}

type ObservedDelta struct {
	Files *StringSetDelta `json:"files,omitempty"`

	Interfaces []ObservedInterfaceDelta `json:"interfaces,omitempty"`

	Fingerprint *FieldDelta `json:"fingerprint,omitempty"`
}

type Delta struct {
	Intent *FieldDelta `json:"intent,omitempty"`

	Upstream *FieldDelta `json:"upstream,omitempty"`

	Orchestrator *FieldDelta `json:"orchestrator,omitempty"`

	Overlay *StringSetDelta `json:"overlay,omitempty"`

	DependsOn *StringSetDelta `json:"dependsOn,omitempty"`

	Introduces *StringSetDelta `json:"introduces,omitempty"`

	CodeRefs *StringSetDelta `json:"codeRefs,omitempty"`

	Requirements []RequirementDelta `json:"requirements,omitempty"`

	Interfaces []InterfaceDelta `json:"interfaces,omitempty"`

	Interactions []InteractionDelta `json:"interactions,omitempty"`

	Observed *ObservedDelta `json:"observed,omitempty"`
}

func (d Delta) Empty() bool {
	if d.Intent != nil || d.Upstream != nil || d.Orchestrator != nil {
		return false
	}
	if !emptySet(d.Overlay) || !emptySet(d.DependsOn) || !emptySet(d.Introduces) || !emptySet(d.CodeRefs) {
		return false
	}
	if len(d.Requirements) > 0 || len(d.Interfaces) > 0 || len(d.Interactions) > 0 {
		return false
	}
	if d.Observed != nil {
		if !emptySet(d.Observed.Files) || len(d.Observed.Interfaces) > 0 || d.Observed.Fingerprint != nil {
			return false
		}
	}
	return true
}

func emptySet(delta *StringSetDelta) bool {
	return delta == nil || (len(delta.Added) == 0 && len(delta.Removed) == 0)
}

type Counts struct {
	Added int `json:"added"`

	Removed int `json:"removed"`

	Changed int `json:"changed"`
}

func (d Delta) Count() Counts {
	counts := Counts{}
	fields := []*FieldDelta{d.Intent, d.Upstream, d.Orchestrator}
	for _, field := range fields {
		if field != nil {
			counts.Changed++
		}
	}
	sets := []*StringSetDelta{d.Overlay, d.DependsOn, d.Introduces, d.CodeRefs}
	for _, set := range sets {
		if set == nil {
			continue
		}
		counts.Added += len(set.Added)
		counts.Removed += len(set.Removed)
	}
	for _, requirement := range d.Requirements {
		counts.add(requirement.Op)
	}
	for _, declared := range d.Interfaces {
		counts.add(declared.Op)
	}
	for _, interaction := range d.Interactions {
		counts.add(interaction.Op)
	}
	if d.Observed != nil {
		if d.Observed.Files != nil {
			counts.Added += len(d.Observed.Files.Added)
			counts.Removed += len(d.Observed.Files.Removed)
		}
		for _, observedInterface := range d.Observed.Interfaces {
			counts.add(observedInterface.Op)
		}
		if d.Observed.Fingerprint != nil {
			counts.Changed++
		}
	}
	return counts
}

func (c *Counts) add(op string) {
	switch op {
	case OpAdded:
		c.Added++
	case OpRemoved:
		c.Removed++
	case OpChanged:
		c.Changed++
	}
}
