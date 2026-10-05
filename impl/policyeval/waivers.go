package policyeval

import (
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

// Waivers is the override list a decision reads: the one-shot overrides an
// acceptance carries, then the library's durable exceptions. An expired
// exception is dropped and returned so a caller can report it.
func Waivers(library policy.Library, overrides []policy.Override, now time.Time) ([]policy.Override, []policy.Exception) {
	durable, expired := policy.Waivers(library.Manifest.Exceptions, now)
	out := make([]policy.Override, 0, len(overrides)+len(durable))
	out = append(out, overrides...)
	out = append(out, durable...)
	return out, expired
}
