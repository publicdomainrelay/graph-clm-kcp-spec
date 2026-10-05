package policyeval

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

//go:embed lib/specd.rego
var specdLib string

//go:embed lib/specd_test.rego
var specdLibTest string

func Lib() string {
	return specdLib
}

func LibTest() string {
	return specdLibTest
}

func WriteLib(dir string) (bool, error) {
	path := filepath.Join(dir, filepath.FromSlash(policy.LibPath))
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == specdLib {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(specdLib), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
