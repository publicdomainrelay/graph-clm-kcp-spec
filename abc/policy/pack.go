package policy

import (
	"fmt"
	"slices"
	"strings"
)

const (
	PackManifestPath = "pack.yaml"

	LockPath = "policies.lock"

	PackDir = "packs"

	SourceEmbedded = "embedded"

	SourceGit = "git"

	SourceOCI = "oci"
)

// PackManifest is a pack's pack.yaml: what the pack is, which binding it
// needs, and which vocabulary classes its templates read.
type PackManifest struct {
	Name string `json:"name"`

	Version string `json:"version,omitempty"`

	Description string `json:"description,omitempty"`

	Roles []string `json:"roles,omitempty"`

	Vocabulary []string `json:"vocabulary,omitempty"`

	Parameters map[string]string `json:"parameters,omitempty"`
}

func (m PackManifest) Reference() string {
	name := m.Name
	if m.Version == "" {
		return name
	}
	return name + "@" + m.Version
}

// Missing names the roles and the vocabulary classes a pack needs and a
// binding does not declare.
func (m PackManifest) Missing(binding Binding) []string {
	out := []string{}
	for _, role := range m.Roles {
		if _, ok := binding.Roles[role]; !ok {
			out = append(out, "role:"+role)
		}
	}
	classes := binding.Vocabulary.Classes()
	for _, class := range m.Vocabulary {
		if !slices.Contains(classes, class) {
			out = append(out, "vocabulary:"+class)
		}
	}
	slices.Sort(out)
	return out
}

// PackSource is a parsed import source: embedded, git:<url>@<ref> or
// oci:<ref>.
type PackSource struct {
	Kind string

	URL string

	Ref string
}

func ParsePackSource(source string) (PackSource, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" || trimmed == SourceEmbedded {
		return PackSource{Kind: SourceEmbedded}, nil
	}
	switch {
	case strings.HasPrefix(trimmed, SourceGit+":"):
		rest := strings.TrimPrefix(trimmed, SourceGit+":")
		at := strings.LastIndex(rest, "@")
		if at <= 0 || at == len(rest)-1 {
			return PackSource{}, fmt.Errorf("policy: git source %q needs <url>@<ref>", source)
		}
		return PackSource{Kind: SourceGit, URL: rest[:at], Ref: rest[at+1:]}, nil
	case strings.HasPrefix(trimmed, SourceOCI+":"):
		return PackSource{Kind: SourceOCI, Ref: strings.TrimPrefix(trimmed, SourceOCI+":")}, nil
	}
	return PackSource{}, fmt.Errorf("policy: unknown pack source %q", source)
}

func (s PackSource) String() string {
	switch s.Kind {
	case SourceGit:
		return SourceGit + ":" + s.URL + "@" + s.Ref
	case SourceOCI:
		return SourceOCI + ":" + s.Ref
	}
	return SourceEmbedded
}

// PackLock pins every imported pack by digests, so a build resolves the same
// content twice.
type PackLock struct {
	Imports []LockEntry `json:"imports"`

	// Members pins every member repository to the commit its ref resolved to,
	// so a second build reads the same cross-repository model.
	Members []MemberLock `json:"members,omitempty"`
}

type LockEntry struct {
	Pack string `json:"pack"`

	Version string `json:"version,omitempty"`

	Source string `json:"source,omitempty"`

	SHA256 string `json:"sha256"`

	Files int `json:"files,omitempty"`
}

func (l PackLock) Entry(pack, version string) (LockEntry, bool) {
	for _, entry := range l.Imports {
		if entry.Pack == pack && entry.Version == version {
			return entry, true
		}
	}
	return LockEntry{}, false
}

func (l *PackLock) Set(entry LockEntry) {
	for index := range l.Imports {
		if l.Imports[index].Pack == entry.Pack && l.Imports[index].Version == entry.Version {
			l.Imports[index] = entry
			return
		}
	}
	l.Imports = append(l.Imports, entry)
	slices.SortStableFunc(l.Imports, func(left, right LockEntry) int {
		if left.Pack != right.Pack {
			return strings.Compare(left.Pack, right.Pack)
		}
		return strings.Compare(left.Version, right.Version)
	})
}

// Matches reports whether a pack's resolved digest is the pinned one. An
// import the lock does not name is not pinned yet, and a build adds it.
func (l PackLock) Matches(entry LockEntry) bool {
	pinned, ok := l.Entry(entry.Pack, entry.Version)
	if !ok {
		return false
	}
	return pinned.SHA256 == entry.SHA256
}

// Member returns the pin of a member.
func (l PackLock) Member(name string) (MemberLock, bool) {
	for _, member := range l.Members {
		if member.Name == name {
			return member, true
		}
	}
	return MemberLock{}, false
}

// SetMember records a member's pin, replacing an earlier one.
func (l *PackLock) SetMember(member MemberLock) {
	for index := range l.Members {
		if l.Members[index].Name == member.Name {
			l.Members[index] = member
			return
		}
	}
	l.Members = append(l.Members, member)
	slices.SortStableFunc(l.Members, func(left, right MemberLock) int {
		return strings.Compare(left.Name, right.Name)
	})
}

// MemberMatches reports whether a member's resolved commit is the pinned one.
func (l PackLock) MemberMatches(member MemberLock) bool {
	pinned, ok := l.Member(member.Name)
	if !ok {
		return false
	}
	return pinned.Commit == member.Commit
}

// ImportReference is the pack@version an import names.
func ImportReference(imp PackImport) string {
	if imp.Version == "" {
		return imp.Pack
	}
	return imp.Pack + "@" + imp.Version
}
