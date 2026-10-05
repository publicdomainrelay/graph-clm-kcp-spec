package spec

import (
	"sort"
	"strconv"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// RetryAttempt is one change specctl retry must create: the next attempt of a
// failed episode, named so it never reuses the name the episode already spent.
type RetryAttempt struct {
	Base string

	Name string

	Attempt int

	Source SpecChange
}

// RetryAttempts names the next attempt for every failed episode of the context
// that has no unsettled change. Attempt counts every change of the episode
// including the failed ones, so the name is <base>-a<N+1> and the history the
// failed attempts carry stays visible in kcp.
func RetryAttempts(changes []SpecChange, context string) []RetryAttempt {
	groups := map[string][]SpecChange{}
	order := []string{}
	for _, change := range changes {
		if change.Spec.SystemContext != context {
			continue
		}
		base := EpisodeBase(change)
		if _, seen := groups[base]; !seen {
			order = append(order, base)
		}
		groups[base] = append(groups[base], change)
	}
	sort.Strings(order)
	out := []RetryAttempt{}
	for _, base := range order {
		group := groups[base]
		unsettled, succeeded, failed := false, false, false
		for _, change := range group {
			switch change.Status.Phase {
			case specapi.PhaseSucceeded:
				succeeded = true
			case specapi.PhaseFailed:
				failed = true
			default:
				unsettled = true
			}
		}
		if unsettled || !failed || succeeded {
			continue
		}
		names := make([]string, 0, len(group))
		for _, change := range group {
			names = append(names, change.Name)
		}
		attempts := AttemptCount(names, base)
		out = append(out, RetryAttempt{
			Base:    base,
			Name:    base + attemptSuffix + strconv.Itoa(attempts+1),
			Attempt: attempts + 1,
			Source:  newestChange(group),
		})
	}
	return out
}

func newestChange(changes []SpecChange) SpecChange {
	newest := changes[0]
	for _, change := range changes[1:] {
		if change.GetCreationTimestamp().Time.After(newest.GetCreationTimestamp().Time) {
			newest = change
			continue
		}
		if change.GetCreationTimestamp().Time.Equal(newest.GetCreationTimestamp().Time) && change.Name > newest.Name {
			newest = change
		}
	}
	return newest
}
