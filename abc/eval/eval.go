package eval

import (
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
)

type Score struct {
	Declared int `json:"declared"`

	Target int `json:"target"`

	Matched int `json:"matched"`

	Missing []string `json:"missing,omitempty"`

	Extra []string `json:"extra,omitempty"`

	Recall float64 `json:"recall"`

	Precision float64 `json:"precision"`

	F1 float64 `json:"f1"`
}

func MatchSets(declared, target []string) Score {
	declaredSet := nameSet(declared)
	targetSet := nameSet(target)
	score := Score{Declared: len(declaredSet), Target: len(targetSet)}
	for name := range declaredSet {
		if targetSet[name] {
			score.Matched++
			continue
		}
		score.Extra = append(score.Extra, name)
	}
	for name := range targetSet {
		if !declaredSet[name] {
			score.Missing = append(score.Missing, name)
		}
	}
	sort.Strings(score.Missing)
	sort.Strings(score.Extra)
	score.Recall = ratio(score.Matched, score.Target)
	score.Precision = ratio(score.Matched, score.Declared)
	if score.Recall+score.Precision > 0 {
		score.F1 = 2 * score.Recall * score.Precision / (score.Recall + score.Precision)
	}
	return score
}

func InterfaceScore(declared []spec.Interface, observed spec.ObservedFacts) Score {
	return MatchSets(interfaceNames(declared), observedNames(observed))
}

func AnchoringRate(requirements []spec.Requirement, observed spec.ObservedFacts) float64 {
	if len(requirements) == 0 {
		return 1
	}
	unresolved := map[string]bool{}
	for _, entry := range specsync.UnresolvedCodeRefs(requirements, observed) {
		if id, _, found := strings.Cut(entry, ": "); found {
			unresolved[id] = true
		}
	}
	anchored := 0
	for _, requirement := range requirements {
		if len(requirement.CodeRefs) > 0 && !unresolved[requirement.ID] {
			anchored++
		}
	}
	return ratio(anchored, len(requirements))
}

func ValidatorPass(name string, declared spec.SystemContextSpec) bool {
	candidate := spec.SystemContext{}
	candidate.Name = name
	candidate.Spec = declared
	return spec.ValidateSystemContext(&candidate).OK()
}

func Jaccard(left, right []string) float64 {
	leftSet := nameSet(left)
	rightSet := nameSet(right)
	if len(leftSet) == 0 && len(rightSet) == 0 {
		return 1
	}
	shared := 0
	union := map[string]bool{}
	for name := range leftSet {
		union[name] = true
		if rightSet[name] {
			shared++
		}
	}
	for name := range rightSet {
		union[name] = true
	}
	return ratio(shared, len(union))
}

type DeltaScore struct {
	Added int `json:"added"`

	Removed int `json:"removed"`

	Changed int `json:"changed"`

	Entries int `json:"entries"`

	Expected int `json:"expected"`

	Precise bool `json:"precise"`
}

func ScoreDelta(change *spec.Delta, expected int) DeltaScore {
	score := DeltaScore{Expected: expected}
	if change != nil {
		counts := change.Count()
		score.Added = counts.Added
		score.Removed = counts.Removed
		score.Changed = counts.Changed
		score.Entries = counts.Added + counts.Removed + counts.Changed
	}
	score.Precise = score.Entries == score.Expected
	return score
}

type InterfaceDelta struct {
	Added []string `json:"added,omitempty"`

	Removed []string `json:"removed,omitempty"`

	Changed []string `json:"changed,omitempty"`
}

func InterfaceDeltaOf(change spec.Delta) InterfaceDelta {
	out := InterfaceDelta{}
	for _, entry := range change.Interfaces {
		switch entry.Op {
		case spec.OpAdded:
			out.Added = append(out.Added, entry.Name)
		case spec.OpRemoved:
			out.Removed = append(out.Removed, entry.Name)
		case spec.OpChanged:
			out.Changed = append(out.Changed, entry.Name)
		}
	}
	sort.Strings(out.Added)
	sort.Strings(out.Removed)
	sort.Strings(out.Changed)
	return out
}

func ScoreInterfaceDelta(change spec.Delta, expected int) (InterfaceDelta, DeltaScore) {
	entries := InterfaceDeltaOf(change)
	score := DeltaScore{
		Added:    len(entries.Added),
		Removed:  len(entries.Removed),
		Changed:  len(entries.Changed),
		Expected: expected,
	}
	score.Entries = score.Added + score.Removed + score.Changed
	score.Precise = score.Entries == score.Expected
	return entries, score
}

func FilesOutsideContext(touched []string, observed spec.ObservedFacts) []string {
	ownedFiles := map[string]bool{}
	ownedDirs := map[string]bool{}
	for _, file := range observed.Files {
		ownedFiles[file] = true
		ownedDirs[directoryOf(file)] = true
	}
	outside := []string{}
	seen := map[string]bool{}
	for _, file := range touched {
		if file == "" || seen[file] || specsync.IsLegacySpecMirrorPath(file) {
			continue
		}
		seen[file] = true
		if ownedFiles[file] || ownedDirs[directoryOf(file)] {
			continue
		}
		outside = append(outside, file)
	}
	sort.Strings(outside)
	return outside
}

func directoryOf(file string) string {
	if index := strings.LastIndex(file, "/"); index >= 0 {
		return file[:index]
	}
	return "."
}

func interfaceNames(declared []spec.Interface) []string {
	out := make([]string, 0, len(declared))
	for _, entry := range declared {
		out = append(out, entry.Name)
	}
	return out
}

func observedNames(observed spec.ObservedFacts) []string {
	out := make([]string, 0, len(observed.Interfaces))
	for _, entry := range observed.Interfaces {
		out = append(out, entry.Name)
	}
	return out
}

func nameSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func ratio(part, whole int) float64 {
	if whole == 0 {
		return 1
	}
	return float64(part) / float64(whole)
}
