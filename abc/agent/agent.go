// Package agent is the pure half of the model loop: what a model reads about
// one context, what it is asked to answer, and the strict reading of that
// answer. It holds no transport, no process and no file, so the parser and the
// budget are testable without a model.
package agent

import (
	"context"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

// Neighbor is one hop of the spec graph around a context: a vertex reached by
// an edge, in one direction, with the properties the graph model stores.
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

// CodeExcerpt is the raw output of one codegraph command, kept with its
// provenance so the model can tell a search result from a symbol's source.
type CodeExcerpt struct {
	Source string
	Text   string
}

// ContextBundle is everything the model reads about one context. It is a
// value: the builder fills it, the budget cuts it, the model answers it.
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

// DroppedRef is a code reference the model proposed that the observed facts do
// not carry. It is reported rather than silently kept, because a requirement
// anchored to nothing is the guess the whole design refuses.
type DroppedRef struct {
	Requirement string

	Ref string

	Reason string
}

// SpecDraft is a model answer after it has been read strictly: the prose that
// belongs in the model zone of the context document, and the machine fields
// that belong in the spec. Refs that did not resolve are in Dropped, never in
// Requirements.
type SpecDraft struct {
	Summary string

	Intent string

	Requirements []spec.Requirement

	Interfaces []spec.Interface

	Dropped []DroppedRef
}

// Agent summarizes code into a spec and realizes a spec into code. The CLI
// implementation shells out to a model; the scripted implementation answers
// from a scenario file; both satisfy this interface.
type Agent interface {
	Summarize(ctx context.Context, bundle ContextBundle) (SpecDraft, error)

	Realize(ctx context.Context, request RealizeRequest) (RealizeResult, error)
}

// RealizeRequest is one spec -> code unit of work: the tree the agent may edit,
// the delta it must work off, the spec to reach, and the command that gates it.
// The delta comes first on purpose: an agent is told what changed, never "here
// is the whole spec, guess what changed".
type RealizeRequest struct {
	Context string

	Repository string

	Dir string

	Delta spec.Delta

	FromSpec spec.SystemContextSpec

	ToSpec spec.SystemContextSpec

	Observed spec.ObservedFacts

	ContextDoc string

	// Verify is the command the repository must pass before the change lands.
	Verify []string

	Instruction string

	Budget int
}

// RealizeResult is what a realize attempt reports back: a one line summary, the
// tail of the agent output, and the files it touched.
type RealizeResult struct {
	Summary string

	Log string

	Files []string
}
