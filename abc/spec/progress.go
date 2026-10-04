package spec

import (
	"reflect"
	"slices"
)

type ProgressRecord struct {
	Turn int `json:"turn,omitempty"`

	Tool string `json:"tool,omitempty"`

	Files []string `json:"files,omitempty"`

	Note string `json:"note,omitempty"`

	At string `json:"at,omitempty"`
}

const MaxProgressRecords = 128

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
