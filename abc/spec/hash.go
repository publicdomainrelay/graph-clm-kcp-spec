package spec

import (
	"fmt"

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
