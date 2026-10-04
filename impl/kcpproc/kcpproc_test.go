package kcpproc

import (
	"context"
	"net"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

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
	instance, err := Start(ctx, Options{Root: root, KcpPort: port, KinePort: kinePort})
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

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
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
			instances[index], errs[index] = Start(ctx, Options{Root: root})
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
