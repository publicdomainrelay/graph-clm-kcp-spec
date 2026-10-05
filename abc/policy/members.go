package policy

import (
	"slices"
	"strings"
)

// Member is another repository a policy library reads besides its own. The
// ArchitectureModel is built over every member, so a pack rule sees a flow
// whose two ends live in two repositories: the provider that reaches into a
// guest and the bidder that drives it are not always one checkout.
type Member struct {
	Name string `json:"name"`

	URL string `json:"url"`

	Ref string `json:"ref,omitempty"`

	// Path limits the member to a subdirectory of its checkout. Components are
	// named <name>/<...> with or without it.
	Path string `json:"path,omitempty"`

	// Roles bind the member's own files to the abstract roles a pack reads,
	// merged with the importing library's roles. Only the binding changes
	// between repositories; the pack does not.
	Roles map[string]RoleBinding `json:"roles,omitempty"`

	// Classifiers names classifier packs under the library's classifiers/
	// directory that apply to the member's code. Empty reads the member
	// checkout's own classifiers/ directory.
	Classifiers []string `json:"classifiers,omitempty"`

	// TestGlobs overrides the library's test globs for the member.
	TestGlobs []string `json:"testGlobs,omitempty"`
}

// MemberLock is the pin a build records for one member: the commit its ref
// resolved to when the library was last built.
type MemberLock struct {
	Name string `json:"name"`

	URL string `json:"url"`

	Ref string `json:"ref,omitempty"`

	Commit string `json:"commit"`
}

// ReportMember is one member of an evaluation, with the commit the model was
// built from: a report is reproducible only if the member's code is pinned.
type ReportMember struct {
	Name string `json:"name"`

	URL string `json:"url,omitempty"`

	Ref string `json:"ref,omitempty"`

	Commit string `json:"commit,omitempty"`
}

// SortMembers puts a report's member pins in name order, so two runs of the
// same evaluation print the same lines.
func SortMembers(members []ReportMember) {
	slices.SortStableFunc(members, func(left, right ReportMember) int {
		return strings.Compare(left.Name, right.Name)
	})
}

// MemberNames lists the members a library declares, sorted, so a report and a
// lock agree on the order.
func (l PolicyLibrary) MemberNames() []string {
	out := make([]string, 0, len(l.Members))
	for _, member := range l.Members {
		out = append(out, member.Name)
	}
	slices.Sort(out)
	return out
}

func (l PolicyLibrary) Member(name string) (Member, bool) {
	for _, member := range l.Members {
		if member.Name == name {
			return member, true
		}
	}
	return Member{}, false
}
