package policy

import (
	"strconv"
	"strings"
)

const (
	DiffStatusAdded    = "added"
	DiffStatusDeleted  = "deleted"
	DiffStatusModified = "modified"

	DevNull = "/dev/null"
)

func ParseUnifiedDiff(text string) CodeDiff {
	diff := CodeDiff{
		APIVersion: APIVersion,
		Kind:       CodeDiffKind,
		Spec:       CodeDiffSpec{Files: []CodeDiffFile{}},
	}
	var current *CodeDiffFile
	oldPath, newPath := "", ""
	oldLine, newLine := 0, 0
	inHunk := false

	flush := func() {
		if current == nil {
			return
		}
		name := newPath
		if name == DevNull || name == "" {
			name = oldPath
		}
		current.Path = trimPathPrefix(name)
		switch {
		case oldPath == DevNull:
			current.Status = DiffStatusAdded
		case newPath == DevNull:
			current.Status = DiffStatusDeleted
		default:
			current.Status = DiffStatusModified
		}
		diff.Spec.Files = append(diff.Spec.Files, *current)
		current = nil
	}

	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			oldPath, newPath, inHunk = "", "", false
		case strings.HasPrefix(line, "--- "):
			oldPath = trimPathPrefix(strings.TrimPrefix(line, "--- "))
			inHunk = false
		case strings.HasPrefix(line, "+++ "):
			newPath = trimPathPrefix(strings.TrimPrefix(line, "+++ "))
			current = &CodeDiffFile{}
			inHunk = false
		case strings.HasPrefix(line, "@@"):
			if current == nil {
				current = &CodeDiffFile{}
			}
			oldLine, newLine = hunkStart(line)
			inHunk = true
		case !inHunk:
			continue
		case strings.HasPrefix(line, "\\"):
			continue
		case strings.HasPrefix(line, "+"):
			current.Added = append(current.Added, CodeDiffLine{Line: newLine, Text: line[1:]})
			newLine++
		case strings.HasPrefix(line, "-"):
			current.Removed = append(current.Removed, CodeDiffLine{Line: oldLine, Text: line[1:]})
			oldLine++
		case strings.HasPrefix(line, " "):
			oldLine++
			newLine++
		}
	}
	flush()
	diff.Sort()
	return diff
}

func trimPathPrefix(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"a/", "b/"} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return value
}

func hunkStart(header string) (int, int) {
	oldLine, newLine := 0, 0
	fields := strings.Fields(header)
	for _, field := range fields {
		if strings.HasPrefix(field, "-") {
			oldLine = lineNumber(field[1:])
			continue
		}
		if strings.HasPrefix(field, "+") {
			newLine = lineNumber(field[1:])
		}
	}
	return oldLine, newLine
}

func lineNumber(value string) int {
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}
