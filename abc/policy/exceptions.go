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

// Waiver turns a durable exception into the override a decision reads. A
// site-scoped exception names one violation by its key. A rule-scoped one
// waives the whole constraint, so it carries no key and no site: the key is
// what makes a waiver precise, and a waiver without one must say so.
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

// ScopeOf is the scope an exception names: site unless it asks for the whole
// rule.
func (e Exception) ScopeOf() string {
	if strings.TrimSpace(e.Scope) == "" {
		return ExceptionScopeSite
	}
	return strings.TrimSpace(e.Scope)
}

// StaleExceptions names the key-scoped exceptions that match no violation of
// the report: the site they were written for is gone, so a decision would
// never honour them and the file should be removed. A rule-scoped exception
// matches by construction, and an expired one is reported as expired.
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

// ValidExpiry reports whether an exception's expiry is a date this package
// understands. A waiver that names an expiry it cannot parse is refused when
// it is written, so an exception never reads as permanent by accident.
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

// Waivers turns the durable exceptions of a library into the waivers the
// decision reads, and returns the expired ones separately so a caller can
// report them instead of dropping them silently.
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

// Key is the stable identity of a violation: the constraint, the object and
// the site. The site is the enclosing declaration and the effect's kind and
// attributes when an evaluation anchored them, and the raw file and line
// otherwise. Two evaluations of the same code produce the same key even when
// an edit moved the line, so a waiver written from one run still matches the
// next.
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
