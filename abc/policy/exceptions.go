package policy

import (
	"strconv"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/glob"
)

const ExceptionsDir = "exceptions"

const (
	ExceptionScopeSite = "site"

	ExceptionScopeRule = "rule"
)

type Exception struct {
	Constraint string `json:"constraint"`

	Key string `json:"key,omitempty"`

	Scope string `json:"scope,omitempty"`

	Object string `json:"object,omitempty"`

	File string `json:"file,omitempty"`

	Line int `json:"line,omitempty"`

	Reason string `json:"reason"`

	Owner string `json:"owner,omitempty"`

	Expires string `json:"expires,omitempty"`
}

func (e Exception) Waiver() Override {
	waiver := Override{
		Constraint: e.Constraint,
		Reason:     e.Reason,
		By:         e.Owner,
		Key:        e.Key,
	}
	if e.Scope == ExceptionScopeRule {
		return waiver
	}
	waiver.Object = e.Object
	waiver.File = e.File
	waiver.Line = e.Line
	return waiver
}

func (e Exception) ScopeOf() string {
	if strings.TrimSpace(e.Scope) == "" {
		return ExceptionScopeSite
	}
	return strings.TrimSpace(e.Scope)
}

func StaleExceptions(exceptions []Exception, violations []Violation, now time.Time) []Exception {
	out := []Exception{}
	for _, exception := range exceptions {
		if exception.Key == "" || exception.Expired(now) {
			continue
		}
		waiver := exception.Waiver()
		matched := false
		for _, violation := range violations {
			if waiver.Matches(violation) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, exception)
		}
	}
	return out
}

func (e Exception) Expired(now time.Time) bool {
	when, ok := parseExpiry(e.Expires)
	return ok && now.After(when)
}

func ValidExpiry(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	_, ok := parseExpiry(value)
	return ok
}

func parseExpiry(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	if when, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return when, true
	}
	if when, err := time.Parse("2006-01-02", trimmed); err == nil {
		return when, true
	}
	return time.Time{}, false
}

func Waivers(exceptions []Exception, now time.Time) ([]Override, []Exception) {
	waivers := make([]Override, 0, len(exceptions))
	expired := []Exception{}
	for _, exception := range exceptions {
		if exception.Expired(now) {
			expired = append(expired, exception)
			continue
		}
		waivers = append(waivers, exception.Waiver())
	}
	return waivers, expired
}

func (o Override) Matches(violation Violation) bool {
	if o.Constraint != "" && o.Constraint != violation.Constraint {
		return false
	}
	if o.Object != "" && !strings.Contains(violation.Object.String(), o.Object) {
		return false
	}
	if o.Key != "" {
		return o.Key == Key(violation)
	}
	file, line := Site(violation)
	if o.File != "" && !glob.Match(o.File, file) {
		return false
	}
	if o.Line != 0 && o.Line != line {
		return false
	}
	return true
}

func Key(violation Violation) string {
	if Anchored(violation.Location) {
		return ViolationID(violation.Constraint, violation.Object.String(),
			violation.Location.File, violation.Location.Declaration,
			violation.Location.Kind, violation.Location.Attrs)
	}
	if file, line := Site(violation); file != "" {
		return ViolationID(violation.Constraint, violation.Object.String(), file, strconv.Itoa(line))
	}
	return ViolationID(violation.Constraint, violation.Object.String(), "no-site", ClauseIdentity(violation))
}

func Site(violation Violation) (string, int) {
	if violation.Location == nil {
		return "", 0
	}
	return violation.Location.File, violation.Location.Line
}
