package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type phase13Machine struct {
	state    string
	kcpPort  string
	kinePort string
}

func (m phase13Machine) run(t *testing.T, binary, dir string, args ...string) string {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"SPECD_STATE_DIR="+m.state,
		"SPECD_KUBECONFIG=",
		"SPECD_CLM_DOC_DIR=",
		"SPECD_UP_KCP_PORT="+m.kcpPort,
		"SPECD_UP_KINE_PORT="+m.kinePort,
	)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("%s %v: %v\n%s", filepath.Base(binary), args, err, output.String())
	}
	return output.String()
}

func phase13Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func phase13Wait(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestPhase13CloneUpPersistsAndASecondCloneRestores(t *testing.T) {
	requireLive(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"specctl", "specd"} {
		build := exec.Command("go", "build", "-o", filepath.Join(bin, name), "./cmd/"+name)
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
	}
	specctl := filepath.Join(bin, "specctl")

	work := t.TempDir()
	upstream := filepath.Join(work, "upstream")
	if out, err := exec.Command("cp", "-r", filepath.Join(root, "fixtures", "calc"), upstream).CombinedOutput(); err != nil {
		t.Fatalf("copy fixture: %v\n%s", err, out)
	}
	os.RemoveAll(filepath.Join(upstream, ".codegraph"))
	phase13Git(t, upstream, "init", "-q", "-b", "main")
	phase13Git(t, upstream, "add", "-A")
	phase13Git(t, upstream, "-c", "user.name=u", "-c", "user.email=u@u", "commit", "-qm", "upstream code")
	remote := filepath.Join(work, "remote", "calc.git")
	if out, err := exec.Command("git", "clone", "-q", "--bare", upstream, remote).CombinedOutput(); err != nil {
		t.Fatalf("bare clone: %v\n%s", err, out)
	}

	machineA := phase13Machine{state: filepath.Join(work, "state-a"), kcpPort: "6453", kinePort: "23803"}
	machineB := phase13Machine{state: filepath.Join(work, "state-b"), kcpPort: "6455", kinePort: "23805"}
	cloneA := filepath.Join(work, "a", "calc")
	cloneB := filepath.Join(work, "b", "somewhere-else")
	for _, clone := range []string{cloneA, cloneB} {
		if out, err := exec.Command("git", "clone", "-q", remote, clone).CombinedOutput(); err != nil {
			t.Fatalf("clone: %v\n%s", err, out)
		}
	}
	t.Cleanup(func() {
		for _, pair := range []struct {
			machine phase13Machine
			clone   string
		}{{machineA, cloneA}, {machineB, cloneB}} {
			command := exec.Command(specctl, "down", "--kcp")
			command.Dir = pair.clone
			command.Env = append(os.Environ(), "SPECD_STATE_DIR="+pair.machine.state)
			_ = command.Run()
		}
	})

	upA := machineA.run(t, specctl, cloneA, "up", "--summarize=false", "--push", "--specd", filepath.Join(bin, "specd"), "--clm-mod", "")
	if !strings.Contains(upA, "no open-architecture/calc yet") {
		t.Fatalf("clone A did not index from scratch:\n%s", upA)
	}
	phase13Wait(t, "the orphan branch on the remote", func() bool {
		return exec.Command("git", "-C", remote, "rev-parse", "-q", "--verify", "refs/heads/open-architecture/calc").Run() == nil
	})
	if status := phase13Git(t, cloneA, "status", "--porcelain"); status != "" {
		t.Fatalf("clone A's tree changed: %q", status)
	}
	if err := exec.Command("git", "-C", cloneA, "merge-base", "main", "open-architecture/calc").Run(); err == nil {
		t.Fatal("open-architecture/calc shares history with main")
	}

	server := strings.TrimSpace(machineA.run(t, specctl, cloneA, "env", "-o", "server"))
	patch := exec.Command("kubectl", "--kubeconfig", filepath.Join(machineA.state, "kcp", "admin.kubeconfig"), "--server", server,
		"patch", "systemcontext", "calc", "--type", "merge", "-p", `{"spec":{"intent":"Integer arithmetic, decided in clone A."}}`)
	if out, err := patch.CombinedOutput(); err != nil {
		t.Fatalf("patch: %v\n%s", err, out)
	}
	phase13Wait(t, "the intent on the remote branch", func() bool {
		out, err := exec.Command("git", "-C", remote, "show", "open-architecture/calc:specs/calc.yaml").CombinedOutput()
		return err == nil && strings.Contains(string(out), "decided in clone A")
	})

	upB := machineB.run(t, specctl, cloneB, "up", "--summarize=false", "--specd", filepath.Join(bin, "specd"), "--clm-mod", "")
	if !strings.Contains(upB, "restored") || !strings.Contains(upB, "the branch on origin") {
		t.Fatalf("clone B did not restore from the remote branch:\n%s", upB)
	}
	outline := machineB.run(t, specctl, cloneB, "arch", "outline")
	if !strings.Contains(outline, "decided in clone A") {
		t.Fatalf("clone B's kcp lacks clone A's decision:\n%s", outline)
	}
	if status := phase13Git(t, cloneB, "status", "--porcelain"); status != "" {
		t.Fatalf("clone B's tree changed: %q", status)
	}
	for _, artefact := range []string{".specs", "arch.yaml", "specs"} {
		for _, clone := range []string{cloneA, cloneB} {
			if _, err := os.Stat(filepath.Join(clone, artefact)); err == nil {
				t.Errorf("%s appeared in %s", artefact, clone)
			}
		}
	}
	status := machineB.run(t, specctl, cloneB, "status")
	if !strings.Contains(status, "open-architecture/calc at") {
		t.Fatalf("status:\n%s", status)
	}
}
