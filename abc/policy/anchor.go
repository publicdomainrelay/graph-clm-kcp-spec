package policy

import (
	"fmt"
	"sort"
	"strings"
)

// Anchor fills in the stable part of a violation's site. The line a violation
// points at moves as soon as code above it is edited, so identity cannot rest
// on it: the enclosing declaration of the location and, when the violation is
// about a model effect, the effect's kind and attributes survive an edit
// elsewhere in the same declaration. Evaluation anchors every violation before
// a report is keyed, so a durable waiver and a baseline comparison name the
// site and not the line.
func Anchor(violation Violation, graph CodeGraph) Violation {
	if violation.Location == nil || violation.Location.File == "" {
		return violation
	}
	location := *violation.Location
	if location.Declaration == "" {
		node := nodeAt(graph, location.File, location.Line)
		location.Declaration = node.QualifiedName
		if location.Declaration == "" {
			location.Declaration = node.Name
		}
	}
	if kind, attrs := effectAnchor(violation.Details); kind != "" || attrs != "" {
		if location.Kind == "" {
			location.Kind = kind
		}
		if location.Attrs == "" {
			location.Attrs = attrs
		}
	}
	violation.Location = &location
	return violation
}

// Anchored reports whether a location carries a site that does not move with
// the lines around it.
func Anchored(location *Location) bool {
	return location != nil && location.Declaration != ""
}

func effectAnchor(details map[string]any) (string, string) {
	effect, ok := details["effect"].(map[string]any)
	if !ok {
		return "", ""
	}
	kind, _ := effect["kind"].(string)
	attrs, _ := effect["attrs"].(map[string]any)
	if len(attrs) == 0 {
		return kind, ""
	}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, attrs[key]))
	}
	return kind, strings.Join(parts, ",")
}
