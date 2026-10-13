// Package graphproc starts the graph database of one instance: a HydraDB
// graph-node whose bolt, http and admin listeners bind port 0. The node logs
// the addresses the kernel gave it; Start reads them back and records them in
// <root>/graph.json, the same way kcpproc records kcp and kine.
package graphproc

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	Backend = "hydradb"

	User = "neo4j"

	NodeBinEnv = "SPECD_GRAPH_NODE"

	DefaultReadyTimeout = 60 * time.Second

	DefaultMaxLogBytes int64 = 16 << 20

	listenersStarted = "graph node listeners started"

	rootMarker = "--specd-graph-root="
)

type Options struct {
	Root string

	NodeBin string

	ReadyTimeout time.Duration

	DieWithStarter bool

	MaxLogBytes int64
}

type Instance struct {
	Root string `json:"root"`

	Backend string `json:"backend"`

	BoltURL string `json:"boltURL"`

	BoltAddr string `json:"boltAddr"`

	HTTPAddr string `json:"httpAddr,omitempty"`

	AdminAddr string `json:"adminAddr,omitempty"`

	User string `json:"user"`

	TokenFile string `json:"tokenFile"`

	Pid int `json:"pid"`

	Log string `json:"log"`
}

func EndpointPath(root string) string {
	return filepath.Join(root, "graph.json")
}

func TokenPath(root string) string {
	return filepath.Join(root, "token")
}

// NodeBin finds the graph-node binary: SPECD_GRAPH_NODE, then PATH, then the
// bin/ folder beside the running executable, where make build and
// scripts/container-extract-and-install.sh put it.
func NodeBin() (string, error) {
	if path := os.Getenv(NodeBinEnv); path != "" {
		return path, nil
	}
	if path, err := exec.LookPath("graph-node"); err == nil {
		return path, nil
	}
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "graph-node")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("graphproc: no graph-node binary: set %s, put graph-node on PATH, or install it with scripts/container-extract-and-install.sh", NodeBinEnv)
}

func (o Options) withDefaults() (Options, error) {
	if o.Root == "" {
		return o, errors.New("graphproc: no root")
	}
	if o.NodeBin == "" {
		bin, err := NodeBin()
		if err != nil {
			return o, err
		}
		o.NodeBin = bin
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = DefaultReadyTimeout
	}
	if o.MaxLogBytes <= 0 {
		o.MaxLogBytes = DefaultMaxLogBytes
	}
	return o, nil
}

// Start brings the instance's graph up, or returns the one already serving the
// root. The token and the data survive a restart; the ports do not, and the
// endpoint file is rewritten with the new ones.
func Start(ctx context.Context, options Options) (Instance, error) {
	if serving, ok := Probe(options.Root); ok {
		return serving, nil
	}
	options, err := options.withDefaults()
	if err != nil {
		return Instance{}, err
	}
	for _, dir := range []string{"data", "cache", "slate"} {
		if err := os.MkdirAll(filepath.Join(options.Root, dir), 0o700); err != nil {
			return Instance{}, err
		}
	}
	tokenFile, err := ensureToken(options.Root)
	if err != nil {
		return Instance{}, err
	}
	instance := Instance{
		Root:      options.Root,
		Backend:   Backend,
		User:      User,
		TokenFile: tokenFile,
		Log:       filepath.Join(options.Root, "graph-node.log"),
	}
	env := append(os.Environ(),
		"GRAPH_ALLOW_PLAINTEXT=true",
		"GRAPH_AUTH_TOKEN_FILE="+tokenFile,
		"GRAPH_DATA_PATH="+filepath.Join(options.Root, "data"),
		"GRAPH_DATA_CACHE_DIR="+filepath.Join(options.Root, "cache"),
		"GRAPH_BOLT_ADDR=127.0.0.1:0",
		"GRAPH_HTTP_ADDR=127.0.0.1:0",
		"GRAPH_ADMIN_ADDR=127.0.0.1:0",
		"GRAPH_LOG_FORMAT=json",
		"CLOUD_PROVIDER=local",
		"LOCAL_PATH="+filepath.Join(options.Root, "slate"),
	)
	if lib := filepath.Join(filepath.Dir(options.NodeBin), "lib"); dirExists(lib) {
		env = append(env, "LD_LIBRARY_PATH="+lib+prefixed(os.Getenv("LD_LIBRARY_PATH")))
	}
	pid, err := spawn(options, instance.Log, env)
	if err != nil {
		return instance, fmt.Errorf("graphproc: start graph-node: %w", err)
	}
	instance.Pid = pid
	addresses, err := waitForListeners(ctx, instance.Log, pid, options.ReadyTimeout)
	if err != nil {
		Stop(instance)
		return instance, err
	}
	instance.BoltAddr = addresses.Bolt
	instance.HTTPAddr = addresses.HTTP
	instance.AdminAddr = addresses.Admin
	instance.BoltURL = "bolt://" + addresses.Bolt
	if err := waitDial(ctx, addresses.Bolt, pid, options.ReadyTimeout); err != nil {
		Stop(instance)
		return instance, err
	}
	if err := SaveEndpoint(instance); err != nil {
		Stop(instance)
		return instance, err
	}
	return instance, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func prefixed(path string) string {
	if path == "" {
		return ""
	}
	return ":" + path
}

func ensureToken(root string) (string, error) {
	path := TokenPath(root)
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return path, nil
	}
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(secret)+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Password reads the token the node authenticates bolt clients with.
func (i Instance) Password() (string, error) {
	data, err := os.ReadFile(i.TokenFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func SaveEndpoint(instance Instance) error {
	data, err := json.MarshalIndent(instance, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(EndpointPath(instance.Root), append(data, '\n'), 0o600)
}

func LoadEndpoint(root string) (Instance, error) {
	data, err := os.ReadFile(EndpointPath(root))
	if err != nil {
		return Instance{}, err
	}
	instance := Instance{}
	if err := json.Unmarshal(data, &instance); err != nil {
		return Instance{}, fmt.Errorf("graphproc: read %s: %w", EndpointPath(root), err)
	}
	return instance, nil
}

// Probe returns the graph serving the root, when its process is alive and its
// bolt listener answers.
func Probe(root string) (Instance, bool) {
	if root == "" {
		return Instance{}, false
	}
	instance, err := LoadEndpoint(root)
	if err != nil {
		return Instance{}, false
	}
	if !Owns(instance.Pid, root) || instance.BoltAddr == "" {
		return instance, false
	}
	connection, err := net.DialTimeout("tcp", instance.BoltAddr, time.Second)
	if err != nil {
		return instance, false
	}
	connection.Close()
	return instance, true
}

func Ready(instance Instance) bool {
	_, ok := Probe(instance.Root)
	return ok
}

func Stop(instance Instance) {
	pid := instance.Pid
	if !Owns(pid, instance.Root) {
		pid = Scan(instance.Root)
	}
	if !Owns(pid, instance.Root) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if waitGone(pid, 5*time.Second) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	waitGone(pid, 5*time.Second)
}

// Owns reports whether pid is the graph-node of root. The node takes its
// configuration from the environment, so the start adds a marker argument it
// ignores, and the marker names the root.
func Owns(pid int, root string) bool {
	if pid <= 0 || root == "" || !alive(pid) {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	for _, field := range strings.Split(string(cmdline), "\x00") {
		if field == rootMarker+root {
			return true
		}
	}
	return false
}

// Scan finds the graph-node of root among the running processes.
func Scan(root string) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		var pid int
		if _, err := fmt.Sscanf(entry.Name(), "%d", &pid); err != nil {
			continue
		}
		if Owns(pid, root) {
			return pid
		}
	}
	return 0
}

// Under finds every graph-node whose root is base or lies below it: the graph
// of a kcp instance lives in that instance's root, so stopping the instance
// stops its graph.
func Under(base string) []int {
	cleaned := filepath.Clean(base)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	pids := []int{}
	for _, entry := range entries {
		var pid int
		if _, err := fmt.Sscanf(entry.Name(), "%d", &pid); err != nil || !alive(pid) {
			continue
		}
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		for _, field := range strings.Split(string(cmdline), "\x00") {
			root, ok := strings.CutPrefix(field, rootMarker)
			if ok && (root == cleaned || strings.HasPrefix(root, cleaned+string(filepath.Separator))) {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

// Terminate stops the graph-node pid when it is one under base.
func Terminate(pid int, base string) {
	for _, candidate := range Under(base) {
		if candidate != pid {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		if !waitGone(pid, 5*time.Second) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			waitGone(pid, 5*time.Second)
		}
		return
	}
}

type Addresses struct {
	Bolt string

	HTTP string

	Admin string
}

// ParseListeners reads the addresses out of one log line of graph-node, in its
// json or its text format.
func ParseListeners(line string) (Addresses, bool) {
	if !strings.Contains(line, listenersStarted) {
		return Addresses{}, false
	}
	record := map[string]any{}
	if json.Unmarshal([]byte(line), &record) == nil {
		fields := record
		if nested, ok := record["fields"].(map[string]any); ok {
			fields = nested
		}
		out := Addresses{Bolt: stringOf(fields["bolt_addr"]), HTTP: stringOf(fields["http_addr"]), Admin: stringOf(fields["admin_addr"])}
		return out, out.Bolt != ""
	}
	out := Addresses{}
	for _, word := range strings.Fields(stripANSI(line)) {
		key, value, ok := strings.Cut(word, "=")
		if !ok {
			continue
		}
		switch key {
		case "bolt_addr":
			out.Bolt = value
		case "http_addr":
			out.HTTP = value
		case "admin_addr":
			out.Admin = value
		}
	}
	return out, out.Bolt != ""
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

func stripANSI(text string) string {
	var builder strings.Builder
	escape := false
	for _, character := range text {
		switch {
		case character == 0x1b:
			escape = true
		case escape && character == 'm':
			escape = false
		case !escape:
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func waitForListeners(ctx context.Context, logPath string, pid int, timeout time.Duration) (Addresses, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if file, err := os.Open(logPath); err == nil {
			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
			for scanner.Scan() {
				if addresses, ok := ParseListeners(scanner.Text()); ok {
					file.Close()
					return addresses, nil
				}
			}
			file.Close()
		}
		if !alive(pid) {
			return Addresses{}, fmt.Errorf("graphproc: graph-node exited; see %s: %s", logPath, lastLine(logPath))
		}
		select {
		case <-ctx.Done():
			return Addresses{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return Addresses{}, fmt.Errorf("graphproc: graph-node did not report its listeners within %s; see %s", timeout, logPath)
}

func waitDial(ctx context.Context, address string, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			connection.Close()
			return nil
		}
		if !alive(pid) {
			return fmt.Errorf("graphproc: graph-node exited before %s answered", address)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("graphproc: bolt %s did not answer within %s", address, timeout)
}

func spawn(options Options, logPath string, env []string) (int, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	command := exec.Command(options.NodeBin, rootMarker+options.Root)
	command.Dir = options.Root
	command.Env = env
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
	ticker := time.NewTicker(30 * time.Second)
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
	file, err := os.Open(path)
	if err != nil {
		return
	}
	if _, err := file.Seek(-(max / 2), io.SeekEnd); err != nil {
		file.Close()
		return
	}
	tail, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		return
	}
	_ = os.WriteFile(path, tail, 0o600)
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

func waitGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !alive(pid)
}

func lastLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return lines[len(lines)-1]
}
