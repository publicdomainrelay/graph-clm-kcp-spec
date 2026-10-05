package specsync

import (
	"sort"
	"strings"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

type ChangeRef struct {
	Name string

	SystemContext string

	Repository string

	Direction string

	Phase string

	Commit string

	CreatedAt time.Time

	DependsOn []string
}

func (c ChangeRef) specToCode() bool {
	return c.Direction == specapi.DirectionSpecToCode
}

func olderChange(left, right ChangeRef) bool {
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	return left.Name < right.Name
}

func orderChanges(changes []ChangeRef) []ChangeRef {
	out := append([]ChangeRef{}, changes...)
	sort.SliceStable(out, func(left, right int) bool {
		return olderChange(out[left], out[right])
	})
	return out
}

func PendingForRepository(changes []ChangeRef, repository string) []ChangeRef {
	out := []ChangeRef{}
	for _, change := range changes {
		if change.Repository != repository || !change.specToCode() {
			continue
		}
		if change.Phase != specapi.PhasePending {
			continue
		}
		out = append(out, change)
	}
	return orderChanges(out)
}

func BatchMembers(pending []ChangeRef) []ChangeRef {
	return orderChanges(pending)
}

func BatchLeader(pending []ChangeRef) (ChangeRef, bool) {
	ordered := BatchMembers(pending)
	if len(ordered) == 0 {
		return ChangeRef{}, false
	}
	return ordered[0], true
}

func BatchGatherWait(leader ChangeRef, now time.Time, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	elapsed := now.Sub(leader.CreatedAt)
	if elapsed >= window {
		return 0
	}
	return window - elapsed
}

func RepositoryBusy(changes []ChangeRef, repository string) bool {
	for _, change := range changes {
		if change.Repository != repository || !change.specToCode() {
			continue
		}
		if change.Phase == specapi.PhaseRunning {
			return true
		}
	}
	return false
}

func SiblingLanded(changes []ChangeRef, repository, base, head string, members []string) bool {
	if head == "" || head == base {
		return false
	}
	own := make(map[string]bool, len(members))
	for _, name := range members {
		own[name] = true
	}
	for _, change := range changes {
		if change.Repository != repository || !change.specToCode() {
			continue
		}
		if own[change.Name] || change.Phase != specapi.PhaseSucceeded {
			continue
		}
		if change.Commit == head {
			return true
		}
	}
	return false
}

// OrderBatch orders a batch so a change whose context depends on another
// changed context comes after it, which is the order the agent applies the
// deltas in. Members the relation cannot order - an independent change, or a
// cycle - keep their input order.
func OrderBatch(changes []ChangeRef) []ChangeRef {
	out := append([]ChangeRef{}, changes...)
	if len(out) < 2 {
		return out
	}
	position := make(map[string]int, len(out))
	for index, change := range out {
		position[change.SystemContext] = index
	}
	emitted := make([]bool, len(out))
	ordered := make([]ChangeRef, 0, len(out))
	for len(ordered) < len(out) {
		progress := false
		for index, change := range out {
			if emitted[index] || !dependenciesMet(change, position, emitted) {
				continue
			}
			emitted[index] = true
			ordered = append(ordered, change)
			progress = true
		}
		if progress {
			continue
		}
		for index, change := range out {
			if emitted[index] {
				continue
			}
			emitted[index] = true
			ordered = append(ordered, change)
		}
	}
	return ordered
}

func dependenciesMet(change ChangeRef, position map[string]int, emitted []bool) bool {
	for _, ref := range change.DependsOn {
		name, ok := dependencyContext(ref)
		if !ok {
			continue
		}
		if at, member := position[name]; member && !emitted[at] {
			return false
		}
	}
	return true
}

func dependencyContext(ref string) (string, bool) {
	if !strings.HasPrefix(ref, spec.RefPrefixContext) {
		return "", false
	}
	name := strings.TrimPrefix(ref, spec.RefPrefixContext)
	return name, name != ""
}

// FilesOwnedElsewhere names the files a realize touched that a context which is
// not a member of the batch owns, mapped to the owning contexts. A file no
// context observes is new code for a changed context and is not named here.
func FilesOwnedElsewhere(touched, members []string, observed map[string]spec.ObservedFacts) map[string][]string {
	member := make(map[string]bool, len(members))
	for _, name := range members {
		member[name] = true
	}
	owners := map[string][]string{}
	for name, facts := range observed {
		if member[name] {
			continue
		}
		for _, file := range append(append([]string{}, facts.Files...), facts.TreeFiles...) {
			owners[file] = append(owners[file], name)
		}
	}
	out := map[string][]string{}
	for _, file := range touched {
		if file == "" {
			continue
		}
		if names := owners[file]; len(names) > 0 {
			sorted := append([]string{}, names...)
			sort.Strings(sorted)
			out[file] = sorted
		}
	}
	return out
}
