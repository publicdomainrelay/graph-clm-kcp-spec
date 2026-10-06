package org

import "fmt"

// MemberState is a member and what a look at its checkout found. The Member
// part is what the root's architecture branch records; the rest is the live
// state of the work tree and is never written to a branch.
type MemberState struct {
	Member

	Initialized bool `json:"initialized"`

	// Head is the commit the member checkout is at.
	Head string `json:"head,omitempty"`

	// Branch is the branch the checkout is on, empty when detached.
	Branch string `json:"checkedOut,omitempty"`

	Dirty bool `json:"dirty,omitempty"`

	// Ahead counts commits the checkout has that the pin does not, Behind the
	// other way round.
	Ahead int `json:"ahead,omitempty"`

	Behind int `json:"behind,omitempty"`

	// PinPresent is false when the pinned commit is not in the checkout, as in
	// a shallow clone.
	PinPresent bool `json:"pinPresent"`

	// Published means the pinned commit is on a remote branch of the member.
	Published bool `json:"published"`

	// Shallow means the member checkout is a shallow or single branch clone,
	// whose remote-tracking branches are too few to tell whether a pin is
	// published: it is unknown, not unpublished.
	Shallow bool `json:"shallow,omitempty"`
}

// Severity of a problem.
type Severity string

const (
	SeverityInfo Severity = "info"

	SeverityWarn Severity = "warn"

	SeverityError Severity = "error"
)

// Problem is something an agent or a person must act on before the root's
// state can be trusted or pushed.
type Problem struct {
	Member string `json:"member"`

	Severity Severity `json:"severity"`

	Code string `json:"code"`

	Message string `json:"message"`

	// Next is the command or action that clears it.
	Next string `json:"next,omitempty"`
}

// Problem codes.
const (
	CodeUninitialized = "uninitialized"

	CodeDirty = "dirty"

	CodeAhead = "ahead-of-pin"

	CodeBehind = "behind-pin"

	CodeUnpublished = "unpublished-pin"

	CodeUnverified = "unverified-pin"

	CodePinMissing = "pin-missing"

	CodeUnspecced = "unspecced"

	CodeStale = "stale-spec"
)

// Problems derives what is wrong with one member.
func Problems(state MemberState) []Problem {
	var out []Problem
	add := func(severity Severity, code, message, next string) {
		out = append(out, Problem{Member: state.Name, Severity: severity, Code: code, Message: message, Next: next})
	}
	if !state.Initialized {
		add(SeverityWarn, CodeUninitialized,
			fmt.Sprintf("%s is not checked out; its spec and policies cannot be read", state.Path),
			"git submodule update --init "+state.Path)
		return out
	}
	if !state.PinPresent {
		add(SeverityWarn, CodePinMissing,
			fmt.Sprintf("the pinned commit %s is not in %s", Short(state.CodeCommit), state.Path),
			"specctl org fetch")
	}
	if state.Dirty {
		add(SeverityWarn, CodeDirty,
			fmt.Sprintf("%s has uncommitted changes; a pointer bump would not include them", state.Path),
			"commit in "+state.Path+" first")
	}
	if state.Ahead > 0 {
		add(SeverityInfo, CodeAhead,
			fmt.Sprintf("%s is %d commit(s) ahead of the pin %s", state.Path, state.Ahead, Short(state.CodeCommit)),
			"specctl org bump "+state.Name)
	}
	if state.Behind > 0 {
		add(SeverityInfo, CodeBehind,
			fmt.Sprintf("%s is %d commit(s) behind the pin %s", state.Path, state.Behind, Short(state.CodeCommit)),
			"git submodule update "+state.Path)
	}
	if state.PinPresent && !state.Published && state.Shallow {
		add(SeverityInfo, CodeUnverified,
			fmt.Sprintf("%s is a shallow or single branch clone: whether the pin %s is published cannot be told from here", state.Path, Short(state.CodeCommit)),
			"git -C "+state.Path+" fetch --unshallow --all")
	}
	if state.PinPresent && !state.Published && !state.Shallow {
		add(SeverityError, CodeUnpublished,
			fmt.Sprintf("the pin %s of %s is on no remote branch: pushing the root would leave a dangling pointer", Short(state.CodeCommit), state.Path),
			"git -C "+state.Path+" push")
	}
	switch state.State {
	case StateUnspecced:
		add(SeverityInfo, CodeUnspecced,
			fmt.Sprintf("%s has no open-architecture branch", state.Name),
			"specctl up --repo "+state.Path)
	case StateStale:
		add(SeverityInfo, CodeStale,
			fmt.Sprintf("no architecture commit of %s describes the pin %s or an ancestor", state.Name, Short(state.CodeCommit)),
			"specctl up --repo "+state.Path)
	}
	return out
}
