package spec

import (
	"reflect"
	"slices"
)

// ProgressRecord is one observation a host made while a SpecChange ran: which
// turn of the agent loop it happened in, which tool ran, which files that tool
// touched, and a short note. A host that lives inside the agent (the Claude
// Code mod, the pi extension) writes these through `specctl clm report`, so the
// controllers watch the work while it happens instead of guessing afterwards.
type ProgressRecord struct {
	Turn int `json:"turn,omitempty"`

	Tool string `json:"tool,omitempty"`

	Files []string `json:"files,omitempty"`

	Note string `json:"note,omitempty"`

	// At is when the host observed it, in RFC 3339. The host stamps it, so the
	// record says when the work happened, not when the tool was told about it.
	At string `json:"at,omitempty"`
}

// MaxProgressRecords bounds the list. A status subresource is read by every
// watcher, so an agent that reports every tool call of a long run must not make
// it grow without end: the newest records are kept and the oldest fall off.
const MaxProgressRecords = 128

// AppendProgress adds one record to the bounded list. A record identical to the
// newest one is dropped, so a host that re-reports the same file on every turn
// does not fill the list with copies. It reports whether the status changed.
func (s *SpecChangeStatus) AppendProgress(record ProgressRecord) bool {
	record.Files = CanonicalSet(record.Files)
	if len(s.Progress) > 0 && sameProgress(s.Progress[len(s.Progress)-1], record) {
		return false
	}
	s.Progress = append(s.Progress, record)
	if len(s.Progress) > MaxProgressRecords {
		s.Progress = slices.Clone(s.Progress[len(s.Progress)-MaxProgressRecords:])
	}
	return true
}

func sameProgress(left, right ProgressRecord) bool {
	return left.Turn == right.Turn && left.Tool == right.Tool && left.Note == right.Note &&
		reflect.DeepEqual(left.Files, right.Files)
}
