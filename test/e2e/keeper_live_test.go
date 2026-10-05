package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
)

const (
	helperEnv = "SPECD_E2E_HELPER"

	helperWorkEnv = "SPECD_E2E_HELPER_WORK"

	helperSpecctlEnv = "SPECD_E2E_HELPER_SPECCTL"

	helperSpecdEnv = "SPECD_E2E_HELPER_SPECD"
)

func TestSpecctlUpHelper(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("not the specctl-up helper")
	}
	work := os.Getenv(helperWorkEnv)
	repo := filepath.Join(work, "calc")
	cloneCalcFixture(t, repo)
	out := filepath.Join(work, "session.json")
	command := exec.Command(os.Getenv(helperSpecctlEnv), "up",
		"--summarize=false", "--remote", "", "--specd", os.Getenv(helperSpecdEnv), "--clm-mod", "", "--out", out)
	command.Dir = repo
	command.Env = append(os.Environ(),
		"SPECD_STATE_DIR="+filepath.Join(work, "state"),
		"SPECD_KUBECONFIG=",
		"SPECD_CLM_DOC_DIR=",
		kcpproc.DieWithEnv+"="+fmt.Sprint(os.Getpid()),
	)
	if output, err := command.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "helper: specctl up: %v\n%s\n", err, output)
		os.Exit(1)
	}
	record := branchRecord{}
	if err := json.Unmarshal(mustRead(t, out), &record); err != nil {
		fmt.Fprintf(os.Stderr, "helper: %v\n", err)
		os.Exit(1)
	}
	payload, err := json.Marshal(record)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("READY %s\n", payload)
	select {}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSpecctlUpDiesWithItsStarter(t *testing.T) {
	requireLive(t, "git", "kcp", "kine")
	specctl, specd := buildSpecctlAndSpecd(t)
	work := t.TempDir()
	helper := exec.Command(os.Args[0], "-test.run=^TestSpecctlUpHelper$", "-test.timeout=0")
	helper.Env = append(os.Environ(),
		helperEnv+"=1",
		helperWorkEnv+"="+work,
		helperSpecctlEnv+"="+specctl,
		helperSpecdEnv+"="+specd,
	)
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	helper.Stderr = os.Stderr
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = helper.Process.Kill()
		_ = helper.Wait()
	})

	record := readHelperRecord(t, stdout)
	if record.KcpPid <= 0 || record.KinePid <= 0 || record.SpecdPid <= 0 {
		t.Fatalf("the helper started an incomplete instance: %+v", record)
	}
	t.Cleanup(func() {
		kcpproc.Terminate(record.KcpPid, record.KcpRoot)
		kcpproc.Terminate(record.KinePid, record.KcpRoot)
		kcpproc.Terminate(record.SpecdPid, record.KcpRoot)
	})
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(record.SpecdPid) && !processAlive(record.KcpPid) && !processAlive(record.KinePid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("specctl up's kcp %d, kine %d and specd %d (alive %v/%v/%v) outlived the process that started them",
		record.KcpPid, record.KinePid, record.SpecdPid,
		processAlive(record.KcpPid), processAlive(record.KinePid), processAlive(record.SpecdPid))
}

func readHelperRecord(t *testing.T, stdout io.Reader) branchRecord {
	t.Helper()
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			lines <- "ERROR " + err.Error()
		}
	}()
	deadline := time.After(5 * time.Minute)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the helper exited before it reported its session")
			}
			if payload, found := strings.CutPrefix(line, "READY "); found {
				record := branchRecord{}
				if err := json.Unmarshal([]byte(payload), &record); err != nil {
					t.Fatalf("the helper's report: %v", err)
				}
				return record
			}
		case <-deadline:
			t.Fatal("the helper did not report its session within 5 minutes")
		}
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) > 2 && fields[2] != "Z"
}
