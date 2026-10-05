package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/runlock"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/session"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/statedir"
)

const (
	envKubeconfig = "SPECD_E2E_KUBECONFIG"

	envWorkspace = "SPECD_E2E_WORKSPACE"

	envStateRoot = "SPECD_E2E_STATE_ROOT"

	defaultWorkspace = "root:specs"

	defaultNamespace = "default"
)

var (
	e2eRoot = ""

	e2eStateRoot = ""

	e2eKubeconfig = ""

	e2eWorkspace = defaultWorkspace

	e2eNamespace = defaultNamespace

	e2eWorkspaceKubeconfig = ""

	e2eInstance = kcpproc.Instance{}

	e2ePrivate = false

	e2eTools = "kcp kine kubectl bash"
)

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(m.Run())
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: locate the repository root: %v\n", err)
		os.Exit(1)
	}
	e2eRoot = root
	if os.Getenv(graphns.Env) == "" {
		os.Setenv(graphns.Env, graphns.New("e2e-"))
	}

	createdDocs := ""
	if os.Getenv(statedir.EnvClmDocDir) == "" {
		docs, err := os.MkdirTemp("", "specd-e2e-clm-docs-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
			os.Exit(1)
		}
		os.Setenv(statedir.EnvClmDocDir, docs)
		createdDocs = docs
	}

	ledger, err := os.MkdirTemp("", "specd-e2e-ledger-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}
	os.Setenv(kcpproc.LedgerEnv, filepath.Join(ledger, "ledger"))

	lock, err := startClusterForTests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}
	if e2eKubeconfig != "" {
		os.Setenv("SPECD_KUBECONFIG", e2eKubeconfig)
		os.Setenv("SPECD_WORKSPACE", e2eWorkspace)
	}
	mark(os.Getenv("SPECD_LIVE_LOCK_MARK"), "begin")
	if want, err := strconv.Atoi(os.Getenv("SPECD_LIVE_LOCK_BARRIER")); err == nil && want > 1 {
		barrier(os.Getenv("SPECD_LIVE_LOCK_MARK"), want)
	}
	code := m.Run()
	mark(os.Getenv("SPECD_LIVE_LOCK_MARK"), "end")
	stopClusterForTests(lock)
	leaks := kcpproc.Leaks()
	for _, leak := range leaks {
		fmt.Fprintf(os.Stderr, "e2e: leaked kcp %d, kine %d and specd %d on the root %s\n", leak.KcpPid, leak.KinePid, leak.SpecdPid, leak.Root)
		kcpproc.Terminate(leak.KcpPid, leak.Root)
		kcpproc.Terminate(leak.KinePid, leak.Root)
		kcpproc.Terminate(leak.SpecdPid, leak.Root)
	}
	if len(leaks) > 0 {
		code = 1
	}
	if createdDocs != "" {
		os.RemoveAll(createdDocs)
	}
	os.RemoveAll(ledger)
	os.Exit(code)
}

func startClusterForTests() (*runlock.Lock, error) {
	if missing := missingTools(); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "e2e: live prerequisites missing: %s; every live test skips\n", strings.Join(missing, ", "))
		return nil, nil
	}
	if named := os.Getenv(envKubeconfig); named != "" {
		absolute, err := filepath.Abs(named)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(absolute); err != nil {
			return nil, fmt.Errorf("%s names %s, which cannot be read: %w", envKubeconfig, absolute, err)
		}
		e2eKubeconfig = absolute
		e2eWorkspace = envOr("SPECD_E2E_WORKSPACE", defaultWorkspace)
		e2eStateRoot = envOr(envStateRoot, filepath.Dir(absolute))
		lockPath, named := os.LookupEnv("SPECD_LIVE_LOCK")
		if !named {
			lockPath = runlock.PathFor(e2eStateRoot, absolute)
		}
		var lock *runlock.Lock
		if lockPath != "" {
			lock, err = runlock.Acquire(lockPath, runlock.Options{Name: "go test ./test/e2e"})
			if err != nil {
				return nil, err
			}
		}
		if err := installSpecs(e2eStateRoot, absolute, e2eWorkspace); err != nil {
			if lock != nil {
				lock.Release()
			}
			return nil, err
		}
		return lock, nil
	}

	stateRoot, err := os.MkdirTemp("", "specd-e2e-kcp-")
	if err != nil {
		return nil, err
	}
	instance, err := kcpproc.Start(context.Background(), kcpproc.Options{Root: stateRoot, DieWithStarter: true})
	if err != nil {
		os.RemoveAll(stateRoot)
		return nil, err
	}
	e2ePrivate = true
	e2eInstance = instance
	e2eStateRoot = stateRoot
	e2eKubeconfig = instance.AdminKubeconfig
	e2eWorkspace = envOr("SPECD_E2E_WORKSPACE", defaultWorkspace)
	if err := installSpecs(e2eStateRoot, e2eKubeconfig, e2eWorkspace); err != nil {
		kcpproc.Stop(instance)
		os.RemoveAll(stateRoot)
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "e2e: private kcp on kernel ports at %s (kine %s), state %s\n", instance.KcpURL, instance.KineURL, stateRoot)
	return nil, nil
}

func stopClusterForTests(lock *runlock.Lock) {
	if lock != nil {
		if err := lock.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "e2e: release the live lock: %v\n", err)
		}
	}
	if e2ePrivate {
		kcpproc.Stop(e2eInstance)
		os.RemoveAll(e2eStateRoot)
	}
}

func installSpecs(stateRoot, kubeconfig, workspace string) error {
	deployDir, err := session.ExtractDeploy()
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(workspace, "root:")
	workspaceKubeconfig := filepath.Join(stateRoot, name+".kubeconfig")
	command := exec.Command("bash", filepath.Join(deployDir, "install-specs.sh"))
	command.Env = append(os.Environ(),
		"ROOT="+stateRoot,
		"KUBECONFIG_PATH="+kubeconfig,
		"SPECS_WORKSPACE="+name,
		"SPECS_NAMESPACE="+e2eNamespace,
		"WORKSPACE_KUBECONFIG="+workspaceKubeconfig,
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("install-specs.sh: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	e2eWorkspaceKubeconfig = workspaceKubeconfig
	return nil
}

func missingTools() []string {
	missing := []string{}
	for _, tool := range strings.Fields(e2eTools) {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing
}

func mark(path, event string) {
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s %d %d\n", event, os.Getpid(), time.Now().UnixNano())
}

// barrier holds a run at its "begin" mark until `want` distinct runs have
// written one, so two runs that must overlap do so by construction and not by
// timing. A run that never arrives releases the others on the deadline.
func barrier(path string, want int) {
	if path == "" {
		return
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if begunRuns(path) >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "e2e: the barrier at %s saw %d of %d runs\n", path, begunRuns(path), want)
}

func begunRuns(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pids := map[int]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "begin" {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		pids[pid] = true
	}
	return len(pids)
}

func ensureCluster(t *testing.T) {
	t.Helper()
	if missing := missingTools(); len(missing) > 0 {
		if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
			t.Fatalf("SPECD_REQUIRE_LIVE=1 but these tools are missing: %s", strings.Join(missing, ", "))
		}
		t.Skipf("live prerequisites missing: %s", strings.Join(missing, ", "))
	}
	if e2eKubeconfig == "" {
		t.Fatal("the e2e cluster was not started; see the TestMain output")
	}
	if _, err := os.Stat(e2eKubeconfig); err != nil {
		t.Fatalf("the e2e kubeconfig %s: %v", e2eKubeconfig, err)
	}
}

func TestPhase14LiveClusterIsPrivate(t *testing.T) {
	ensureCluster(t)
	if e2eWorkspace != defaultWorkspace && os.Getenv(envKubeconfig) == "" {
		t.Fatalf("workspace = %q, want %s", e2eWorkspace, defaultWorkspace)
	}
	if os.Getenv(graphns.Env) == "" {
		t.Fatal("the run has no graph namespace, so parallel runs would collide in the shared graph")
	}
	if e2ePrivate && !kcpproc.Ready(e2eInstance) {
		t.Fatalf("the private kcp at %s is not ready", e2eInstance.KcpURL)
	}
}

func TestPhase12LiveLockMarker(t *testing.T) {
	if os.Getenv("SPECD_LIVE_LOCK_MARK") == "" {
		t.Skip("not a lock proof run")
	}
	time.Sleep(1200 * time.Millisecond)
}

func contextDocPath(repository, context string) string {
	return filepath.Join(os.Getenv(statedir.EnvClmDocDir), repository, context+".md")
}

func assertNoSpecArtefacts(t *testing.T, repoPath string) {
	t.Helper()
	for _, artefact := range []string{".specs", "arch.yaml"} {
		if _, err := os.Stat(filepath.Join(repoPath, artefact)); err == nil {
			t.Errorf("%s appeared in the project tree %s", artefact, repoPath)
		}
	}
	out, err := exec.Command("git", "-C", repoPath, "log", "--all", "--name-only", "--format=", "--not", "--glob=refs/heads/open-architecture/*").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, ".specs/") || line == "arch.yaml" {
			t.Errorf("a code commit carries the spec artefact %s", line)
		}
	}
}
