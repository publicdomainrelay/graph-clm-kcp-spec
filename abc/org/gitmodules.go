package org

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Submodule is one entry of a .gitmodules file.
type Submodule struct {
	Name string

	Path string

	URL string

	// Branch is the branch `git submodule update --remote` follows, when the
	// file sets one.
	Branch string
}

// ParseGitmodules reads the text of a .gitmodules file. Entries without a path
// are an error, because a submodule is its path in the superproject.
func ParseGitmodules(text string) ([]Submodule, error) {
	var out []Submodule
	var current *Submodule
	flush := func() error {
		if current == nil {
			return nil
		}
		if current.Path == "" {
			return fmt.Errorf("org: submodule %q has no path", current.Name)
		}
		out = append(out, *current)
		current = nil
		return nil
	}
	for number, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if err := flush(); err != nil {
				return nil, err
			}
			name, ok := sectionName(line)
			if ok {
				current = &Submodule{Name: name}
			}
			continue
		}
		if current == nil {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("org: .gitmodules line %d: %q is not key = value", number+1, line)
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "path":
			current.Path = path.Clean(value)
		case "url":
			current.URL = value
		case "branch":
			current.Branch = value
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Path < out[right].Path })
	return out, nil
}

func sectionName(line string) (string, bool) {
	inner := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
	kind, rest, found := strings.Cut(inner, " ")
	if !found || !strings.EqualFold(strings.TrimSpace(kind), "submodule") {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(rest), `"`), true
}

// RepositoryCandidates are the names a member's branches may use, most likely
// first: the checkout directory (what ingest names a repository after), the
// repository name in the url, then the submodule name.
func (s Submodule) RepositoryCandidates() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, candidate := range []string{path.Base(s.Path), URLBase(s.URL), path.Base(s.Name)} {
		if candidate == "" || candidate == "." || candidate == "/" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

// URLBase is the repository name at the end of a clone url, for https, ssh and
// scp-style urls.
func URLBase(url string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(url), "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	if index := strings.LastIndexAny(trimmed, "/:"); index >= 0 {
		trimmed = trimmed[index+1:]
	}
	return trimmed
}
