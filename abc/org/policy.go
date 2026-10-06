package org

import (
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/glob"
)

// PolicyMembers derives the members of the combined ArchitectureModel from the
// gitlinks, so the list is not a second copy of .gitmodules. Ref is the pinned
// commit: the pin of a member is the gitlink, nothing else. An explicit
// library member of the same name overrides the derived one in everything it
// sets (roles, classifiers, test globs, ref).
func PolicyMembers(members []Member, config policy.Submodules, explicit []policy.Member) []policy.Member {
	override := map[string]policy.Member{}
	for _, member := range explicit {
		override[member.Name] = member
	}
	out := []policy.Member{}
	seen := map[string]bool{}
	for _, member := range members {
		if !selected(member.Path, config) {
			continue
		}
		name := nameIn(member, override)
		derived := policy.Member{Name: name, URL: member.URL, Ref: member.CodeCommit}
		if explicit, ok := override[name]; ok {
			derived = merge(derived, explicit)
		}
		seen[name] = true
		out = append(out, derived)
	}
	for _, member := range explicit {
		if !seen[member.Name] {
			out = append(out, member)
		}
	}
	return out
}

// nameIn is the name a member goes by: its own when the library names it,
// else the first candidate the library names, else its own.
func nameIn(member Member, named map[string]policy.Member) string {
	if _, ok := named[member.Name]; ok {
		return member.Name
	}
	for _, candidate := range member.Candidates {
		if _, ok := named[candidate]; ok {
			return candidate
		}
	}
	return member.Name
}

func merge(base, over policy.Member) policy.Member {
	if over.URL != "" {
		base.URL = over.URL
	}
	if over.Ref != "" {
		base.Ref = over.Ref
	}
	base.Path = over.Path
	base.Roles = over.Roles
	base.Classifiers = over.Classifiers
	base.TestGlobs = over.TestGlobs
	return base
}

func selected(path string, config policy.Submodules) bool {
	if len(config.Include) > 0 && !matchAny(config.Include, path) {
		return false
	}
	return !matchAny(config.Exclude, path)
}

func matchAny(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if glob.Match(pattern, path) {
			return true
		}
	}
	return false
}
