package policy

import (
	"fmt"
	"sort"
	"strings"
)

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

func Anchored(location *Location) bool {
	return location != nil && location.Declaration != ""
}

func ClauseIdentity(violation Violation) string {
	return signature(violation.Details)
}

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
