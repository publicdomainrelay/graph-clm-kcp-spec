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

// Merge folds another binding's roles into this one: a role both declare
// carries the union of their selectors, so a member repository adds to the
// abstract role instead of replacing it. The vocabulary and the imports stay
// this binding's.
func (b Binding) Merge(other Binding) Binding {
	out := Binding{
		Roles:      map[string]RoleBinding{},
		Vocabulary: b.Vocabulary,
		Imports:    b.Imports,
	}
	for name, role := range b.Roles {
		out.Roles[name] = role
	}
	for name, role := range other.Roles {
		existing, ok := out.Roles[name]
		if !ok {
			out.Roles[name] = role
			continue
		}
		out.Roles[name] = mergeRoleBindings(existing, role)
	}
	return out
}

func mergeRoleBindings(left, right RoleBinding) RoleBinding {
	return RoleBinding{
		Contexts: appendUnique(left.Contexts, right.Contexts),
		Labels:   mergeLabels(left.Labels, right.Labels),
		Globs:    appendUnique(left.Globs, right.Globs),
		Symbols:  appendUnique(left.Symbols, right.Symbols),
		Declared: left.Declared || right.Declared,
		Targets:  mergeTargets(left.Targets, right.Targets),
	}
}

func mergeLabels(left, right map[string]string) map[string]string {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func mergeTargets(left, right *RoleTargets) *RoleTargets {
	if left == nil && right == nil {
		return nil
	}
	out := &RoleTargets{}
	if left != nil {
		out.Hosts = append([]string{}, left.Hosts...)
		out.NSIDs = append([]string{}, left.NSIDs...)
		out.Routes = append([]string{}, left.Routes...)
		out.Symbols = append([]string{}, left.Symbols...)
		out.Attrs = append([]string{}, left.Attrs...)
	}
	if right != nil {
		out.Hosts = appendUnique(out.Hosts, right.Hosts)
		out.NSIDs = appendUnique(out.NSIDs, right.NSIDs)
		out.Routes = appendUnique(out.Routes, right.Routes)
		out.Symbols = appendUnique(out.Symbols, right.Symbols)
		out.Attrs = appendUnique(out.Attrs, right.Attrs)
	}
	return out
}

func appendUnique(base, extra []string) []string {
	out := append([]string{}, base...)
	for _, value := range extra {
		if value == "" || slices.Contains(out, value) {
			continue
		}
		out = append(out, value)
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
