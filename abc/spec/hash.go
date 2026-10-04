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
