package spec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func HashSystemContextSpec(specification SystemContextSpec) (string, error) {
	hash, err := specapi.HashJSON(specification)
	if err != nil {
		return "", fmt.Errorf("spec: hash the spec: %w", err)
	}
	return hash, nil
}

const changeNameHashLength = 12

// ChangeNameCodeToSpec names a CodeToSpec change after the context and the
// commit pair, so a reconcile that runs twice lands on the same object and the
// change is created exactly once.
func ChangeNameCodeToSpec(context, fromCommit, toCommit string) string {
	return context + "-c2s-" + shorten(fromCommit) + "-" + shorten(toCommit)
}

// ChangeNameSpecToCode names a SpecToCode change after the context and the
// spec hash it carries.
func ChangeNameSpecToCode(context, specHash string) string {
	return context + "-s2c-" + shorten(specHash)
}

func shorten(value string) string {
	if len(value) <= changeNameHashLength {
		return value
	}
	return value[:changeNameHashLength]
}

const attemptSuffix = "-a"

// AttemptCount is how many records of one episode exist: the bare name plus
// every numbered retry. It is what a failure cap counts.
func AttemptCount(existing []string, base string) int {
	attempts := 0
	for _, name := range existing {
		if name == base || strings.HasPrefix(name, base+attemptSuffix) {
			attempts++
		}
	}
	return attempts
}

// NextChangeName names the next attempt at one episode. The first attempt
// carries the bare deterministic name; a later one is suffixed with its
// attempt number. A change that failed is therefore retried as a new record
// instead of colliding with its own name and being dropped, while an
// unfinished change still blocks the caller before it ever asks.
func NextChangeName(existing []string, base string) string {
	attempts := AttemptCount(existing, base)
	if attempts == 0 {
		return base
	}
	return base + attemptSuffix + strconv.Itoa(attempts+1)
}
