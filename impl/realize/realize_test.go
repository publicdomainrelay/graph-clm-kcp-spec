package realize

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/coverage"
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
	single := []Member{{Context: "calc", Change: "calc-s2c-1", Delta: change}}
	if got := commitMessage(single, "open-architecture/calc", nil, nil); got != "realize calc: +2\n\nSpec-Change: calc-s2c-1\nOpen-Architecture: open-architecture/calc\n" {
		t.Errorf("message = %q", got)
	}
	if got := commitMessage([]Member{{Context: "calc"}}, "", nil, nil); got != "realize calc: no delta\n\n" {
		t.Errorf("message = %q", got)
	}
}

func TestCommitMessageCarriesOneTrailerPerBatchMember(t *testing.T) {
	members := []Member{
		{Context: "calc", Change: "calc-s2c-1", Delta: spec.Delta{Requirements: []spec.RequirementDelta{{
			Op: spec.OpAdded, ID: "r.subtract", To: &spec.Requirement{ID: "r.subtract", Level: spec.LevelShould, Text: "Subtract."},
		}}}},
		{Context: "cmd-calc", Change: "cmd-calc-s2c-2", Delta: spec.Delta{Interfaces: []spec.InterfaceDelta{{
			Op: spec.OpAdded, Name: "Main", To: &spec.Interface{Name: "Main"},
		}}}},
	}
	message := commitMessage(members, "open-architecture/calc", nil, nil)
	want := "realize calc, cmd-calc: +2\n\nSpec-Change: calc-s2c-1\nSpec-Change: cmd-calc-s2c-2\nOpen-Architecture: open-architecture/calc\n"
	if message != want {
		t.Errorf("message = %q, want %q", message, want)
	}
}

func TestCommitMessageNamesEveryContextOfTheBatch(t *testing.T) {
	members := []Member{
		{Context: "lib-did-key-ingress-proxy", Change: "a-s2c-1"},
		{Context: "lib-common-cloud-init-common", Change: "b-s2c-2"},
		{Context: "request-vm-ssh", Change: "c-s2c-3"},
		{Context: "lib-common-cloud-init-common", Change: "d-s2c-4"},
	}
	subject := strings.SplitN(commitMessage(members, "", nil, nil), "\n", 2)[0]
	if subject != "realize lib-did-key-ingress-proxy, lib-common-cloud-init-common, request-vm-ssh: no delta" {
		t.Errorf("subject = %q", subject)
	}
	for _, member := range members {
		if !strings.Contains(subject, member.Context) {
			t.Errorf("subject %q does not name %s", subject, member.Context)
		}
	}
}

func TestCoverBatchFeedsTheDiffToTheJudge(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, "calc.go"), []byte("package calc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "base")
	base := gitRun(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "calc.go"), []byte("package calc\n\nfunc Subtract(a, b int) int { return a - b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "subtract")
	commit := gitRun(t, dir, "rev-parse", "HEAD")

	script := filepath.Join(dir, "judge.sh")
	if err := os.WriteFile(script, []byte(`#!/usr/bin/env bash
prompt=$(cat)
if grep -q "r.sub" <<<"$prompt" && grep -q "+func Subtract" <<<"$prompt"; then
  printf '{"verdicts":[{"id":"r.sub","implemented":true,"evidence":"func Subtract"}]}\n'
else
  printf '{"verdicts":[{"id":"r.sub","implemented":false,"evidence":"the diff was not shown"}],"extra":"x"}\n'
fi
`), 0o755); err != nil {
		t.Fatal(err)
	}
	judge := coverage.New(coverage.Options{Command: "bash", Args: []string{script}, Dir: dir, Timeout: 30 * time.Second})
	targets := []target{{member: Member{
		Context: "calc",
		Change:  "calc-s2c-1",
		Delta: spec.Delta{Requirements: []spec.RequirementDelta{{
			Op: spec.OpAdded, ID: "r.sub", To: &spec.Requirement{ID: "r.sub", Level: spec.LevelMust, Text: "Subtract returns the difference."},
		}}},
	}}}
	repository := &spec.Repository{ObjectMeta: metav1.ObjectMeta{Name: "calc"}}
	repository.Status.ResolvedPath = dir
	verdicts, failure := coverBatch(context.Background(), Options{Coverage: judge, Repository: repository}, targets, base, commit)
	if failure != "" {
		t.Fatalf("coverBatch: %s", failure)
	}
	got := verdicts["calc-s2c-1"]
	if len(got) != 1 || !got[0].Implemented || got[0].ID != "r.sub" {
		t.Fatalf("verdicts = %+v, want r.sub implemented from the diff", got)
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRunAcceptanceRecordsEachStep(t *testing.T) {
	dir := t.TempDir()
	steps := []spec.AcceptanceStep{
		{Name: "unit", Command: []string{"sh", "-c", "echo unit ok"}, Gate: true},
		{Name: "env", Command: []string{"sh", "-c", "test \"$BOB_WORKSPACE\" = bob && echo workspace ok"}, Gate: true, Env: map[string]string{"BOB_WORKSPACE": "bob"}},
		{Name: "market", Command: []string{"sh", "-c", "echo the market is not up; exit 3"}, Gate: false},
	}
	results := RunAcceptance(context.Background(), steps, dir, time.Minute)
	if len(results) != 3 {
		t.Fatalf("results = %d, want one per step", len(results))
	}
	for index, want := range []struct {
		name   string
		passed bool
		code   int
	}{{"unit", true, 0}, {"env", true, 0}, {"market", false, 3}} {
		got := results[index]
		if got.Name != want.name || got.Passed != want.passed || got.ExitCode != want.code {
			t.Errorf("result %d = %+v, want %s passed=%v code=%d", index, got, want.name, want.passed, want.code)
		}
	}
	if !strings.Contains(results[0].OutputTail, "unit ok") || !strings.Contains(results[2].OutputTail, "the market is not up") {
		t.Errorf("output tails = %q and %q", results[0].OutputTail, results[2].OutputTail)
	}
	if results[0].DurationSeconds <= 0 {
		t.Errorf("duration = %v, want the wall time", results[0].DurationSeconds)
	}
	empty := RunAcceptance(context.Background(), []spec.AcceptanceStep{{Name: "none"}}, dir, time.Minute)
	if len(empty) != 1 || empty[0].Passed || empty[0].ExitCode != -1 {
		t.Errorf("a step without a command = %+v, want a recorded failure", empty)
	}
}

func TestRunAcceptanceReapsWhatAStepLeavesBehind(t *testing.T) {
	dir := t.TempDir()
	steps := []spec.AcceptanceStep{
		{Name: "daemon", Command: []string{"sh", "-c", "sleep 120 & echo $! > bg.pid; echo started"}, Gate: true},
		{Name: "timeout", Command: []string{"sh", "-c", "sleep 120 & echo $! > blocked.pid; sleep 120"}, TimeoutSeconds: 2, Gate: false},
	}
	results := RunAcceptance(context.Background(), steps, dir, time.Minute)
	if !results[0].Passed {
		t.Fatalf("the daemon step did not pass: %+v", results[0])
	}
	if results[1].Passed {
		t.Fatalf("the blocking step passed: %+v", results[1])
	}
	for _, name := range []string{"bg.pid", "blocked.pid"} {
		pid := readPid(t, filepath.Join(dir, name))
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("the step left %s (%d) running", name, pid)
		}
	}
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func processAlive(pid int) bool {
	for range 50 {
		if err := syscall.Kill(pid, 0); err != nil {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

func TestRunGatesStopsAtTheFirstGate(t *testing.T) {
	dir := t.TempDir()
	repository := &spec.Repository{Spec: spec.RepositorySpec{
		Verify: []string{"sh", "-c", "echo verify said no; exit 1"},
		Acceptance: []spec.AcceptanceStep{
			{Name: "market", Command: []string{"sh", "-c", "echo acceptance ran"}, Gate: true},
		},
	}}
	result := Result{}
	err := runGates(context.Background(), repository, dir, time.Minute, time.Minute, &result)
	if _, ok := errors.AsType[*VerifyError](err); !ok {
		t.Fatalf("err = %v, want a VerifyError", err)
	}
	if len(result.Acceptance) != 0 {
		t.Errorf("acceptance ran after verify failed: %+v", result.Acceptance)
	}

	repository.Spec.Verify = []string{"true"}
	repository.Spec.Acceptance = []spec.AcceptanceStep{{Name: "market", Command: []string{"sh", "-c", "echo bob is down; exit 2"}, Gate: true}}
	result = Result{}
	err = runGates(context.Background(), repository, dir, time.Minute, time.Minute, &result)
	acceptanceErr, ok := errors.AsType[*AcceptanceError](err)
	if !ok || acceptanceErr.Result.Name != "market" {
		t.Fatalf("err = %v, want an AcceptanceError naming market", err)
	}
	if len(result.Acceptance) != 1 || result.Acceptance[0].Passed {
		t.Errorf("acceptance = %+v, want the failing step recorded", result.Acceptance)
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
