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

// ParseUnifiedDiff turns the output of `git diff` into the CodeDiff a policy
// reviews. Hunk headers give the line numbers; a file whose old side is
// /dev/null is added and one whose new side is /dev/null is deleted.
func ParseUnifiedDiff(text string) CodeDiff {
	diff := CodeDiff{
		APIVersion: APIVersion,
		Kind:       CodeDiffKind,
		Spec:       CodeDiffSpec{Files: []CodeDiffFile{}},
	}
	var current *CodeDiffFile
	oldPath, newPath := "", ""
	oldLine, newLine := 0, 0
	oldRemaining, newRemaining := 0, 0
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
		if inHunk {
			oldRemaining, newRemaining, inHunk = hunkLine(current, line, oldLine, newLine, oldRemaining, newRemaining)
			oldLine += hunkOldAdvance(line)
			newLine += hunkNewAdvance(line)
			continue
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			oldPath, newPath = gitHeaderPaths(line)
			current = &CodeDiffFile{}
		case strings.HasPrefix(line, "--- "):
			oldPath = trimPathPrefix(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			newPath = trimPathPrefix(strings.TrimPrefix(line, "+++ "))
			if current == nil {
				current = &CodeDiffFile{}
			}
		case strings.HasPrefix(line, "rename from "):
			oldPath = trimPathPrefix(strings.TrimPrefix(line, "rename from "))
			if current == nil {
				current = &CodeDiffFile{}
			}
		case strings.HasPrefix(line, "rename to "):
			newPath = trimPathPrefix(strings.TrimPrefix(line, "rename to "))
			if current == nil {
				current = &CodeDiffFile{}
			}
		case strings.HasPrefix(line, "@@"):
			if current == nil {
				current = &CodeDiffFile{}
			}
			oldLine, newLine, oldRemaining, newRemaining = hunkRange(line)
			inHunk = true
		}
	}
	flush()
	diff.Sort()
	return diff
}

// hunkLine consumes one line inside a hunk. A removed line that starts with
// `--` reads like a `---` file header and an added line that starts with `++`
// reads like a `+++` header; inside a hunk both are content, and the hunk ends
// when the line counts of its `@@` header are spent.
func hunkLine(current *CodeDiffFile, line string, oldLine, newLine, oldRemaining, newRemaining int) (int, int, bool) {
	switch {
	case strings.HasPrefix(line, "\\"):
		return oldRemaining, newRemaining, true
	case strings.HasPrefix(line, "+"):
		current.Added = append(current.Added, CodeDiffLine{Line: newLine, Text: line[1:]})
		newRemaining--
	case strings.HasPrefix(line, "-"):
		current.Removed = append(current.Removed, CodeDiffLine{Line: oldLine, Text: line[1:]})
		oldRemaining--
	case strings.HasPrefix(line, " "):
		oldRemaining--
		newRemaining--
	default:
		return 0, 0, false
	}
	return oldRemaining, newRemaining, oldRemaining > 0 || newRemaining > 0
}

func hunkOldAdvance(line string) int {
	if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "\\") {
		return 0
	}
	if strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") {
		return 1
	}
	return 0
}

func hunkNewAdvance(line string) int {
	if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "\\") {
		return 0
	}
	if strings.HasPrefix(line, "+") || strings.HasPrefix(line, " ") {
		return 1
	}
	return 0
}

func gitHeaderPaths(line string) (string, string) {
	fields := strings.Fields(strings.TrimPrefix(line, "diff --git "))
	if len(fields) < 2 {
		return "", ""
	}
	return trimPathPrefix(fields[0]), trimPathPrefix(fields[1])
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

func hunkRange(header string) (int, int, int, int) {
	oldLine, newLine := 0, 0
	oldCount, newCount := 0, 0
	fields := strings.Fields(header)
	for _, field := range fields {
		if strings.HasPrefix(field, "-") {
			oldLine, oldCount = lineRange(field[1:])
			continue
		}
		if strings.HasPrefix(field, "+") {
			newLine, newCount = lineRange(field[1:])
		}
	}
	return oldLine, newLine, oldCount, newCount
}

// lineRange reads a `@@` range: `start` or `start,count`. A missing count is
// one line.
func lineRange(value string) (int, int) {
	count := 1
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		count = lineNumber(value[comma+1:])
		value = value[:comma]
	}
	return lineNumber(value), count
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
