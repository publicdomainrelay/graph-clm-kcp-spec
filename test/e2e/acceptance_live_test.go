package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/factory/specd"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const plan2cGatingRepository = "plan2c-gating"

const plan2cAcceptanceRepository = "plan2c-acceptance"

func plan2cRepository(t *testing.T, name, repoPath string, acceptance []spec.AcceptanceStep) *spec.Repository {
	t.Helper()
	return &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:       repoPath,
			Branch:     "main",
			Verify:     []string{"go", "test", "./..."},
			Acceptance: acceptance,
			Agent:      &spec.AgentSpec{Kind: "scripted:" + plan2bScenario(t, "batch.yaml")},
		},
	}
}

func plan2cCalcBaseline(repository string) *spec.SystemContext {
	baseline := plan2bCalcBaseline()
	baseline.Spec.Repository = repository
	return baseline
}

func TestPlan2cGatingAcceptanceKeepsTheChangeFromLanding(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", plan2cGatingRepository)
	names := []string{"calc", "cmd-calc", plan2cGatingRepository}
	repositories := []string{plan2cGatingRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, plan2cRepository(t, plan2cGatingRepository, repoPath, []spec.AcceptanceStep{{
		Name:    "market",
		Command: []string{"sh", "-c", "echo bob workspace is not up; exit 1"},
		Gate:    true,
	}}))
	applyTyped(t, ctx, client, plan2cCalcBaseline(plan2cGatingRepository))

	startPlan2bController(t, ctx, 1, 2*time.Second)

	waitFor(t, ctx, "the first ingest of calc", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != ""
	})
	base := headOf(t, repoPath)

	applySpecEdit(t, ctx, client, plan2bSubtractEdit())
	waitFor(t, ctx, "the gated change to fail", func() bool {
		for _, change := range changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode) {
			if change.Status.Phase == specapi.PhaseFailed {
				return true
			}
		}
		return false
	})

	found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
	if len(found) != 1 {
		t.Fatalf("spec to code changes = %d, want one: a failed attempt under --max-attempts=1 must not be retried", len(found))
	}
	change := found[0]
	if change.Status.Phase != specapi.PhaseFailed {
		t.Fatalf("phase = %s, want Failed", change.Status.Phase)
	}
	if !strings.Contains(change.Status.Message, "market") {
		t.Errorf("message = %q, want the acceptance step named", change.Status.Message)
	}
	if len(change.Status.Acceptance) != 1 {
		t.Fatalf("acceptance = %+v, want the one failing step recorded", change.Status.Acceptance)
	}
	result := change.Status.Acceptance[0]
	if result.Name != "market" || result.Passed || result.ExitCode != 1 {
		t.Errorf("acceptance result = %+v, want market failed with exit 1", result)
	}
	if !strings.Contains(result.OutputTail, "bob workspace is not up") {
		t.Errorf("outputTail = %q, want the step's output", result.OutputTail)
	}
	if result.DurationSeconds <= 0 {
		t.Errorf("durationSeconds = %v, want the wall time", result.DurationSeconds)
	}
	if head := headOf(t, repoPath); head != base {
		t.Errorf("HEAD = %s, want the untouched base %s: a gating acceptance failure must not commit", head, base)
	}
	if count := gitOutput(t, repoPath, "rev-list", "--count", base+"..HEAD"); count != "0" {
		t.Errorf("the branch advanced by %s commit(s), want none", count)
	}
	if branches := gitOutput(t, repoPath, "branch", "--list", "spec/calc/*"); !strings.Contains(branches, "spec/calc/") {
		t.Errorf("the realize branch was not kept: %q", branches)
	}
	runGoTest(t, repoPath)
}

func TestPlan2cAcceptanceResultsLandWithTheCommit(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "codegraph", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}

	repoPath := fixture.CopyAs(t, "calc", plan2cAcceptanceRepository)
	names := []string{"calc", "cmd-calc", plan2cAcceptanceRepository}
	repositories := []string{plan2cAcceptanceRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	applyTyped(t, ctx, client, plan2cRepository(t, plan2cAcceptanceRepository, repoPath, []spec.AcceptanceStep{
		{Name: "unit", Command: []string{"sh", "-c", "echo unit ok"}, Gate: true},
		{Name: "e2e", Command: []string{"sh", "-c", "echo the market is down; exit 4"}, Gate: false},
	}))
	applyTyped(t, ctx, client, plan2cCalcBaseline(plan2cAcceptanceRepository))

	startPlan2bController(t, ctx, specd.DefaultMaxAttempts, 2*time.Second)

	waitFor(t, ctx, "the first ingest of calc", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != ""
	})
	base := headOf(t, repoPath)

	applySpecEdit(t, ctx, client, plan2bSubtractEdit())
	waitFor(t, ctx, "the change to land", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseSucceeded
	})

	change := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0]
	if change.Status.Commit == "" || headOf(t, repoPath) != change.Status.Commit {
		t.Fatalf("commit = %q, HEAD = %q: a report-only failure must not block the commit", change.Status.Commit, headOf(t, repoPath))
	}
	if count := gitOutput(t, repoPath, "rev-list", "--count", base+"..HEAD"); count != "1" {
		t.Errorf("the branch advanced by %s commit(s), want 1", count)
	}
	if len(change.Status.Acceptance) != 2 {
		t.Fatalf("acceptance = %+v, want both steps recorded", change.Status.Acceptance)
	}
	unit, report := change.Status.Acceptance[0], change.Status.Acceptance[1]
	if !unit.Passed || unit.ExitCode != 0 {
		t.Errorf("unit = %+v, want passed", unit)
	}
	if report.Passed || report.ExitCode != 4 || report.Name != "e2e" {
		t.Errorf("e2e = %+v, want the report-only failure recorded", report)
	}
	message := gitOutput(t, repoPath, "log", "-1", "--format=%B", change.Status.Commit)
	for _, want := range []string{"Acceptance: unit passed (gate)", "Acceptance: e2e failed (report)"} {
		if !strings.Contains(message, want) {
			t.Errorf("the commit message lacks %q:\n%s", want, message)
		}
	}
	runGoTest(t, repoPath)
}

func TestPlan2cSpecctlAcceptRunsAgainstTheTree(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "git", "go")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	specctl, _ := buildSpecctlAndSpecd(t)

	passing := fixture.CopyAs(t, "calc", "accept-pass")
	failing := fixture.CopyAs(t, "calc", "accept-fail")
	repositories := []string{"accept-pass", "accept-fail"}
	forgetObjects(t, ctx, client, repositories, nil)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, nil)
	})

	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "accept-pass", Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{Path: passing, Acceptance: []spec.AcceptanceStep{
			{Name: "unit", Command: []string{"sh", "-c", "echo unit ok"}, Gate: true},
			{Name: "market", Command: []string{"sh", "-c", "echo the market is down; exit 3"}, Gate: false},
		}},
	})
	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "accept-fail", Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{Path: failing, Acceptance: []spec.AcceptanceStep{
			{Name: "market", Command: []string{"sh", "-c", "echo bob is not running; exit 2"}, Gate: true},
		}},
	})

	stdout, stderr, code := runSpecctlAccept(t, specctl, passing)
	if code != exitOKCode {
		t.Fatalf("specctl accept on a passing tree = %d\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"accept unit: passed", "accept market: failed", "the market is down"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the output lacks %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runSpecctlAccept(t, specctl, failing, "--name", "market")
	if code != exitErrorCode {
		t.Fatalf("specctl accept on a gating failure = %d, want %d\n%s\n%s", code, exitErrorCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "accept market: failed") {
		t.Errorf("the output lacks the failing step:\n%s", stdout)
	}
	if !strings.Contains(stderr, "gates the commit") {
		t.Errorf("the error does not say the step gates:\n%s", stderr)
	}
}

const exitOKCode = 0

const exitErrorCode = 1

func runSpecctlAccept(t *testing.T, specctl, dir string, extra ...string) (string, string, int) {
	t.Helper()
	args := append([]string{
		"accept", "--repo", dir,
		"--kubeconfig", e2eKubeconfig,
		"--workspace", e2eWorkspace,
	}, extra...)
	command := exec.Command(specctl, args...)
	command.Env = append(os.Environ(), "SPECD_STATE_DIR="+filepath.Join(t.TempDir(), "state"))
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		t.Fatalf("run specctl accept: %v\n%s", err, stderr.String())
	}
	return stdout.String(), stderr.String(), exitErr.ExitCode()
}
