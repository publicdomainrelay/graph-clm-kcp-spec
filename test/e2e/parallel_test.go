package e2e

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/graphns"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Every live test runs in parallel with the others, each in a workspace of
// its own on the one private kcp, so no two tests ever see each other's
// objects or controllers. The graph-node is shared, and each test writes in a
// graph namespace of its own.

const envParallel = "SPECD_E2E_PARALLEL"

const defaultParallel = 12

var (
	workspaces sync.Map

	workspaceCounter atomic.Int64
)

type testWorkspace struct {
	once sync.Once

	name string

	kubeconfig string

	err error
}

// setParallel caps how many tests run at once: SPECD_E2E_PARALLEL, else 12,
// unless -parallel was given.
func setParallel() {
	if !flag.Parsed() {
		flag.Parse()
	}
	given := false
	flag.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "test.parallel" {
			given = true
		}
	})
	if given {
		return
	}
	value := defaultParallel
	if parsed, err := strconv.Atoi(os.Getenv(envParallel)); err == nil && parsed > 0 {
		value = parsed
	}
	_ = flag.Set("test.parallel", strconv.Itoa(value))
}

func topLevel(t *testing.T) string {
	name, _, _ := strings.Cut(t.Name(), "/")
	return name
}

// workspaceFor gives the test (and its subtests) a fresh workspace with the
// spec CRDs installed, made on first use and deleted when the test ends.
func workspaceFor(t *testing.T) *testWorkspace {
	t.Helper()
	ensureCluster(t)
	value, loaded := workspaces.LoadOrStore(topLevel(t), &testWorkspace{})
	entry := value.(*testWorkspace)
	entry.once.Do(func() {
		name := fmt.Sprintf("e2e-%d", workspaceCounter.Add(1))
		entry.name = "root:" + name
		entry.kubeconfig, entry.err = installSpecs(e2eStateRoot, e2eKubeconfig, entry.name)
		if entry.err == nil && !loaded {
			t.Cleanup(func() { dropTestWorkspace(name) })
		}
	})
	if entry.err != nil {
		t.Fatalf("make the test's workspace: %v", entry.err)
	}
	return entry
}

func clusterWorkspace(t *testing.T) string {
	t.Helper()
	return workspaceFor(t).name
}

func workspaceKubeconfig(t *testing.T) string {
	t.Helper()
	return workspaceFor(t).kubeconfig
}

func dropTestWorkspace(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", e2eKubeconfig, "delete", "workspace", name, "--wait=false")
	_ = command.Run()
	_ = os.Remove(filepath.Join(e2eStateRoot, name+".kubeconfig"))
}

// graphNamespaceFor scopes a test's graph writes to a namespace of its own on
// the shared graph-node, so a rebuild in one test never clears another's.
func graphNamespaceFor(t *testing.T) string {
	t.Helper()
	return graphns.FromEnv() + "-" + strings.ToLower(topLevel(t))
}

// testEnviron is the environment of a command a test starts: the test's
// workspace and graph namespace replace the run's.
func testEnviron(t *testing.T, overrides map[string]string) []string {
	t.Helper()
	merged := map[string]string{
		"SPECD_WORKSPACE": clusterWorkspace(t),
		graphns.Env:       graphNamespaceFor(t),
	}
	for name, value := range overrides {
		merged[name] = value
	}
	return specdEnv(merged)
}

var (
	binariesOnce sync.Once

	binariesDir string

	binariesErr error
)

// sharedBinaries builds specctl and specd once per run; every test that
// shells out to them uses the same two files.
func sharedBinaries(t *testing.T) (string, string) {
	t.Helper()
	binariesOnce.Do(func() {
		dir, err := os.MkdirTemp("", "specd-e2e-bin-")
		if err != nil {
			binariesErr = err
			return
		}
		binariesDir = dir
		root, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			binariesErr = err
			return
		}
		for _, name := range []string{"specctl", "specd"} {
			build := exec.Command("go", "build", "-o", filepath.Join(dir, name), "./cmd/"+name)
			build.Dir = root
			if output, err := build.CombinedOutput(); err != nil {
				binariesErr = fmt.Errorf("build %s: %w\n%s", name, err, output)
				return
			}
		}
	})
	if binariesErr != nil {
		t.Fatal(binariesErr)
	}
	return filepath.Join(binariesDir, "specctl"), filepath.Join(binariesDir, "specd")
}

func removeSharedBinaries() {
	if binariesDir != "" {
		os.RemoveAll(binariesDir)
	}
}

// testLog is a controller logger that keeps everything and prints it only
// when the test fails, so a parallel run stays quiet and a failure explains
// itself.
func testLog(t *testing.T) *slog.Logger {
	t.Helper()
	buffer := &lockedBuffer{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("controller log:\n%s", buffer.String())
		}
	})
	return slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type lockedBuffer struct {
	mu sync.Mutex

	data bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}
