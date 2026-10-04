package realize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

func TestVerifyPassesAnEmptyCommand(t *testing.T) {
	code, output := verify(context.Background(), nil, t.TempDir(), time.Minute)
	if code != 0 || output != "" {
		t.Errorf("verify with no command = %d, %q", code, output)
	}
}

func TestVerifyReportsTheExitCodeAndTheOutput(t *testing.T) {
	dir := t.TempDir()
	defer func() { _ = os.RemoveAll(dir) }()
	script := filepath.Join(dir, "gate.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho the gate said no\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, output := verify(context.Background(), []string{script}, dir, time.Minute)
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if !strings.Contains(output, "the gate said no") {
		t.Errorf("output = %q, want the command's own words", output)
	}

	passing := filepath.Join(dir, "pass.sh")
	if err := os.WriteFile(passing, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _ := verify(context.Background(), []string{passing}, dir, time.Minute); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestVerifyReportsAMissingCommandAsAFailure(t *testing.T) {
	code, output := verify(context.Background(), []string{"definitely-not-a-command"}, t.TempDir(), time.Minute)
	if code == 0 {
		t.Error("a command that cannot run must not pass")
	}
	if output == "" {
		t.Error("the failure carries no output")
	}
}

func TestVerifyTimesOut(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if code, _ := verify(context.Background(), []string{script}, dir, 300*time.Millisecond); code == 0 {
		t.Error("a command that runs past its timeout must not pass")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("the timeout took %s", elapsed)
	}
}

func TestCommitMessageNamesTheContextAndTheDelta(t *testing.T) {
	change := spec.Delta{
		Requirements: []spec.RequirementDelta{{
			Op: spec.OpAdded, ID: "r.subtract",
			To: &spec.Requirement{ID: "r.subtract", Level: spec.LevelShould, Text: "Subtract."},
		}},
		Interfaces: []spec.InterfaceDelta{{
			Op: spec.OpAdded, Name: "Subtract", To: &spec.Interface{Name: "Subtract"},
		}},
	}
	if got := commitMessage("calc", "calc-s2c-1", "calc", "open-architecture/calc", change); got != "realize calc: +2\n\nSpec-Change: calc-s2c-1\nOpen-Architecture: open-architecture/calc\n" {
		t.Errorf("message = %q", got)
	}
	if got := commitMessage("calc", "", "", "", spec.Delta{}); got != "realize calc: no delta\n\n" {
		t.Errorf("message = %q", got)
	}
}

func TestLimitBoundsTheDiffStat(t *testing.T) {
	values := []string{"a", "b", "c"}
	if got := limit(values, 2); len(got) != 2 || got[1] != "b" {
		t.Errorf("limit = %v", got)
	}
	if got := limit(values, 5); len(got) != 3 {
		t.Errorf("limit = %v", got)
	}
}

func TestRealizeRunNeedsARepository(t *testing.T) {
	_, err := Run(context.Background(), Options{Context: "calc"})
	if err == nil || !strings.Contains(err.Error(), "no repository") {
		t.Errorf("err = %v", err)
	}
}
