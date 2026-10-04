package statedir

import (
	"path/filepath"
	"testing"
)

func TestDirPrecedence(t *testing.T) {
	t.Setenv(EnvStateDir, "")
	t.Setenv(EnvClmDocDir, "")
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if got := Dir(); got != filepath.Join("/xdg", "specd") {
		t.Fatalf("Dir = %q", got)
	}
	if got := ClmDocDir(); got != filepath.Join("/xdg", "specd", "clm") {
		t.Fatalf("ClmDocDir = %q", got)
	}
	t.Setenv(EnvStateDir, "/state")
	if got := ClmDocDir(); got != filepath.Join("/state", "clm") {
		t.Fatalf("ClmDocDir = %q", got)
	}
	t.Setenv(EnvClmDocDir, "/docs")
	if got := ClmDocDir(); got != "/docs" {
		t.Fatalf("ClmDocDir = %q", got)
	}
}
