package spec

// The delta types are the wire form of "what changed". They live beside the
// spec types because a SpecChange carries one, and the pure Diff/Apply that
// compute and invert them live in abc/delta. The JSON spelling is the contract
// phase 8 mirrors in TypeScript, so it is flat, explicit and stably ordered.

const (
	OpAdded   = "added"
	OpRemoved = "removed"
	OpChanged = "changed"

	// Field lists name the fields of one keyed entry that differ. They are
	// sorted, and an entry that is added or removed has none: the whole entry
	// is in From or To.
	FieldLevel     = "level"
	FieldText      = "text"
	FieldCodeRefs  = "codeRefs"
	FieldKind      = "kind"
	FieldSignature = "signature"
	FieldFile      = "file"
	FieldLine      = "line"
	FieldID        = "codegraphId"
)

// FieldDelta is one scalar field that changed.
type FieldDelta struct {
	From string `json:"from"`

	To string `json:"to"`
}

// StringSetDelta is the difference of one set field (codeRefs, overlay,
// dependsOn, introduces): both lists are sorted, so the form is stable.
type StringSetDelta struct {
	Added []string `json:"added,omitempty"`

	Removed []string `json:"removed,omitempty"`
}

// RequirementDelta is one keyed requirement: added, removed, or changed with
// the fields that differ.
type RequirementDelta struct {
	Op string `json:"op"`

	ID string `json:"id"`

	From *Requirement `json:"from,omitempty"`

	To *Requirement `json:"to,omitempty"`

	Fields []string `json:"fields,omitempty"`
}

// InterfaceDelta is one keyed interface.
type InterfaceDelta struct {
	Op string `json:"op"`

	Name string `json:"name"`

	From *Interface `json:"from,omitempty"`

	To *Interface `json:"to,omitempty"`

	Fields []string `json:"fields,omitempty"`
}

// ObservedInterfaceDelta is one keyed observed interface. The code -> spec
// direction diffs the facts the index reports, not the spec.
type ObservedInterfaceDelta struct {
	Op string `json:"op"`

	Name string `json:"name"`

	From *ObservedInterface `json:"from,omitempty"`

	To *ObservedInterface `json:"to,omitempty"`

	Fields []string `json:"fields,omitempty"`
}

// ObservedDelta is the difference of two observed fact sets.
type ObservedDelta struct {
	Files *StringSetDelta `json:"files,omitempty"`

	Interfaces []ObservedInterfaceDelta `json:"interfaces,omitempty"`

	Fingerprint *FieldDelta `json:"fingerprint,omitempty"`
}

// Delta is the structured change of one spec, or of one observed fact set
// (Observed). A SpecToCode change carries the first, a CodeToSpec change the
// second.
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

	Observed *ObservedDelta `json:"observed,omitempty"`
}

// Empty reports whether a delta asks for nothing. A change with an empty delta
// is a spec that was reordered or rewritten to say the same thing; raising work
// for it would be the loop the two directions are careful to avoid.
func (d Delta) Empty() bool {
	if d.Intent != nil || d.Upstream != nil || d.Orchestrator != nil {
		return false
	}
	if !emptySet(d.Overlay) || !emptySet(d.DependsOn) || !emptySet(d.Introduces) || !emptySet(d.CodeRefs) {
		return false
	}
	if len(d.Requirements) > 0 || len(d.Interfaces) > 0 {
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

// Counts is the compact form a reader sees: how many entries were added,
// removed and changed.
type Counts struct {
	Added int `json:"added"`

	Removed int `json:"removed"`

	Changed int `json:"changed"`
}

// Count sums a delta. A set entry counts once, a field change counts once.
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
