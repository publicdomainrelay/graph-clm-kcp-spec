package runlock_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTwoLiveSuitesSerialise proves the lock the way the failure it prevents
// would happen: two `go test ./test/e2e` runs started at once. Each run takes
// the same lock in its TestMain and appends a begin and an end mark around the
// window it holds it. Serialized, the marks read begin, end, begin, end; two
// runs overlapping would read begin, begin, ... and the second run's objects
// would land on the first run's.
func TestTwoLiveSuitesSerialise(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "test", "e2e", "main_test.go")); err != nil {
		t.Skipf("the live suite is not in this tree: %v", err)
	}
	marks := filepath.Join(t.TempDir(), "marks")
	lockPath := filepath.Join(t.TempDir(), "live.lock")

	runs := make([]*exec.Cmd, 0, 2)
	outputs := make([]*bytes.Buffer, 0, 2)
	for range 2 {
		command := exec.Command("go", "test", "./test/e2e", "-run", "^TestPhase12LiveLockMarker$", "-count=1")
		command.Dir = root
		command.Env = append(os.Environ(),
			"SPECD_LIVE_LOCK="+lockPath,
			"SPECD_LIVE_LOCK_MARK="+marks,
		)
		output := &bytes.Buffer{}
		command.Stdout = output
		command.Stderr = output
		if err := command.Start(); err != nil {
			t.Fatalf("start a second live suite: %v", err)
		}
		runs = append(runs, command)
		outputs = append(outputs, output)
	}
	for index, command := range runs {
		if err := command.Wait(); err != nil {
			t.Fatalf("a live suite failed: %v\n%s", err, outputs[index])
		}
	}

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
	at   int64
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
		if len(fields) != 2 {
			continue
		}
		at, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, markEvent{name: fields[0], at: at})
	}
	return out
}
