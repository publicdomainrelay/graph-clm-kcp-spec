package org

// ArchEntry is one commit of a member's architecture branch and the code
// commit its repository.yaml says it indexed.
type ArchEntry struct {
	Commit string

	IndexedCommit string
}

// SelectArch picks the architecture commit that describes the pinned code:
// walking newest first, the first whose indexed commit is the pin or an
// ancestor of it. History is newest first. isAncestor(a, b) reports whether a
// is an ancestor of b.
func SelectArch(history []ArchEntry, pinned string, isAncestor func(ancestor, commit string) bool) (ArchEntry, bool) {
	for _, entry := range history {
		if entry.IndexedCommit == "" {
			continue
		}
		if entry.IndexedCommit == pinned {
			return entry, true
		}
		if pinned != "" && isAncestor != nil && isAncestor(entry.IndexedCommit, pinned) {
			return entry, true
		}
	}
	return ArchEntry{}, false
}

// StateOf classifies a member from what was found.
func StateOf(initialized, hasArch, selected bool) State {
	switch {
	case !initialized:
		return StateUninitialized
	case !hasArch:
		return StateUnspecced
	case selected:
		return StateResolved
	default:
		return StateStale
	}
}
