package specd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "specd-clm-docs-")
	if err != nil {
		panic(err)
	}
	os.Setenv(statedir.EnvClmDocDir, dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func contextDocPath(repository, context string) string {
	return filepath.Join(os.Getenv(statedir.EnvClmDocDir), repository, context+".md")
}
