package orggit

import (
	"context"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/org"
)

// CommitLine is one member commit, as a one-line log entry.
type CommitLine struct {
	Commit string `json:"commit"`

	Subject string `json:"subject"`
}

// MoveDetail is a pointer move with what it brought into the root.
type MoveDetail struct {
	org.Move

	Member string `json:"member"`

	// Commits are the member commits in From..To, newest first, capped.
	Commits []CommitLine `json:"commits,omitempty"`

	// Total is the real count of member commits in the range; zero when the
	// member is not checked out or the commits are not present.
	Total int `json:"total,omitempty"`

	// Arch is the member architecture commit the new pin resolves to.
	Arch string `json:"arch,omitempty"`

	// Message says why the range is not listed, when it is not.
	Message string `json:"message,omitempty"`
}

// HistoryEntry is a root commit that moved at least one pointer.
type HistoryEntry struct {
	org.RootCommit

	Details []MoveDetail `json:"moves"`

	// Bumps are the Member trailers of the commit message, when it has any.
	Bumps []org.Bump `json:"bumps,omitempty"`
}

// HistoryOptions selects the commits History reads.
type HistoryOptions struct {
	// Ref is where the walk starts; empty means HEAD.
	Ref string

	// Member keeps only the moves of the member whose name or path matches.
	Member string

	// Limit caps the entries returned; zero means 20.
	Limit int

	// RangeLimit caps the member commits listed per move; zero means 10.
	RangeLimit int

	// Scan caps the root commits looked at; zero means 2000.
	Scan int
}

// History lists how the root's submodule pointers moved: the root commits that
// changed a gitlink, newest first, with the member commits each move brought in
// and the member architecture commit the new pin resolves to.
func (r *Root) History(ctx context.Context, options HistoryOptions) ([]HistoryEntry, error) {
	ref := options.Ref
	if ref == "" {
		ref = "HEAD"
	}
	limit, rangeLimit, scan := options.Limit, options.RangeLimit, options.Scan
	if limit == 0 {
		limit = 20
	}
	if rangeLimit == 0 {
		rangeLimit = 10
	}
	if scan == 0 {
		scan = 2000
	}
	out, err := r.output(ctx, r.Dir, "log", "--first-parent", "-m", "--raw", "--no-abbrev",
		"--max-count="+strconv.Itoa(scan), "--format="+org.HistoryFormat, ref)
	if err != nil {
		return nil, err
	}
	states, _ := r.Members(ctx, MembersOptions{})
	byPath := map[string]org.MemberState{}
	for _, state := range states {
		byPath[state.Path] = state
	}
	var entries []HistoryEntry
	for _, commit := range org.ParseHistory(out) {
		entry := HistoryEntry{RootCommit: commit}
		for _, move := range commit.Moves {
			state, known := byPath[move.Path]
			name := state.Name
			if !known {
				name = move.Path
			}
			if options.Member != "" && options.Member != name && options.Member != move.Path {
				continue
			}
			entry.Details = append(entry.Details, r.detail(ctx, move, name, state, known, rangeLimit))
		}
		if len(entry.Details) == 0 {
			continue
		}
		message, err := r.run(ctx, r.Dir, "log", "-1", "--format=%B", commit.Commit)
		if err == nil {
			entry.Bumps = org.ParseBumps(message)
		}
		entries = append(entries, entry)
		if len(entries) == limit {
			break
		}
	}
	return entries, nil
}

func (r *Root) detail(ctx context.Context, move org.Move, name string, state org.MemberState, known bool, rangeLimit int) MoveDetail {
	detail := MoveDetail{Move: move, Member: name}
	switch {
	case move.Added():
		detail.Message = "added"
		return detail
	case move.Removed():
		detail.Message = "removed"
		return detail
	case !known || !state.Initialized:
		detail.Message = "member not checked out"
		return detail
	}
	dir := r.MemberDir(state.Member)
	if !r.hasCommit(ctx, dir, move.From) || !r.hasCommit(ctx, dir, move.To) {
		detail.Message = "commits not in this checkout"
		return detail
	}
	spec := move.From + ".." + move.To
	detail.Total = r.count(ctx, dir, spec)
	logged, err := r.run(ctx, dir, "log", "--max-count="+strconv.Itoa(rangeLimit), "--format=%H%x09%s", spec)
	if err == nil {
		for _, line := range strings.Split(logged, "\n") {
			if hash, subject, ok := strings.Cut(line, "\t"); ok {
				detail.Commits = append(detail.Commits, CommitLine{Commit: hash, Subject: subject})
			}
		}
	}
	probe := state
	probe.CodeCommit = move.To
	probe.Arch, probe.Policy = nil, nil
	if err := r.resolveSpec(ctx, &probe, dir); err == nil && probe.Arch != nil && probe.State == org.StateResolved {
		detail.Arch = probe.Arch.Commit
	}
	return detail
}
