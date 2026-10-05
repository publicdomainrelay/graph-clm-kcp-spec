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

// ClauseIdentity is the identity of a violation that has no site at all: a
// declared flow a rule reports before any code exists, or a require. The
// clause's report carries the facts that tell one such violation from another
// -- the two roles, the channel, the purpose, the payloads -- so the key is
// those facts and not an empty site every violation of the rule would share.
// A waiver on one declared reach-in then covers that one and not the next.
func ClauseIdentity(violation Violation) string {
	return signature(violation.Details)
}

// volatileDetail names a detail that moves with an edit or with an evaluation
// and is not part of the clause's identity. An effect id embeds its line, so
// two evaluations of the same code produce different ids.
func volatileDetail(key string) bool {
	return key == "line" || key == "id"
}

func signature(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if volatileDetail(key) {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+signature(typed[key]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, 0, len(typed))
		for _, entry := range typed {
			parts = append(parts, signature(entry))
		}
		sort.Strings(parts)
		return "[" + strings.Join(parts, ",") + "]"
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", value)
	}
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
