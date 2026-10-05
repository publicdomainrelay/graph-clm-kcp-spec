package kcpproc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("KCPPROC_HELPER") == "1" {
		os.Exit(m.Run())
	}
	ledger, err := os.MkdirTemp("", "kcpproc-ledger-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "kcpproc: %v\n", err)
		os.Exit(1)
	}
	os.Setenv(LedgerEnv, filepath.Join(ledger, "ledger"))
	code := m.Run()
	leaks := Leaks()
	for _, leak := range leaks {
		fmt.Fprintf(os.Stderr, "kcpproc: leaked kcp %d, kine %d and specd %d on the root %s\n", leak.KcpPid, leak.KinePid, leak.SpecdPid, leak.Root)
		Terminate(leak.KcpPid, leak.Root)
		Terminate(leak.KinePid, leak.Root)
		Terminate(leak.SpecdPid, leak.Root)
	}
	if len(leaks) > 0 {
		code = 1
	}
	os.RemoveAll(ledger)
	os.Exit(code)
}

func requireKcp(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"kcp", "kine"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
				t.Fatalf("%s not on PATH", tool)
			}
			t.Skipf("%s not on PATH", tool)
		}
	}
	if testing.Short() {
		t.Skip("starts a kcp server")
	}
}

func TestFixedPortsAndEndpointRoundTrip(t *testing.T) {
	requireKcp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	root := t.TempDir()
	port := freePort(t)
	kinePort := freePort(t)
	instance, err := Start(ctx, Options{Root: root, KcpPort: port, KinePort: kinePort, DieWithStarter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(instance) })
	if instance.KcpPort != port {
		t.Fatalf("kcp port = %d, want the fixed %d", instance.KcpPort, port)
	}
	if instance.KinePort != kinePort {
		t.Fatalf("kine port = %d, want the fixed %d", instance.KinePort, kinePort)
	}
	if err := SaveEndpoint(instance); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEndpoint(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != instance {
		t.Fatalf("endpoint = %+v, want %+v", loaded, instance)
	}
	probed, ok := Probe(root)
	if !ok || probed.KcpPort != port || probed.KinePort != kinePort {
		t.Fatalf("Probe = %+v, %v", probed, ok)
	}
	if _, err := Start(ctx, Options{Root: root}); err == nil {
		t.Fatal("a second kcp started on a root that is already serving one")
	}
}

func TestProbeAdoptsTheRealPidsAndStopWaitsForThem(t *testing.T) {
	requireKcp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	root := t.TempDir()
	instance, err := Start(ctx, Options{Root: root, DieWithStarter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(instance) })

	probed, ok := Probe(root)
	if !ok {
		t.Fatalf("Probe did not adopt the running instance at %s", root)
	}
	if probed.KcpPid != instance.KcpPid || probed.KinePid != instance.KinePid {
		t.Fatalf("Probe adopted pids %d/%d, want the real %d/%d",
			probed.KcpPid, probed.KinePid, instance.KcpPid, instance.KinePid)
	}
	if !owns(probed.KcpPid, root) || !owns(probed.KinePid, root) {
		t.Fatalf("Probe adopted pids that do not name %s: %+v", root, probed)
	}
	if got := Running(root); len(got) != 2 {
		t.Fatalf("Running = %v, want the kcp and kine pids", got)
	}

	Stop(probed)
	if alive(instance.KcpPid) || alive(instance.KinePid) {
		t.Fatalf("Stop left a process alive: kcp %d alive=%v, kine %d alive=%v",
			instance.KcpPid, alive(instance.KcpPid), instance.KinePid, alive(instance.KinePid))
	}
	if got := Running(root); len(got) != 0 {
		t.Fatalf("Running after Stop = %v, want none", got)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestStartStopStartResumesOnOneRoot(t *testing.T) {
	requireKcp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	root := t.TempDir()
	first, err := Start(ctx, Options{Root: root, DieWithStarter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(first) })
	if err := SaveEndpoint(first); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(root, "kine.db")
	before, err := os.Stat(store)
	if err != nil || before.Size() == 0 {
		t.Fatalf("the store after the first start: %v, %v", before, err)
	}
	Stop(first)
	if live := Running(root); len(live) != 0 {
		t.Fatalf("Stop left %v alive", live)
	}

	second, err := Start(ctx, Options{Root: root, DieWithStarter: true})
	if err != nil {
		t.Fatalf("a restart on a root this process used: %v", err)
	}
	t.Cleanup(func() { Stop(second) })
	if !Ready(second) {
		t.Fatalf("not ready after the restart: %+v", second)
	}
	if second.KcpPort != first.KcpPort {
		t.Fatalf("the restart moved the kcp port: %+v, was %+v", second, first)
	}
	after, err := os.Stat(store)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("the restart replaced the store: %v, %v", after, err)
	}
	if after.Size() < before.Size() {
		t.Fatalf("the store shrank across the restart: %d, was %d", after.Size(), before.Size())
	}
	if err := SaveEndpoint(second); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(root, "kcp.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "empty token for the shard-admin user") {
		t.Fatalf("the restart could not reuse the shard-admin token:\n%s", log)
	}
}

func TestTwoInstancesRunSideBySideOnKernelPorts(t *testing.T) {
	for _, tool := range []string{"kcp", "kine"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
				t.Fatalf("%s not on PATH", tool)
			}
			t.Skipf("%s not on PATH", tool)
		}
	}
	if testing.Short() {
		t.Skip("starts two kcp servers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	roots := []string{t.TempDir(), t.TempDir()}
	instances := make([]Instance, len(roots))
	errs := make([]error, len(roots))
	var wait sync.WaitGroup
	for index, root := range roots {
		wait.Add(1)
		go func() {
			defer wait.Done()
			instances[index], errs[index] = Start(ctx, Options{Root: root, DieWithStarter: true})
		}()
	}
	wait.Wait()
	t.Cleanup(func() {
		for _, instance := range instances {
			Stop(instance)
		}
	})
	for index, err := range errs {
		if err != nil {
			t.Fatalf("instance %d: %v", index, err)
		}
	}
	first, second := instances[0], instances[1]
	if first.KcpPort == second.KcpPort || first.KinePort == second.KinePort {
		t.Fatalf("the instances share a port: %+v %+v", first, second)
	}
	for _, instance := range instances {
		if !Ready(instance) {
			t.Fatalf("not ready: %+v", instance)
		}
		if port, ok := kubeconfigPort(instance.AdminKubeconfig); !ok || port != instance.KcpPort {
			t.Fatalf("the kubeconfig names %d, the instance %d", port, instance.KcpPort)
		}
	}
	Stop(first)
	if Ready(first) || alive(first.KcpPid) || alive(first.KinePid) {
		t.Fatal("the first instance survived Stop")
	}
	if !Ready(second) {
		t.Fatal("stopping one instance touched the other")
	}
}

func TestKcpStarterHelper(t *testing.T) {
	if os.Getenv("KCPPROC_HELPER") != "1" {
		t.Skip("not the starter helper")
	}
	instance, err := Start(context.Background(), Options{
		Root:           os.Getenv("KCPPROC_HELPER_ROOT"),
		DieWithStarter: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "kcpproc helper: %v\n", err)
		os.Exit(1)
	}
	data, err := json.Marshal(instance)
	if err != nil {
		os.Exit(1)
	}
	fmt.Printf("READY %s\n", data)
	select {}
}

func TestKcpDiesWithItsStarter(t *testing.T) {
	requireKcp(t)
	root := t.TempDir()
	helper := exec.Command(os.Args[0], "-test.run=^TestKcpStarterHelper$", "-test.timeout=0")
	helper.Env = append(os.Environ(), "KCPPROC_HELPER=1", "KCPPROC_HELPER_ROOT="+root)
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

	instance := readHelperInstance(t, stdout)
	if instance.KcpPid <= 0 || instance.KinePid <= 0 {
		t.Fatalf("the helper reported no pids: %+v", instance)
	}
	if !alive(instance.KcpPid) || !alive(instance.KinePid) {
		t.Fatalf("the helper's kcp and kine are not running: %+v", instance)
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(instance.KcpPid) && !alive(instance.KinePid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("kcp %d (alive %v) and kine %d (alive %v) outlived the process that started them",
		instance.KcpPid, alive(instance.KcpPid), instance.KinePid, alive(instance.KinePid))
}

func readHelperInstance(t *testing.T, stdout io.Reader) Instance {
	t.Helper()
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			lines <- "ERROR " + err.Error()
		}
	}()
	deadline := time.After(3 * time.Minute)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the starter helper exited before it reported its kcp")
			}
			if payload, found := strings.CutPrefix(line, "READY "); found {
				instance := Instance{}
				if err := json.Unmarshal([]byte(payload), &instance); err != nil {
					t.Fatalf("the starter helper's report: %v", err)
				}
				return instance
			}
		case <-deadline:
			t.Fatal("the starter helper did not report its kcp within 3 minutes")
		}
	}
}

func TestDieWithPidReadsTheEnv(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 0},
		{"0", 0},
		{"1", 0},
		{" 4242 ", 4242},
		{"not-a-pid", 0},
		{"-3", 0},
	} {
		t.Setenv(DieWithEnv, test.value)
		if got := DieWithPid(); got != test.want {
			t.Errorf("DieWithPid() with %q = %d, want %d", test.value, got, test.want)
		}
	}
}

func TestRecordSpecdAppendsToTheLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger")
	t.Setenv(LedgerEnv, path)
	root := filepath.Join(t.TempDir(), "kcp")
	RecordSpecd(root, 4242)
	RecordSpecd(root, 0)
	entries := Ledger()
	if len(entries) != 1 {
		t.Fatalf("Ledger() = %+v, want the one specd entry", entries)
	}
	if entries[0].Root != root || entries[0].SpecdPid != 4242 || entries[0].KcpPid != 0 || entries[0].KinePid != 0 {
		t.Fatalf("the ledger entry = %+v, want the root and the specd pid alone", entries[0])
	}
}
