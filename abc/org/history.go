package org

import (
	"strings"
)

// GitlinkMode is the tree entry mode of a submodule.
const GitlinkMode = "160000"

// Move is one submodule pointer change in a root commit. An empty From is an
// added submodule, an empty To a removed one.
type Move struct {
	Path string `json:"path"`

	From string `json:"from,omitempty"`

	To string `json:"to,omitempty"`
}

func (m Move) Added() bool { return m.From == "" }

func (m Move) Removed() bool { return m.To == "" }

// RootCommit is a root commit that changed at least one pointer.
type RootCommit struct {
	Commit string `json:"commit"`

	Date string `json:"date,omitempty"`

	Author string `json:"author,omitempty"`

	Subject string `json:"subject,omitempty"`

	// Moves are the raw pointer moves; History reports them, enriched, as
	// HistoryEntry.Details.
	Moves []Move `json:"-"`
}

// HistoryFormat is the `git log --format` ParseHistory reads, with --raw
// --no-abbrev.
const HistoryFormat = "commit %H%x09%aI%x09%an%x09%s"

// ParseHistory reads `git log --raw --no-abbrev --format=HistoryFormat`
// output and keeps the commits that moved a gitlink. Raw lines look like
// ":160000 160000 <from> <to> M\t<path>".
func ParseHistory(output string) []RootCommit {
	var out []RootCommit
	var current *RootCommit
	flush := func() {
		if current != nil && len(current.Moves) > 0 {
			out = append(out, *current)
		}
		current = nil
	}
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "commit "):
			flush()
			fields := strings.SplitN(strings.TrimPrefix(line, "commit "), "\t", 4)
			current = &RootCommit{Commit: fields[0]}
			if len(fields) == 4 {
				current.Date, current.Author, current.Subject = fields[1], fields[2], fields[3]
			}
		case strings.HasPrefix(line, ":") && current != nil:
			if move, ok := parseRaw(line); ok {
				current.Moves = append(current.Moves, move)
			}
		}
	}
	flush()
	return out
}

func parseRaw(line string) (Move, bool) {
	meta, path, found := strings.Cut(strings.TrimPrefix(line, ":"), "\t")
	if !found {
		return Move{}, false
	}
	fields := strings.Fields(meta)
	if len(fields) < 5 {
		return Move{}, false
	}
	oldMode, newMode, from, to := fields[0], fields[1], fields[2], fields[3]
	if oldMode != GitlinkMode && newMode != GitlinkMode {
		return Move{}, false
	}
	if oldMode != GitlinkMode {
		from = ""
	}
	if newMode != GitlinkMode {
		to = ""
	}
	return Move{Path: path, From: from, To: to}, true
}
