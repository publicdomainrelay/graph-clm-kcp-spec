package spec

import (
	"regexp"
	"strings"
)

var machinePathPattern = regexp.MustCompile(`/(?:home|Users|root|tmp|mnt|opt|srv)/[A-Za-z0-9._@+-]+`)

var flagPattern = regexp.MustCompile(`--[a-z0-9][a-z0-9-]*`)

var environmentPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}\b`)

func AbsoluteMachinePath(text string) (string, bool) {
	if found := machinePathPattern.FindString(text); found != "" {
		return found, true
	}
	return "", false
}

func NamedFlags(text string) []string {
	return CanonicalSet(flagPattern.FindAllString(text, -1))
}

func NamedEnvironments(text string) []string {
	found := []string{}
	for _, name := range environmentPattern.FindAllString(text, -1) {
		switch name {
		case "MUST", "SHOULD", "MAY", "TODO", "NOTE", "JSON", "HTTP", "HTTPS", "URL", "URI", "API", "YAML", "TLS", "DNS", "RBAC", "ID", "OK":
			continue
		}
		found = append(found, name)
	}
	return CanonicalSet(found)
}

func DeclaresConfigSurface(requirements []Requirement) bool {
	for _, requirement := range requirements {
		if len(NamedFlags(requirement.Text)) > 0 || len(NamedEnvironments(requirement.Text)) > 0 {
			return true
		}
	}
	return false
}

func CommandContext(files []string) bool {
	for _, file := range files {
		if strings.HasPrefix(file, "cmd/") || strings.Contains(file, "/cmd/") {
			return true
		}
	}
	return false
}

func EnumeratesNames(text string) bool {
	parts := strings.Split(text, ",")
	if len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(part), "."))
		if trimmed == "" || strings.ContainsAny(trimmed, " \t") {
			return false
		}
		if !identifierLike(trimmed) {
			return false
		}
	}
	return true
}

func identifierLike(value string) bool {
	for index, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
			if index == 0 {
				return false
			}
		case char == '_', char == '.', char == '*':
		default:
			return false
		}
	}
	return value != ""
}
