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

	// Attrs names patterns matched against the attributes of an effect, not
	// against the text around its site. `verb: getNodeId` on a container.exec
	// points at the guest; the word getNodeId in a comment does not.
	Attrs []string `json:"attrs,omitempty"`
}

type Vocabulary struct {
	Events map[string][]string `json:"events,omitempty"`

	Channels map[string][]string `json:"channels,omitempty"`

	Payloads map[string][]string `json:"payloads,omitempty"`

	Purposes map[string][]string `json:"purposes,omitempty"`

	// Routes names the routes a package rule needs to recognize, for example
	// the route that receives the guest's report. A route class is matched
	// against the path of an http.handle effect.
	Routes map[string][]string `json:"routes,omitempty"`
}

// Classes names every vocabulary class the binding declares, as
// group/name: channels/relay, events/network-report, payloads/network-info,
// purposes/network-discovery, routes/report.
func (v Vocabulary) Classes() []string {
	groups := []struct {
		name   string
		values map[string][]string
	}{
		{"channels", v.Channels},
		{"events", v.Events},
		{"payloads", v.Payloads},
		{"purposes", v.Purposes},
		{"routes", v.Routes},
	}
	out := []string{}
	for _, group := range groups {
		names := make([]string, 0, len(group.values))
		for name := range group.values {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if name == "" || len(group.values[name]) == 0 {
				continue
			}
			out = append(out, group.name+"/"+name)
		}
	}
	return out
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
