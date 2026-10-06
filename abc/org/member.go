package org

import (
	"sort"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/graph"
)

// State says how far a member's spec is connected to the root.
type State string

const (
	// StateResolved: the member has an architecture branch whose recorded
	// commit is the pinned commit or an ancestor of it.
	StateResolved State = "resolved"

	// StateStale: the member has an architecture branch but none of its commits
	// describes the pinned commit or an ancestor.
	StateStale State = "stale"

	// StateUnspecced: the member has no architecture branch.
	StateUnspecced State = "unspecced"

	// StateUninitialized: the submodule is not checked out, so its branches are
	// not readable.
	StateUninitialized State = "uninitialized"
)

// BranchRef is a reference to an orphan branch of a member: where it is, the
// tip it was read at, and the code commit that tip describes.
type BranchRef struct {
	Branch string `json:"branch"`

	Commit string `json:"commit,omitempty"`

	// IndexedCommit is the code commit the architecture says it was indexed at.
	IndexedCommit string `json:"indexedCommit,omitempty"`
}

// Member is one submodule of the org root and how its spec connects to the
// root. It holds references only: a member's spec is read from the member.
type Member struct {
	// Name is the member's repository name, the one its branches use.
	Name string `json:"name"`

	Path string `json:"path"`

	URL string `json:"url,omitempty"`

	// Branch is the tracking branch .gitmodules names, if any.
	Branch string `json:"branch,omitempty"`

	// CodeCommit is the gitlink: the commit of the member the root pins.
	CodeCommit string `json:"codeCommit"`

	Arch *BranchRef `json:"arch,omitempty"`

	Policy *BranchRef `json:"policy,omitempty"`

	State State `json:"state"`
}

// SortMembers orders members by path, the order every document of the root
// uses.
func SortMembers(members []Member) {
	sort.Slice(members, func(left, right int) bool { return members[left].Path < members[right].Path })
}

// GraphRefs is what the root's graph records of its members.
func GraphRefs(members []Member) []graph.MemberRef {
	out := make([]graph.MemberRef, 0, len(members))
	for _, member := range members {
		ref := graph.MemberRef{
			Name:       member.Name,
			Repository: member.Name,
			Path:       member.Path,
			URL:        member.URL,
			CodeCommit: member.CodeCommit,
			State:      string(member.State),
		}
		if member.Arch != nil {
			ref.ArchCommit = member.Arch.Commit
		}
		out = append(out, ref)
	}
	return out
}
