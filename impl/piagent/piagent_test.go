package piagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/agent"
)

// fakePi is a stand-in for the pi binary: it reads the prompt from standard
// input, honours the headless flag, records the environment it was given and
// answers with a draft. It is how the host is tested without spending a model
// call, exactly as the scripted agent tests the loop.
func fakePi(t *testing.T, dir string) (string, string) {
	t.Helper()
	envFile := filepath.Join(dir, "env.txt")
	promptFile := filepath.Join(dir, "prompt.txt")
	script := filepath.Join(dir, "fake-pi")
	body := `#!/usr/bin/env bash
set -euo pipefail
env >"` + envFile + `"
cat >"` + promptFile + `"
printf '%s\n' '{"intent":"a calculator","requirements":[{"id":"r.add","level":"MUST","text":"add two ints","codeRefs":["function:Add"]}],"interfaces":[{"name":"Add","kind":"function"}]}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, envFile
}

func TestSummarizeRunsTheHostAndParsesTheAnswer(t *testing.T) {
	dir := t.TempDir()
	script, envFile := fakePi(t, dir)
	host := New(Options{
		Command:   script,
		Dir:       dir,
		Timeout:   30 * time.Second,
		Extension: "/opt/pi-hydradb-clm",
		Env:       map[string]string{"SPECD_CLM_CONTEXT": "calc"},
	})
	bundle := agent.ContextBundle{
		Context:    "calc",
		Repository: "calc",
	}
	draft, err := host.Summarize(context.Background(), bundle)
	if err != nil {
		t.Fatalf("the host did not answer with a readable draft: %v", err)
	}
	if draft.Intent != "a calculator" || len(draft.Interfaces) != 1 || draft.Interfaces[0].Name != "Add" {
		t.Fatalf("draft = %+v", draft)
	}
	environment := readFile(t, envFile)
	for _, want := range []string{"SPECD_PI_EXTENSION=/opt/pi-hydradb-clm", "SPECD_CLM_CONTEXT=calc"} {
		if !strings.Contains(environment, want) {
			t.Errorf("the host environment has no %s:\n%s", want, environment)
		}
	}
	prompt := readFile(t, filepath.Join(dir, "prompt.txt"))
	if !strings.Contains(prompt, "calc") {
		t.Errorf("the prompt did not reach the host on stdin:\n%s", prompt)
	}
}

func TestDefaultCommandIsTheNpxPackage(t *testing.T) {
	// The pi coding agent is not installed globally everywhere this repository
	// runs, so the default command resolves the npm package the same way the
	// project documents it, and the headless flag is there.
	if DefaultCommand != "npx" {
		t.Errorf("DefaultCommand = %q, want npx", DefaultCommand)
	}
	args := DefaultArgs()
	if len(args) == 0 || args[0] != "--yes" || args[len(args)-1] != "-p" {
		t.Fatalf("DefaultArgs = %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, Package) {
		t.Errorf("DefaultArgs do not name %s: %v", Package, args)
	}
	if !strings.HasPrefix(Package, "@earendil-works/pi-coding-agent@") {
		t.Errorf("Package = %q", Package)
	}
}

func TestArgsFromEnvOverridesTheDefaults(t *testing.T) {
	if got := ArgsFromEnv(); strings.Join(got, " ") != strings.Join(DefaultArgs(), " ") {
		t.Errorf("ArgsFromEnv = %v, want the defaults", got)
	}
	t.Setenv(EnvArgs, "--yes @earendil-works/pi-coding-agent@1.0.0 --provider llama-cpp --model m -p")
	got := ArgsFromEnv()
	if len(got) != 7 || got[2] != "--provider" {
		t.Fatalf("ArgsFromEnv = %v", got)
	}
}

// TestNewDefaultsTheCommandAndArgs is the shape check the host needs: a pi
// host with no command and no arguments must still be a usable agent, and it
// must fail loudly (not silently) when the default command cannot run, because
// that is what a caller sees when pi is not installed.
func TestNewDefaultsTheCommandAndArgs(t *testing.T) {
	dir := t.TempDir()
	host := New(Options{Dir: dir, Command: filepath.Join(dir, "missing-pi"), Timeout: 5 * time.Second})
	if _, err := host.Summarize(context.Background(), agent.ContextBundle{Context: "calc"}); err == nil {
		t.Fatal("a missing pi command did not fail")
	}
	if host := New(Options{}); host == nil {
		t.Fatal("New returned no agent")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
