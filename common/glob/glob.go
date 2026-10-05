package glob

import (
	"regexp"
	"strings"
)

// Regexp translates one doublestar glob into an anchored regular expression.
// `**` crosses a slash, `*` and `?` do not, and every other character is
// literal. It is the one dialect the Go model and lib.specd's glob.match share.
func Regexp(pattern string) string {
	builder := strings.Builder{}
	builder.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		switch char := pattern[index]; char {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				if index+2 < len(pattern) && pattern[index+2] == '/' {
					builder.WriteString("(?:.*/)?")
					index += 2
					continue
				}
				builder.WriteString(".*")
				index++
				continue
			}
			builder.WriteString("[^/]*")
		case '?':
			builder.WriteString("[^/]")
		default:
			builder.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	builder.WriteString("$")
	return builder.String()
}

// Match reports whether one doublestar glob matches one slash-separated path.
func Match(pattern, name string) bool {
	if pattern == "" {
		return false
	}
	matched, err := regexp.MatchString(Regexp(pattern), name)
	return err == nil && matched
}
