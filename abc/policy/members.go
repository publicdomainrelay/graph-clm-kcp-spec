package policy

import (
	"slices"
	"strings"
)

type Member struct {
	Name string `json:"name"`

	URL string `json:"url"`

	Ref string `json:"ref,omitempty"`

	Path string `json:"path,omitempty"`

	Roles map[string]RoleBinding `json:"roles,omitempty"`

	Classifiers []string `json:"classifiers,omitempty"`

	TestGlobs []string `json:"testGlobs,omitempty"`
}

type MemberLock struct {
	Name string `json:"name"`

	URL string `json:"url"`

	Ref string `json:"ref,omitempty"`

	Commit string `json:"commit"`
}

type ReportMember struct {
	Name string `json:"name"`

	URL string `json:"url,omitempty"`

	Ref string `json:"ref,omitempty"`

	Commit string `json:"commit,omitempty"`
}

func SortMembers(members []ReportMember) {
	slices.SortStableFunc(members, func(left, right ReportMember) int {
		return strings.Compare(left.Name, right.Name)
	})
}

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
