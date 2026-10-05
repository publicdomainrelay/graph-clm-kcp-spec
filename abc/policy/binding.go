package policy

import "slices"

type RoleBinding struct {
	Contexts []string `json:"contexts,omitempty"`

	Labels map[string]string `json:"labels,omitempty"`

	Globs []string `json:"globs,omitempty"`

	Symbols []string `json:"symbols,omitempty"`

	Declared bool `json:"declared,omitempty"`

	Targets *RoleTargets `json:"targets,omitempty"`
}

type RoleTargets struct {
	Hosts []string `json:"hosts,omitempty"`

	NSIDs []string `json:"nsids,omitempty"`

	Routes []string `json:"routes,omitempty"`

	Symbols []string `json:"symbols,omitempty"`
}

type Vocabulary struct {
	Events map[string][]string `json:"events,omitempty"`

	Channels map[string][]string `json:"channels,omitempty"`

	Payloads map[string][]string `json:"payloads,omitempty"`

	Purposes map[string][]string `json:"purposes,omitempty"`
}

type PackImport struct {
	Pack string `json:"pack"`

	Version string `json:"version,omitempty"`

	Source string `json:"source,omitempty"`
}

type Binding struct {
	Roles map[string]RoleBinding `json:"roles,omitempty"`

	Vocabulary Vocabulary

	Imports []PackImport `json:"imports,omitempty"`
}

func (l PolicyLibrary) Binding() Binding {
	out := Binding{Roles: l.Roles, Imports: l.Imports}
	if l.Vocabulary != nil {
		out.Vocabulary = *l.Vocabulary
	}
	return out
}

func (b Binding) RoleNames() []string {
	out := make([]string, 0, len(b.Roles))
	for name := range b.Roles {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
