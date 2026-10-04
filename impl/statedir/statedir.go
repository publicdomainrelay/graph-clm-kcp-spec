package statedir

import (
	"os"
	"path/filepath"
)

const (
	EnvStateDir = "SPECD_STATE_DIR"

	EnvClmDocDir = "SPECD_CLM_DOC_DIR"
)

func Dir() string {
	if value := os.Getenv(EnvStateDir); value != "" {
		return value
	}
	if value := os.Getenv("XDG_STATE_HOME"); value != "" {
		return filepath.Join(value, "specd")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "specd")
	}
	return filepath.Join(os.TempDir(), "specd")
}

func ClmDocDir() string {
	if value := os.Getenv(EnvClmDocDir); value != "" {
		return value
	}
	return filepath.Join(Dir(), "clm")
}
