package org

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// MemberTrailer records one pointer move: "<member> <path> <from>..<to>".
	MemberTrailer = "Member"

	// MemberSpecTrailer records the architecture commit of the member the new
	// pin resolves to: "<member> <commit>".
	MemberSpecTrailer = "Member-Spec"

	// SpecChangeTrailer is the same trailer the architecture branch uses.
	SpecChangeTrailer = "Spec-Change"
)

// Bump is one submodule pointer move.
type Bump struct {
	Member string

	Path string

	From string

	To string

	// Spec is the architecture commit of the member the new pin resolves to.
	Spec string
}

// Short is the abbreviated form of a commit id in a subject line.
func Short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	if commit == "" {
		return "none"
	}
	return commit
}

// Subject names the move so `git log --oneline` at the root answers which
// member moved.
func Subject(bumps []Bump) string {
	switch len(bumps) {
	case 0:
		return "bump: nothing"
	case 1:
		b := bumps[0]
		return fmt.Sprintf("bump(%s): %s -> %s", b.Member, Short(b.From), Short(b.To))
	}
	names := make([]string, 0, len(bumps))
	for _, b := range bumps {
		names = append(names, b.Member)
	}
	sort.Strings(names)
	return fmt.Sprintf("bump(%s): %d members", strings.Join(names, ", "), len(bumps))
}

// Message is the full commit message of a pointer move: the subject, an
// optional body, and one Member trailer per move.
func Message(bumps []Bump, change, body string) string {
	var b strings.Builder
	b.WriteString(Subject(bumps))
	b.WriteString("\n")
	if body != "" {
		b.WriteString("\n" + strings.TrimSpace(body) + "\n")
	}
	b.WriteString("\n")
	for _, bump := range bumps {
		fmt.Fprintf(&b, "%s: %s %s %s..%s\n", MemberTrailer, bump.Member, bump.Path, bump.From, bump.To)
	}
	for _, bump := range bumps {
		if bump.Spec != "" {
			fmt.Fprintf(&b, "%s: %s %s\n", MemberSpecTrailer, bump.Member, bump.Spec)
		}
	}
	if change != "" {
		fmt.Fprintf(&b, "%s: %s\n", SpecChangeTrailer, change)
	}
	return b.String()
}

// ParseBumps reads the Member and Member-Spec trailers of a commit message.
func ParseBumps(message string) []Bump {
	var out []Bump
	specs := map[string]string{}
	for _, line := range strings.Split(message, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), ": ")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		switch key {
		case MemberTrailer:
			if len(fields) != 3 {
				continue
			}
			from, to, ok := strings.Cut(fields[2], "..")
			if !ok {
				continue
			}
			out = append(out, Bump{Member: fields[0], Path: fields[1], From: from, To: to})
		case MemberSpecTrailer:
			if len(fields) == 2 {
				specs[fields[0]] = fields[1]
			}
		}
	}
	for index := range out {
		out[index].Spec = specs[out[index].Member]
	}
	return out
}
