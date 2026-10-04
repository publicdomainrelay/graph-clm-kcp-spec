package graphns

import (
	"strings"
	"testing"
)

func TestFromEnvTrims(t *testing.T) {
	t.Setenv(Env, "  run-1  ")
	if got := FromEnv(); got != "run-1" {
		t.Fatalf("FromEnv = %q", got)
	}
	t.Setenv(Env, "")
	if got := FromEnv(); got != "" {
		t.Fatalf("FromEnv = %q, want empty", got)
	}
}

func TestNewIsPrefixedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		namespace := New("eval-")
		if !strings.HasPrefix(namespace, "eval-") || len(namespace) != len("eval-")+8 {
			t.Fatalf("New = %q", namespace)
		}
		if seen[namespace] {
			t.Fatalf("New repeated %q", namespace)
		}
		seen[namespace] = true
	}
}
