package policy

import (
	"strconv"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/glob"
)

const ExceptionsDir = "exceptions"

type Exception struct {
	Constraint string `json:"constraint"`

	Key string `json:"key,omitempty"`

	Object string `json:"object,omitempty"`

	File string `json:"file,omitempty"`

	Line int `json:"line,omitempty"`

	Reason string `json:"reason"`

	Owner string `json:"owner,omitempty"`

	Expires string `json:"expires,omitempty"`
}

func (e Exception) Waiver() Override {
	return Override{
		Constraint: e.Constraint,
		Reason:     e.Reason,
		By:         e.Owner,
		Key:        e.Key,
		Object:     e.Object,
		File:       e.File,
		Line:       e.Line,
	}
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
	if o.Key != "" && o.Key != Key(violation) {
		return false
	}
	if o.Object != "" && !strings.Contains(violation.Object.String(), o.Object) {
		return false
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
// the site. Two evaluations of the same tree produce the same key, so a
// waiver written from one run still matches the next.
func Key(violation Violation) string {
	file, line := Site(violation)
	return ViolationID(violation.Constraint, violation.Object.String(), file, strconv.Itoa(line))
}

func Site(violation Violation) (string, int) {
	if violation.Location == nil {
		return "", 0
	}
	return violation.Location.File, violation.Location.Line
}
