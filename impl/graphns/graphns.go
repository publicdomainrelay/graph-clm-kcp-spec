package graphns

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
)

const Env = "SPECD_GRAPH_NAMESPACE"

func FromEnv() string {
	return strings.TrimSpace(os.Getenv(Env))
}

func New(prefix string) string {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return prefix
	}
	return prefix + hex.EncodeToString(bytes)
}
