package kcpproc

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

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
