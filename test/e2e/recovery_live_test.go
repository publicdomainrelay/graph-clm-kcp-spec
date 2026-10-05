package e2e

import (
	"context"
	"fmt"
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
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

const recoveryRepository = "recovery-calc"

// TestSpecdRestartMidRealizeRequeuesTheRunningChangeLive is the regression test
// for the recovery bug found in phase I: specd is killed while a realize runs,
// the change stays Running with no live driver, and a restarted specd has to
// re-drive it and let the repository's queue move again.
func TestSpecdRestartMidRealizeRequeuesTheRunningChangeLive(t *testing.T) {
	requireLive(t, "kcp", "kine", "kubectl", "bash", "git", "go", "codegraph")
	root := repoRoot(t)
	startCluster(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client := liveClient(t, root)
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("kcp is not serving the specs API: %v", err)
	}
	_, specdBinary := buildSpecctlAndSpecd(t)
	work := t.TempDir()
	repoPath := fixture.CopyAs(t, "calc", recoveryRepository)

	names := []string{"calc", "cmd-calc", recoveryRepository}
	repositories := []string{recoveryRepository, "calc"}
	forgetObjects(t, ctx, client, repositories, names)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		forgetObjects(t, cleanupCtx, client, repositories, names)
	})

	marker := filepath.Join(work, "verify-open")
	verify := writeBlockingVerify(t, work)
	applyTyped(t, ctx, client, &spec.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: recoveryRepository, Namespace: specapi.DefaultNamespace},
		Spec: spec.RepositorySpec{
			Path:   repoPath,
			Branch: "main",
			Verify: []string{verify, marker},
			Agent:  &spec.AgentSpec{Kind: "scripted:" + plan2bScenario(t, "batch.yaml")},
		},
	})
	applyTyped(t, ctx, client, recoveryCalcBaseline())
	applyTyped(t, ctx, client, recoveryCLIBaseline())

	stateDir := filepath.Join(work, "state")
	first := startRecoverySpecd(t, specdBinary, stateDir, work)
	waitFor(t, ctx, "the first ingest of both contexts", func() bool {
		return plan2bRealizedHash(t, ctx, client, "calc") != "" &&
			plan2bRealizedHash(t, ctx, client, "cmd-calc") != ""
	})
	base := headOf(t, repoPath)

	applySpecEdit(t, ctx, client, plan2bSubtractEdit())
	waitFor(t, ctx, "the calc change to block in its realize", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhaseRunning
	})
	orphanName := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)[0].Name

	applySpecEdit(t, ctx, client, plan2bCLISubtractEdit())
	waitFor(t, ctx, "the second change to queue behind the running one", func() bool {
		found := changesFor(liveSpecChanges(t, ctx, client), "cmd-calc", specapi.DirectionSpecToCode)
		return len(found) == 1 && found[0].Status.Phase == specapi.PhasePending
	})

	if err := first.Kill(); err != nil {
		t.Fatalf("kill the first specd: %v", err)
	}
	waitFor(t, ctx, "the first specd to be gone", func() bool { return !first.Running() })

	// The next attempt's verify reads the marker and passes at once.
	if err := os.WriteFile(marker, []byte("open\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := startRecoverySpecd(t, specdBinary, stateDir, work)
	t.Cleanup(func() { second.Stop(t) })

	waitForState(t, ctx, "the recovered change and the queued one to land", func() bool {
		calc := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
		cli := changesFor(liveSpecChanges(t, ctx, client), "cmd-calc", specapi.DirectionSpecToCode)
		return recoveryEpisodeSucceeded(calc) && len(cli) == 1 && cli[0].Status.Phase == specapi.PhaseSucceeded
	}, func() string { return recoveryState(t, ctx, client) })

	orphan := changeNamed(t, ctx, client, orphanName)
	if orphan.Status.Phase != specapi.PhaseFailed {
		t.Errorf("the orphan is %q, want Failed", orphan.Status.Phase)
	}
	if !strings.Contains(orphan.Status.Message, specd.RecoveredReason) {
		t.Errorf("the orphan message = %q, want %q", orphan.Status.Message, specd.RecoveredReason)
	}
	next := changesFor(liveSpecChanges(t, ctx, client), "calc", specapi.DirectionSpecToCode)
	if !recoveryEpisodeSucceeded(next) {
		t.Fatalf("the calc episode did not succeed: %+v", next)
	}
	if headOf(t, repoPath) == base {
		t.Error("the branch did not move: the queue never proceeded")
	}
	for _, change := range liveSpecChanges(t, ctx, client) {
		if change.Status.Phase == specapi.PhaseRunning {
			t.Errorf("%s is still Running after the recovery", change.Name)
		}
	}
	if worktrees := gitOutput(t, repoPath, "worktree", "list", "--porcelain"); strings.Count(worktrees, "worktree ") != 1 {
		t.Errorf("worktrees were left behind:\n%s", worktrees)
	}
	if branches := gitOutput(t, repoPath, "branch", "--list", "spec/*"); branches != "" {
		t.Errorf("leftover realize branches: %q", branches)
	}
	assertNoSpecArtefacts(t, repoPath)
	runGoTest(t, repoPath)
}

func recoveryEpisodeSucceeded(changes []spec.SpecChange) bool {
	for _, change := range changes {
		if change.Status.Phase == specapi.PhaseSucceeded {
			return true
		}
	}
	return false
}

func recoveryState(t *testing.T, ctx context.Context, client *kcpclient.Client) string {
	t.Helper()
	parts := []string{}
	for _, change := range liveSpecChanges(t, ctx, client) {
		parts = append(parts, fmt.Sprintf("%s=%s(%s)", change.Name, change.Status.Phase, change.Status.Message))
	}
	return strings.Join(parts, "; ")
}

func changeNamed(t *testing.T, ctx context.Context, client *kcpclient.Client, name string) spec.SpecChange {
	t.Helper()
	for _, change := range liveSpecChanges(t, ctx, client) {
		if change.Name == name {
			return change
		}
	}
	t.Fatalf("no change named %s", name)
	return spec.SpecChange{}
}

func writeBlockingVerify(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
marker="$1"
tries=0
while [ ! -f "$marker" ]; do
	tries=$((tries + 1))
	if [ "$tries" -gt 900 ]; then
		echo "the verify waited too long for $marker" >&2
		exit 1
	fi
	sleep 1
done
exit 0
`
	target := filepath.Join(dir, "verify.sh")
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func recoveryCalcBaseline() *spec.SystemContext {
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: recoveryRepository,
			Upstream:   spec.RefSelf,
			Intent:     "Arithmetic on two integers, as a library a command line front end wraps.",
			Requirements: []spec.Requirement{
				{ID: "r.add", Level: spec.LevelMust, Text: "Add returns the sum of two integers.", CodeRefs: []string{"function:Add", "file:calc/calc.go"}},
				{ID: "r.multiply", Level: spec.LevelMust, Text: "Multiply returns the product of two integers.", CodeRefs: []string{"function:Multiply", "file:calc/calc.go"}},
			},
			Interfaces: []spec.Interface{
				{Name: "Add", Kind: "function", Signature: "func Add(a, b int) int", File: "calc/calc.go"},
				{Name: "Multiply", Kind: "function", Signature: "func Multiply(a, b int) int", File: "calc/calc.go"},
			},
			CodeRefs: []string{"file:calc/calc.go"},
		},
	}
}

func recoveryCLIBaseline() *spec.SystemContext {
	return &spec.SystemContext{
		ObjectMeta: metav1.ObjectMeta{Name: "cmd-calc", Namespace: specapi.DefaultNamespace},
		Spec: spec.SystemContextSpec{
			Repository: recoveryRepository,
			Upstream:   spec.RefSelf,
			Intent:     "A command line front end over the calc package.",
			Requirements: []spec.Requirement{{
				ID: "r.read-two-operands", Level: spec.LevelMust,
				Text:     "The CLI reads two integers and an operator from the arguments.",
				CodeRefs: []string{"file:cmd/calc/main.go"},
			}},
			CodeRefs: []string{"file:cmd/calc/main.go"},
		},
	}
}

type recoverySpecd struct {
	command *exec.Cmd
	log     *os.File
	path    string
	done    chan struct{}
	err     error
}

func startRecoverySpecd(t *testing.T, binary, stateDir, work string) *recoverySpecd {
	t.Helper()
	logPath := filepath.Join(work, fmt.Sprintf("specd-%d.log", time.Now().UnixNano()))
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary,
		"--kubeconfig", e2eKubeconfig,
		"--workspace", e2eWorkspace,
		"--namespace", specapi.DefaultNamespace,
		"--resync", "500ms",
		"--batch-window", "2s",
		"--retry-backoff", "1s",
		"--max-attempts", "3",
		"--persist-delay", "200ms",
		"--log-level", "debug",
	)
	command.Env = specdEnv(map[string]string{
		"SPECD_KUBECONFIG":  e2eKubeconfig,
		"KUBECONFIG":        e2eKubeconfig,
		"SPECD_WORKSPACE":   e2eWorkspace,
		"SPECD_NAMESPACE":   specapi.DefaultNamespace,
		"SPECD_STATE_DIR":   stateDir,
		"SPECD_CLM_DOC_DIR": filepath.Join(stateDir, "clm"),
	})
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		log.Close()
		t.Fatalf("start specd: %v", err)
	}
	process := &recoverySpecd{command: command, log: log, path: logPath, done: make(chan struct{})}
	go func() {
		process.err = command.Wait()
		close(process.done)
	}()
	return process
}

// specdEnv overlays overrides on the test's environment without leaving a
// duplicate key behind, because the first match of a duplicated variable wins.
func specdEnv(overrides map[string]string) []string {
	out := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[name]; replaced {
			continue
		}
		out = append(out, entry)
	}
	for name, value := range overrides {
		out = append(out, name+"="+value)
	}
	return out
}

func (p *recoverySpecd) Running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *recoverySpecd) Kill() error {
	if !p.Running() {
		return nil
	}
	if err := p.command.Process.Kill(); err != nil {
		return err
	}
	<-p.done
	return nil
}

func (p *recoverySpecd) Stop(t *testing.T) {
	t.Helper()
	if p.Running() {
		if err := p.command.Process.Signal(os.Interrupt); err == nil {
			select {
			case <-p.done:
			case <-time.After(30 * time.Second):
				_ = p.command.Process.Kill()
				<-p.done
			}
		}
	}
	data, _ := os.ReadFile(p.path)
	if t.Failed() && len(data) > 0 {
		t.Logf("specd log tail:\n%s", tailLines(string(data), 40))
	}
	_ = p.log.Close()
}

func tailLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) <= count {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-count:], "\n")
}
