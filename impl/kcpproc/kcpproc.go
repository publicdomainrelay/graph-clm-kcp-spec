package kcpproc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultFeatureGates = "WorkspaceMounts=true"

	kcpAttempts = 5
)

var kineAvailable = regexp.MustCompile(`Kine available at (http://127\.0\.0\.1:(\d+))`)

type Options struct {
	Root string

	KcpBin string

	KineBin string

	FeatureGates string

	ReadyTimeout time.Duration
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
	return o
}

func Start(ctx context.Context, options Options) (Instance, error) {
	options = options.withDefaults()
	if err := os.MkdirAll(options.Root, 0o755); err != nil {
		return Instance{}, err
	}
	instance := Instance{Root: options.Root, AdminKubeconfig: filepath.Join(options.Root, "admin.kubeconfig")}

	kineLog := filepath.Join(options.Root, "kine.log")
	kinePid, err := spawn(options.KineBin, kineLog,
		"--endpoint", "sqlite://"+filepath.Join(options.Root, "kine.db"),
		"--listen-address", "127.0.0.1:0",
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
		port, err := kernelPort()
		if err != nil {
			Stop(instance)
			return instance, err
		}
		_ = os.Remove(instance.AdminKubeconfig)
		kcpLog := filepath.Join(options.Root, "kcp.log")
		kcpPid, err := spawn(options.KcpBin, kcpLog,
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

func Ready(instance Instance) bool {
	if instance.KcpPort == 0 || !owns(instance.KcpPid, instance.Root) || !owns(instance.KinePid, instance.Root) {
		return false
	}
	return readyz(instance.KcpPort)
}

func Stop(instance Instance) {
	terminate(instance.KcpPid, instance.Root)
	terminate(instance.KinePid, instance.Root)
}

var errPortTaken = errors.New("kcpproc: the kcp port was taken before kcp bound it")

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

func spawn(binary, logPath string, args ...string) (int, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	command := exec.Command(binary, args...)
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return 0, err
	}
	pid := command.Process.Pid
	go func() { _ = command.Wait() }()
	return pid, nil
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
	for range 50 {
		if !alive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
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
