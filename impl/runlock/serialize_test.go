package runlock_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
)

const markerTest = "^TestPhase12LiveLockMarker$"

func liveTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"go", "kcp", "kine", "kubectl", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "test", "e2e", "main_test.go")); err != nil {
		t.Skipf("the live suite is not in this tree: %v", err)
	}
	return root
}

func startSuite(t *testing.T, root, marks string, env ...string) *exec.Cmd {
	t.Helper()
	command := exec.Command("go", "test", "./test/e2e", "-run", markerTest, "-count=1")
	command.Dir = root
	command.Env = append(append(os.Environ(), "SPECD_LIVE_LOCK_MARK="+marks), env...)
	return command
}

func runPair(t *testing.T, root, marks string, env ...string) {
	t.Helper()
	runs := make([]*exec.Cmd, 0, 2)
	outputs := make([]*bytes.Buffer, 0, 2)
	for range 2 {
		command := startSuite(t, root, marks, env...)
		output := &bytes.Buffer{}
		command.Stdout = output
		command.Stderr = output
		if err := command.Start(); err != nil {
			t.Fatalf("start a live suite: %v", err)
		}
		runs = append(runs, command)
		outputs = append(outputs, output)
	}
	for index, command := range runs {
		if err := command.Wait(); err != nil {
			t.Fatalf("a live suite failed: %v\n%s", err, outputs[index])
		}
	}
}

func TestTwoLiveSuitesRunInParallel(t *testing.T) {
	liveTools(t)
	root := repoRoot(t)
	marks := filepath.Join(t.TempDir(), "marks")

	runPair(t, root, marks)

	events := readMarks(t, marks)
	if len(events) != 4 {
		t.Fatalf("marks = %v, want a begin and an end per run", events)
	}
	windows := windowsOf(events)
	if !overlaps(windows[0], windows[1]) {
		t.Fatalf("two suites with their own kcp did not overlap, so they serialised: %v", events)
	}
}

func TestTwoRunsNamingOneKcpSerialise(t *testing.T) {
	liveTools(t)
	root := repoRoot(t)
	marks := filepath.Join(t.TempDir(), "marks")
	stateRoot := t.TempDir()

	instance, err := kcpproc.Start(context.Background(), kcpproc.Options{Root: stateRoot})
	if err != nil {
		t.Skipf("cannot start a kcp for the shared-kcp case: %v", err)
	}
	t.Cleanup(func() { kcpproc.Stop(instance) })

	runPair(t, root, marks, "SPECD_E2E_KUBECONFIG="+instance.AdminKubeconfig, "SPECD_E2E_STATE_ROOT="+stateRoot)

	events := readMarks(t, marks)
	if len(events) != 4 {
		t.Fatalf("marks = %v, want a begin and an end per run", events)
	}
	for index, event := range events {
		want := "begin"
		if index%2 == 1 {
			want = "end"
		}
		if event.name != want {
			t.Fatalf("the lock did not serialise the runs: marks = %v", events)
		}
	}
	if events[1].at > events[2].at {
		t.Errorf("the first run's window ends after the second begins: %v", events)
	}
}

type markEvent struct {
	name string
	pid  int
	at   int64
}

type window struct {
	begin int64
	end   int64
}

func windowsOf(events []markEvent) []window {
	begins, ends := map[int]int64{}, map[int]int64{}
	for _, event := range events {
		if event.name == "begin" {
			begins[event.pid] = event.at
		} else {
			ends[event.pid] = event.at
		}
	}
	windows := []window{}
	for pid, begin := range begins {
		windows = append(windows, window{begin: begin, end: ends[pid]})
	}
	return windows
}

func overlaps(left, right window) bool {
	return left.begin < right.end && right.begin < left.end
}

func readMarks(t *testing.T, path string) []markEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the marks: %v", err)
	}
	out := []markEvent{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		at, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, markEvent{name: fields[0], pid: pid, at: at})
	}
	return out
}
