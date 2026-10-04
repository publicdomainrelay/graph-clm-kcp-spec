package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
)

func runKcp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "specctl kcp: start, stop or endpoint is required")
		return exitUsage
	}
	switch args[0] {
	case "start":
		return runKcpStart(args[1:], stdout, stderr)
	case "stop":
		return runKcpStop(args[1:], stdout, stderr)
	case "endpoint":
		return runKcpEndpoint(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "specctl kcp: unknown subcommand %q\n", args[0])
	return exitUsage
}

func runKcpStart(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl kcp start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".kcp-specd", "state directory the kcp and kine keep their state in")
	out := fs.String("out", "", "also write the bound ports and urls here as JSON (default <root>/endpoint.json)")
	port := fs.Int("port", 0, "kcp secure port; 0 asks the kernel for a free one")
	kinePort := fs.Int("kine-port", 0, "kine listen port; 0 asks the kernel for a free one")
	reuse := fs.Bool("reuse", true, "reuse an already ready kcp whose endpoint.json names this root")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *root == "" {
		fmt.Fprintln(stderr, "specctl kcp start: --root is required")
		return exitUsage
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "specctl kcp start: %v\n", err)
		return exitError
	}
	*root = absolute

	if *reuse {
		if instance, ok := kcpproc.Probe(*root); ok {
			if err := kcpproc.SaveEndpoint(instance); err != nil {
				fmt.Fprintf(stderr, "specctl kcp start: %v\n", err)
				return exitError
			}
			if err := writePidFiles(instance); err != nil {
				fmt.Fprintf(stderr, "specctl kcp start: %v\n", err)
				return exitError
			}
			reportKcp(stdout, *root, instance, "kcp ready")
			return writeEndpoint(*out, *root, instance, stdout, stderr)
		}
	}

	instance, err := kcpproc.Start(context.Background(), kcpproc.Options{Root: *root, KcpPort: *port, KinePort: *kinePort})
	if err != nil {
		fmt.Fprintf(stderr, "specctl kcp start: %v\n", err)
		return exitError
	}
	if err := kcpproc.SaveEndpoint(instance); err != nil {
		fmt.Fprintf(stderr, "specctl kcp start: %v\n", err)
		return exitError
	}
	if err := writePidFiles(instance); err != nil {
		fmt.Fprintf(stderr, "specctl kcp start: %v\n", err)
		return exitError
	}
	reportKcp(stdout, *root, instance, "kcp ready")
	return writeEndpoint(*out, *root, instance, stdout, stderr)
}

func reportKcp(stdout io.Writer, root string, instance kcpproc.Instance, headline string) {
	fmt.Fprintln(stdout, headline)
	fmt.Fprintf(stdout, "kubeconfig: %s\n", instance.AdminKubeconfig)
	fmt.Fprintf(stdout, "endpoint: %s\n", kcpproc.EndpointPath(root))
	fmt.Fprintf(stdout, "kcp: %s\n", instance.KcpURL)
	fmt.Fprintf(stdout, "kine: %s\n", instance.KineURL)
}

func writeEndpoint(out, root string, instance kcpproc.Instance, stdout, stderr io.Writer) int {
	if out == "" {
		return exitOK
	}
	absolute, err := filepath.Abs(out)
	if err != nil {
		fmt.Fprintf(stderr, "specctl kcp start: %v\n", err)
		return exitError
	}
	if absolute != kcpproc.EndpointPath(root) {
		if err := kcpproc.SaveEndpointTo(absolute, instance); err != nil {
			fmt.Fprintf(stderr, "specctl kcp start: write %s: %v\n", absolute, err)
			return exitError
		}
	}
	return exitOK
}

func runKcpStop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl kcp stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".kcp-specd", "state directory the kcp and kine keep their state in")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *root == "" {
		fmt.Fprintln(stderr, "specctl kcp stop: --root is required")
		return exitUsage
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "specctl kcp stop: %v\n", err)
		return exitError
	}
	*root = absolute

	if instance, err := kcpproc.LoadEndpoint(*root); err == nil {
		kcpproc.Stop(instance)
		removeQuietly(kcpproc.EndpointPath(*root), filepath.Join(*root, "kcp.pid"), filepath.Join(*root, "kine.pid"))
		fmt.Fprintf(stdout, "kcp and kine stopped for %s\n", *root)
		return exitOK
	}

	pids, err := pidsForRoot(*root)
	if err != nil {
		fmt.Fprintf(stderr, "specctl kcp stop: %v\n", err)
		return exitError
	}
	if len(pids) == 0 {
		fmt.Fprintf(stdout, "nothing running for %s\n", *root)
		removeQuietly(filepath.Join(*root, "kcp.pid"), filepath.Join(*root, "kine.pid"))
		return exitOK
	}
	for _, pid := range pids {
		kcpproc.Terminate(pid, *root)
	}
	removeQuietly(filepath.Join(*root, "kcp.pid"), filepath.Join(*root, "kine.pid"))
	fmt.Fprintf(stdout, "kcp and kine stopped for %s\n", *root)
	return exitOK
}

func runKcpEndpoint(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl kcp endpoint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".kcp-specd", "state directory the kcp and kine keep their state in")
	output := fs.String("o", "json", "json or sh (export lines)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	instance, err := kcpproc.LoadEndpoint(*root)
	if err != nil {
		fmt.Fprintf(stderr, "specctl kcp endpoint: %v\n", err)
		return exitError
	}
	switch *output {
	case "json":
		data, err := json.MarshalIndent(instance, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl kcp endpoint: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(data))
	case "sh":
		for _, pair := range [][2]string{
			{"KCP_URL", instance.KcpURL},
			{"KCP_PORT", strconv.Itoa(instance.KcpPort)},
			{"KCP_PID", strconv.Itoa(instance.KcpPid)},
			{"KINE_URL", instance.KineURL},
			{"KINE_PORT", strconv.Itoa(instance.KinePort)},
			{"KINE_PID", strconv.Itoa(instance.KinePid)},
			{"KUBECONFIG", instance.AdminKubeconfig},
			{"KCP_ROOT", instance.Root},
		} {
			fmt.Fprintf(stdout, "export %s=%q\n", pair[0], pair[1])
		}
	default:
		fmt.Fprintf(stderr, "specctl kcp endpoint: unknown output %q\n", *output)
		return exitUsage
	}
	return exitOK
}

func writePidFiles(instance kcpproc.Instance) error {
	for name, pid := range map[string]int{"kcp": instance.KcpPid, "kine": instance.KinePid} {
		if pid <= 0 {
			continue
		}
		if err := os.WriteFile(filepath.Join(instance.Root, name+".pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func removeQuietly(paths ...string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func pidsForRoot(root string) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	pids := []int{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(data), "\x00", " ")
		if !strings.Contains(cmdline, root) {
			continue
		}
		fields := strings.Fields(cmdline)
		if len(fields) == 0 {
			continue
		}
		switch filepath.Base(fields[0]) {
		case "kcp", "kine":
			pids = append(pids, pid)
		}
	}
	return pids, nil
}
