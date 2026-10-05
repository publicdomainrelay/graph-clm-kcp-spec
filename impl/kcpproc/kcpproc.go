package kcpproc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultFeatureGates = "WorkspaceMounts=true"

	DefaultMaxLogBytes int64 = 16 << 20

	LedgerEnv = "SPECD_KCP_LEDGER"

	kcpAttempts = 5

	logTrimEvery = time.Second
)

var kineAvailable = regexp.MustCompile(`Kine available at (http://127\.0\.0\.1:(\d+))`)

type Options struct {
	Root string

	KcpBin string

	KineBin string

	FeatureGates string

	ReadyTimeout time.Duration

	KcpPort int

	KinePort int

	DieWithStarter bool

	MaxLogBytes int64
}

type LedgerEntry struct {
	Root string `json:"root"`

	KcpPid int `json:"kcpPid"`

	KinePid int `json:"kinePid"`
}

type Instance struct {
	Root string `json:"root"`

	KcpPort int `json:"kcpPort"`

	KcpPid int `json:"kcpPid"`

	KcpURL string `json:"kcpURL"`

	KinePort int `json:"kinePort"`

	KinePid int `json:"kinePid"`

	KineURL string `json:"kineURL"`

	AdminKubeconfig string `json:"adminKubeconfig"`
}

const EndpointFile = "endpoint.json"

func EndpointPath(root string) string {
	return filepath.Join(root, EndpointFile)
}

func SaveEndpoint(instance Instance) error {
	return SaveEndpointTo(EndpointPath(instance.Root), instance)
}

func SaveEndpointTo(path string, instance Instance) error {
	data, err := json.MarshalIndent(instance, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func LoadEndpoint(root string) (Instance, error) {
	data, err := os.ReadFile(EndpointPath(root))
	if err != nil {
		return Instance{}, err
	}
	instance := Instance{}
	if err := json.Unmarshal(data, &instance); err != nil {
		return Instance{}, fmt.Errorf("kcpproc: read %s: %w", EndpointPath(root), err)
	}
	if instance.Root == "" {
		instance.Root = root
	}
	return instance, nil
}

func (o Options) withDefaults() Options {
	if o.KcpBin == "" {
		o.KcpBin = envOr("KCP_BIN", "kcp")
	}
	if o.KineBin == "" {
		o.KineBin = envOr("KINE_BIN", "kine")
	}
	if o.FeatureGates == "" {
		o.FeatureGates = DefaultFeatureGates
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = 120 * time.Second
	}
	if o.MaxLogBytes <= 0 {
		o.MaxLogBytes = DefaultMaxLogBytes
	}
	return o
}

func Start(ctx context.Context, options Options) (Instance, error) {
	options = options.withDefaults()
	if err := os.MkdirAll(options.Root, 0o755); err != nil {
		return Instance{}, err
	}
	if serving, ok := Probe(options.Root); ok {
		return serving, fmt.Errorf("kcpproc: a kcp is already serving %s at %s; reuse it or stop it before starting another", options.Root, serving.KcpURL)
	}
	instance := Instance{Root: options.Root, AdminKubeconfig: filepath.Join(options.Root, "admin.kubeconfig")}
	resuming := hasKineStore(options.Root)
	if options.KcpPort == 0 && resuming {
		if existing, ok := kubeconfigPort(instance.AdminKubeconfig); ok {
			options.KcpPort = existing
		}
	}

	kineLog := filepath.Join(options.Root, "kine.log")
	kinePid, err := spawn(options.KineBin, kineLog, options,
		"--endpoint", "sqlite://"+filepath.Join(options.Root, "kine.db"),
		"--listen-address", "127.0.0.1:"+strconv.Itoa(options.KinePort),
		"--metrics-bind-address=0")
	if err != nil {
		return instance, fmt.Errorf("kcpproc: start kine: %w", err)
	}
	instance.KinePid = kinePid
	kineURL, kinePort, err := waitForKine(ctx, kineLog, kinePid, options.ReadyTimeout)
	if err != nil {
		Stop(instance)
		return instance, err
	}
	instance.KineURL, instance.KinePort = kineURL, kinePort

	var lastErr error
	for attempt := 0; attempt < kcpAttempts; attempt++ {
		port, err := requestedPort(options.KcpPort)
		if err != nil {
			Stop(instance)
			return instance, err
		}
		if !resuming {
			_ = os.Remove(instance.AdminKubeconfig)
		}
		kcpLog := filepath.Join(options.Root, "kcp.log")
		kcpPid, err := spawn(options.KcpBin, kcpLog, options,
			"start",
			"--root-directory="+options.Root,
			"--etcd-servers="+kineURL,
			"--bind-address=127.0.0.1",
			"--secure-port="+strconv.Itoa(port),
			"--feature-gates="+options.FeatureGates)
		if err != nil {
			Stop(instance)
			return instance, fmt.Errorf("kcpproc: start kcp: %w", err)
		}
		instance.KcpPid = kcpPid
		served, err := waitForKcp(ctx, instance.AdminKubeconfig, kcpLog, kcpPid, options.ReadyTimeout)
		if err == nil {
			instance.KcpPort = served
			instance.KcpURL = "https://127.0.0.1:" + strconv.Itoa(served)
			recordStart(instance)
			return instance, nil
		}
		lastErr = err
		terminate(kcpPid, options.Root)
		instance.KcpPid = 0
		if !errors.Is(err, errPortTaken) {
			break
		}
	}
	Stop(instance)
	return instance, lastErr
}

func Probe(root string) (Instance, bool) {
	instance, err := LoadEndpoint(root)
	if err != nil {
		instance = Instance{Root: root, AdminKubeconfig: filepath.Join(root, "admin.kubeconfig")}
	}
	instance = fillPids(root, instance)
	if Ready(instance) {
		return refreshKine(instance), true
	}
	port, ok := kubeconfigPort(instance.AdminKubeconfig)
	if !ok || !readyz(port) {
		return instance, false
	}
	instance.KcpPort = port
	instance.KcpURL = "https://127.0.0.1:" + strconv.Itoa(port)
	instance = fillPids(root, instance)
	return refreshKine(instance), true
}

func fillPids(root string, instance Instance) Instance {
	if owns(instance.KcpPid, root) && owns(instance.KinePid, root) {
		return instance
	}
	scannedKcp, scannedKine := ScanPids(root)
	if !owns(instance.KcpPid, root) {
		instance.KcpPid = scannedKcp
	}
	if !owns(instance.KinePid, root) {
		instance.KinePid = scannedKine
	}
	return instance
}

func ScanPids(root string) (int, int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, 0
	}
	cleaned := filepath.Clean(root)
	self := os.Getpid()
	kcpPid, kinePid := 0, 0
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self || !alive(pid) {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		fields := strings.Fields(strings.ReplaceAll(string(data), "\x00", " "))
		if len(fields) == 0 {
			continue
		}
		switch filepath.Base(fields[0]) {
		case "kcp":
			if value, found := flagValue(fields, "--root-directory"); found && filepath.Clean(value) == cleaned {
				kcpPid = pid
			}
		case "kine":
			if value, found := flagValue(fields, "--endpoint"); found && filepath.Clean(strings.TrimPrefix(value, "sqlite://")) == filepath.Join(cleaned, "kine.db") {
				kinePid = pid
			}
		}
	}
	return kcpPid, kinePid
}

func Running(root string) []int {
	kcpPid, kinePid := ScanPids(root)
	pids := []int{}
	for _, pid := range []int{kcpPid, kinePid} {
		if pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func flagValue(fields []string, name string) (string, bool) {
	for index, field := range fields {
		if value, found := strings.CutPrefix(field, name+"="); found {
			return value, true
		}
		if field == name && index+1 < len(fields) {
			return fields[index+1], true
		}
	}
	return "", false
}

func refreshKine(instance Instance) Instance {
	if address := kineAddressFromProc(instance.KinePid); address != "" {
		instance.KineURL = "http://" + address
		instance.KinePort, _ = strconv.Atoi(address[strings.LastIndex(address, ":")+1:])
		return instance
	}
	if data, err := os.ReadFile(filepath.Join(instance.Root, "kine.log")); err == nil {
		if match := kineAvailable.FindStringSubmatch(string(data)); match != nil {
			instance.KineURL = match[1]
			instance.KinePort, _ = strconv.Atoi(match[2])
		}
	}
	return instance
}

func kineAddressFromProc(pid int) string {
	if pid <= 0 {
		return ""
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	fields := strings.Fields(strings.ReplaceAll(string(data), "\x00", " "))
	for index, field := range fields {
		if value, found := strings.CutPrefix(field, "--listen-address="); found {
			return value
		}
		if field == "--listen-address" && index+1 < len(fields) {
			return fields[index+1]
		}
	}
	return ""
}

func Ready(instance Instance) bool {
	if instance.KcpPort == 0 || !owns(instance.KcpPid, instance.Root) || !owns(instance.KinePid, instance.Root) {
		return false
	}
	return readyz(instance.KcpPort)
}

func Stop(instance Instance) {
	kcpPid, kinePid := instance.KcpPid, instance.KinePid
	if !owns(kcpPid, instance.Root) || !owns(kinePid, instance.Root) {
		scannedKcp, scannedKine := ScanPids(instance.Root)
		if !owns(kcpPid, instance.Root) {
			kcpPid = scannedKcp
		}
		if !owns(kinePid, instance.Root) {
			kinePid = scannedKine
		}
	}
	terminate(kcpPid, instance.Root)
	terminate(kinePid, instance.Root)
}

func Terminate(pid int, root string) {
	terminate(pid, root)
}

var errPortTaken = errors.New("kcpproc: the kcp port was taken before kcp bound it")

func requestedPort(fixed int) (int, error) {
	if fixed > 0 {
		return fixed, nil
	}
	return kernelPort()
}

func kernelPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("kcpproc: ask the kernel for a port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func spawn(binary, logPath string, options Options, args ...string) (int, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	command := exec.Command(binary, args...)
	command.Stdout = logFile
	command.Stderr = logFile
	attributes := &syscall.SysProcAttr{Setsid: true}
	if options.DieWithStarter {
		setPdeathsig(attributes)
	}
	command.SysProcAttr = attributes

	type outcome struct {
		pid int
		err error
	}
	started := make(chan outcome, 1)
	done := make(chan struct{})
	go func() {
		if options.DieWithStarter {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
		}
		if err := command.Start(); err != nil {
			started <- outcome{err: err}
			return
		}
		started <- outcome{pid: command.Process.Pid}
		_ = command.Wait()
		close(done)
	}()
	result := <-started
	if result.err != nil {
		return 0, result.err
	}
	go trimLog(logPath, options.MaxLogBytes, done)
	return result.pid, nil
}

func trimLog(path string, max int64, done <-chan struct{}) {
	ticker := time.NewTicker(logTrimEvery)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			trimLogOnce(path, max)
		}
	}
}

func trimLogOnce(path string, max int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= max {
		return
	}
	keep := max / 2
	file, err := os.Open(path)
	if err != nil {
		return
	}
	if _, err := file.Seek(-keep, io.SeekEnd); err != nil {
		file.Close()
		return
	}
	tail, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		return
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	defer out.Close()
	fmt.Fprintf(out, "kcpproc: trimmed the log to its last %d bytes at %s\n", len(tail), time.Now().UTC().Format(time.RFC3339))
	_, _ = out.Write(tail)
}

func recordStart(instance Instance) {
	path := os.Getenv(LedgerEnv)
	if path == "" {
		return
	}
	line, err := json.Marshal(LedgerEntry{Root: instance.Root, KcpPid: instance.KcpPid, KinePid: instance.KinePid})
	if err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(line, '\n'))
}

func Ledger() []LedgerEntry {
	path := os.Getenv(LedgerEnv)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	entries := []LedgerEntry{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := LedgerEntry{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func Leaks() []LedgerEntry {
	leaks := []LedgerEntry{}
	for _, entry := range Ledger() {
		if owns(entry.KcpPid, entry.Root) || owns(entry.KinePid, entry.Root) {
			leaks = append(leaks, entry)
		}
	}
	return leaks
}

func waitForKine(ctx context.Context, logPath string, pid int, timeout time.Duration) (string, int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(logPath); err == nil {
			if match := kineAvailable.FindStringSubmatch(string(data)); match != nil {
				port, _ := strconv.Atoi(match[2])
				return match[1], port, nil
			}
		}
		if !alive(pid) {
			return "", 0, fmt.Errorf("kcpproc: kine exited; see %s", logPath)
		}
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", 0, fmt.Errorf("kcpproc: kine did not report its address within %s; see %s", timeout, logPath)
}

func waitForKcp(ctx context.Context, kubeconfig, logPath string, pid int, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if port, ok := kubeconfigPort(kubeconfig); ok && readyz(port) {
			return port, nil
		}
		if !alive(pid) {
			data, _ := os.ReadFile(logPath)
			if strings.Contains(string(data), "address already in use") {
				return 0, errPortTaken
			}
			return 0, fmt.Errorf("kcpproc: kcp exited; see %s: %s", logPath, lastLine(string(data)))
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return 0, fmt.Errorf("kcpproc: kcp was not ready within %s; see %s", timeout, logPath)
}

func kubeconfigPort(path string) (int, bool) {
	config, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return 0, false
	}
	for _, cluster := range config.Clusters {
		parsed, err := url.Parse(cluster.Server)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(parsed.Port())
		if err == nil && port > 0 {
			return port, true
		}
	}
	return 0, false
}

func readyz(port int) bool {
	client := http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	response, err := client.Get("https://127.0.0.1:" + strconv.Itoa(port) + "/readyz")
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func hasKineStore(root string) bool {
	info, err := os.Stat(filepath.Join(root, "kine.db"))
	return err == nil && info.Size() > 0
}

func alive(pid int) bool {
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

func owns(pid int, root string) bool {
	if !alive(pid) {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return err == nil && strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), root)
}

func terminate(pid int, root string) {
	if !owns(pid, root) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if waitGone(pid) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	waitGone(pid)
}

func waitGone(pid int) bool {
	for range 50 {
		if !alive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !alive(pid)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
