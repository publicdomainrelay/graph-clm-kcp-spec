package agent

import (
	"context"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

type Neighbor struct {
	Edge      string
	Direction string
	Label     string
	Name      string
	Props     map[string]string
}

const (
	DirectionOut = "out"
	DirectionIn  = "in"
)

type CodeExcerpt struct {
	Source string
	Text   string
}

type ContextBundle struct {
	Context string

	Repository string

	Spec spec.SystemContextSpec

	Observed spec.ObservedFacts

	ContextDoc string

	Neighbors []Neighbor

	CodeExcerpts []CodeExcerpt

	Budget int
}

type DroppedRef struct {
	Requirement string

	Ref string

	Reason string
}

type SpecDraft struct {
	Summary string

	Intent string

	Requirements []spec.Requirement

	Interfaces []spec.Interface

	Dropped []DroppedRef

	Warnings []string
}

type Agent interface {
	Summarize(ctx context.Context, bundle ContextBundle) (SpecDraft, error)

	Realize(ctx context.Context, request RealizeRequest) (RealizeResult, error)
}

type RealizeMember struct {
	Context string

	Change string

	Delta spec.Delta

	FromSpec spec.SystemContextSpec

	ToSpec spec.SystemContextSpec

	Observed spec.ObservedFacts

	ContextDoc string
}

type RealizeRequest struct {
	Context string

	Change string

	Repository string

	Dir string

	Delta spec.Delta

	FromSpec spec.SystemContextSpec

	ToSpec spec.SystemContextSpec

	Observed spec.ObservedFacts

	ContextDoc string

	Verify []string

	Instruction string

	Budget int

	Members []RealizeMember
}

func (r RealizeRequest) Batch() []RealizeMember {
	if len(r.Members) > 0 {
		return r.Members
	}
	return []RealizeMember{{
		Context:    r.Context,
		Change:     r.Change,
		Delta:      r.Delta,
		FromSpec:   r.FromSpec,
		ToSpec:     r.ToSpec,
		Observed:   r.Observed,
		ContextDoc: r.ContextDoc,
	}}
}

type RealizeResult struct {
	Summary string

	Log string

	Files []string
}
