package spec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func HashSystemContextSpec(specification SystemContextSpec) (string, error) {
	hash, err := specapi.HashJSON(Canonicalize(specification))
	if err != nil {
		return "", fmt.Errorf("spec: hash the spec: %w", err)
	}
	return hash, nil
}

const changeNameHashLength = 12

func ChangeNameCodeToSpec(context, fromCommit, toCommit string) string {
	return context + "-c2s-" + shorten(fromCommit) + "-" + shorten(toCommit)
}

func ChangeNameSpecToCode(context, specHash string) string {
	return context + "-s2c-" + shorten(specHash)
}

func shorten(value string) string {
	if value == "" {
		return "none"
	}
	if len(value) <= changeNameHashLength {
		return value
	}
	return value[:changeNameHashLength]
}

// EpisodeBase names the change a later attempt repeats: the same context and
// the same code or spec hash, whatever the attempt's own name is.
func EpisodeBase(change SpecChange) string {
	switch change.Spec.Direction {
	case specapi.DirectionCodeToSpec:
		return ChangeNameCodeToSpec(change.Spec.SystemContext, change.Spec.FromCommit, change.Spec.ToCommit)
	case specapi.DirectionSpecToCode:
		return ChangeNameSpecToCode(change.Spec.SystemContext, change.Spec.ToSpecHash)
	}
	return change.Name
}

const attemptSuffix = "-a"

func AttemptCount(existing []string, base string) int {
	attempts := 0
	for _, name := range existing {
		if ChangeNameMatches(name, base) {
			attempts++
		}
	}
	return attempts
}

func ChangeNameMatches(name, base string) bool {
	return name == base || strings.HasPrefix(name, base+attemptSuffix)
}

func NextChangeName(existing []string, base string) string {
	attempts := AttemptCount(existing, base)
	if attempts == 0 {
		return base
	}
	return base + attemptSuffix + strconv.Itoa(attempts+1)
}

// EpisodeOpen reports whether an unsettled change already realizes this
// episode: the same context and target hash, not yet Succeeded or Failed. An
// open episode blocks a second change for it, but not a change for a different
// target hash, so a spec edit applied while an earlier one is still running
// gets its own change to queue behind it.
func EpisodeOpen(changes []SpecChange, base string) bool {
	for _, change := range changes {
		if !ChangeNameMatches(change.Name, base) {
			continue
		}
		switch change.Status.Phase {
		case specapi.PhaseSucceeded, specapi.PhaseFailed:
			continue
		default:
			return true
		}
	}
	return false
}
