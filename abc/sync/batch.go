package specsync

import (
	"sort"
	"time"

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
