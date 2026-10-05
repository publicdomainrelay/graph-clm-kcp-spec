package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpproc"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

type branchRecord struct {
	Repo            string `json:"repo"`
	Repository      string `json:"repository"`
	Branch          string `json:"branch"`
	Workspace       string `json:"workspace"`
	AdminKubeconfig string `json:"adminKubeconfig"`
	KcpRoot         string `json:"kcpRoot"`
	KcpPort         int    `json:"kcpPort"`
	KcpURL          string `json:"kcpURL"`
	KcpPid          int    `json:"kcpPid"`
	KinePort        int    `json:"kinePort"`
	KineURL         string `json:"kineURL"`
	KinePid         int    `json:"kinePid"`
	SpecdPid        int    `json:"specdPid"`
}

func branchSession(t *testing.T, path string) branchRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	record := branchRecord{}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func buildSpecctlAndSpecd(t *testing.T) (string, string) {
	t.Helper()
	root := repoRoot(t)
	bin := t.TempDir()
	for _, name := range []string{"specctl", "specd"} {
		build := exec.Command("go", "build", "-o", filepath.Join(bin, name), "./cmd/"+name)
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
	}
	return filepath.Join(bin, "specctl"), filepath.Join(bin, "specd")
}

func cloneCalcFixture(t *testing.T, dir string) {
	t.Helper()
	fixture.Stage(t, "calc", dir)
	phase13Git(t, dir, "init", "-q", "-b", "main")
	phase13Git(t, dir, "add", "-A")
	phase13Git(t, dir, "-c", "user.name=u", "-c", "user.email=u@u", "commit", "-qm", "upstream code")
}

func specdRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return err == nil && strings.Contains(strings.ReplaceAll(string(data), "\x00", " "), "specd")
}

func kcpServing(record branchRecord) bool {
	return kcpproc.Ready(kcpproc.Instance{
		Root:            record.KcpRoot,
		KcpPort:         record.KcpPort,
		KcpPid:          record.KcpPid,
		KinePort:        record.KinePort,
		KinePid:         record.KinePid,
		KineURL:         record.KineURL,
		AdminKubeconfig: record.AdminKubeconfig,
	})
}

func downEveryBranch(t *testing.T, specctl, repo, state string, branches []string) {
	t.Helper()
	for _, branch := range branches {
		if err := exec.Command("git", "-C", repo, "checkout", "-q", branch).Run(); err != nil {
			continue
		}
		command := exec.Command(specctl, "down")
		command.Dir = repo
		command.Env = append(os.Environ(),
			"SPECD_STATE_DIR="+state,
			"SPECD_KUBECONFIG=",
			"SPECD_CLM_DOC_DIR=",
		)
		_ = command.Run()
	}
}

func TestPhase14OneInstancePerBranch(t *testing.T) {
	requireLive(t, "git", "kubectl", "kcp", "kine")
	specctl, specd := buildSpecctlAndSpecd(t)
	work := t.TempDir()
	repo := filepath.Join(work, "calc")
	cloneCalcFixture(t, repo)
	machine := phase13Machine{state: filepath.Join(work, "state")}
	t.Cleanup(func() { downEveryBranch(t, specctl, repo, machine.state, []string{"main", "feature"}) })

	outMain := filepath.Join(work, "main.json")
	upMain := machine.run(t, specctl, repo, "up", "--summarize=false", "--remote", "", "--specd", specd, "--clm-mod", "", "--out", outMain)
	if !strings.Contains(upMain, "no open-architecture/calc branch yet") {
		t.Fatalf("main did not index from scratch:\n%s", upMain)
	}
	phase13Wait(t, "main to be populated", func() bool {
		return strings.Contains(machine.run(t, specctl, repo, "status"), "Populated")
	})
	phase13Wait(t, "main's architecture branch", func() bool {
		return exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", "refs/heads/open-architecture/calc").Run() == nil
	})
	mainRecord := branchSession(t, outMain)
	if mainRecord.Branch != "main" || mainRecord.Repository != "calc" {
		t.Fatalf("main's record = %+v", mainRecord)
	}
	if !kcpServing(mainRecord) || !specdRunning(mainRecord.SpecdPid) {
		t.Fatalf("main's instance is not running: %+v", mainRecord)
	}

	phase13Git(t, repo, "switch", "-c", "feature")
	outFeature := filepath.Join(work, "feature.json")
	upFeature := machine.run(t, specctl, repo, "up", "--summarize=false", "--remote", "", "--specd", specd, "--clm-mod", "", "--out", outFeature)
	if !strings.Contains(upFeature, "restored") || !strings.Contains(upFeature, "open-architecture/calc at") {
		t.Fatalf("feature did not restore from main's architecture:\n%s", upFeature)
	}
	if !strings.Contains(upFeature, "specd of branch main stopped") {
		t.Fatalf("up on feature did not stop main's specd:\n%s", upFeature)
	}
	featureRecord := branchSession(t, outFeature)
	if featureRecord.Branch != "feature" {
		t.Fatalf("feature's record = %+v", featureRecord)
	}
	if featureRecord.KcpPort == mainRecord.KcpPort || featureRecord.KinePort == mainRecord.KinePort || featureRecord.KcpRoot == mainRecord.KcpRoot {
		t.Fatalf("the two branches share an instance: %+v %+v", mainRecord, featureRecord)
	}
	if specdRunning(mainRecord.SpecdPid) {
		t.Fatal("main's specd survived up on feature")
	}
	if !kcpServing(mainRecord) {
		t.Fatal("feature's up stopped main's kcp")
	}

	server := strings.TrimSpace(machine.run(t, specctl, repo, "env", "-o", "server"))
	patch := exec.Command("kubectl",
		"--kubeconfig", filepath.Join(featureRecord.KcpRoot, "admin.kubeconfig"),
		"--server", server,
		"patch", "systemcontext", "calc", "--type", "merge",
		"-p", `{"spec":{"intent":"Feature branch architecture, decided on feature."}}`)
	if out, err := patch.CombinedOutput(); err != nil {
		t.Fatalf("patch: %v\n%s", err, out)
	}
	phase13Wait(t, "feature's architecture branch", func() bool {
		out, err := exec.Command("git", "-C", repo, "show", "open-architecture/calc--feature:specs/calc.yaml").CombinedOutput()
		return err == nil && strings.Contains(string(out), "decided on feature")
	})
	if out, err := exec.Command("git", "-C", repo, "show", "open-architecture/calc:specs/calc.yaml").CombinedOutput(); err == nil && strings.Contains(string(out), "decided on feature") {
		t.Fatal("the feature edit leaked into main's architecture")
	}
	featureOutline := machine.run(t, specctl, repo, "arch", "outline")
	if !strings.Contains(featureOutline, "decided on feature") {
		t.Fatalf("feature's kcp lacks the edit:\n%s", featureOutline)
	}
	mainOutline := machine.run(t, specctl, repo, "arch", "outline",
		"--kubeconfig", filepath.Join(mainRecord.KcpRoot, "admin.kubeconfig"), "--workspace", "root:calc")
	if strings.Contains(mainOutline, "decided on feature") {
		t.Fatalf("main's kcp carries the feature edit:\n%s", mainOutline)
	}

	phase13Git(t, repo, "switch", "main")
	outMain2 := filepath.Join(work, "main2.json")
	upMain2 := machine.run(t, specctl, repo, "up", "--summarize=false", "--remote", "", "--specd", specd, "--clm-mod", "", "--out", outMain2)
	mainAgain := branchSession(t, outMain2)
	if mainAgain.KcpPort != mainRecord.KcpPort || mainAgain.KcpRoot != mainRecord.KcpRoot {
		t.Fatalf("main's instance moved: %+v, was %+v", mainAgain, mainRecord)
	}
	if !strings.Contains(upMain2, "already running") {
		t.Fatalf("main's kcp was not adopted:\n%s", upMain2)
	}
	if !strings.Contains(upMain2, "specd of branch feature stopped") {
		t.Fatalf("up on main did not stop feature's specd:\n%s", upMain2)
	}
	if specdRunning(featureRecord.SpecdPid) {
		t.Fatal("feature's specd survived up on main")
	}
	if !kcpServing(featureRecord) {
		t.Fatal("up on main stopped feature's kcp")
	}

	listed := machine.run(t, specctl, repo, "ls")
	for _, want := range []string{mainRecord.KcpURL, featureRecord.KcpURL, "main", "feature"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("specctl ls does not list %s:\n%s", want, listed)
		}
	}
	if strings.Count(listed, "running, pid") != 1 || strings.Count(listed, "stopped") != 1 {
		t.Fatalf("specctl ls should show one running and one stopped specd:\n%s", listed)
	}
}

func TestPhase14TwoWorktreesRunAtTheSameTime(t *testing.T) {
	requireLive(t, "git", "kcp", "kine")
	specctl, specd := buildSpecctlAndSpecd(t)
	work := t.TempDir()
	repo := filepath.Join(work, "a", "calc")
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		t.Fatal(err)
	}
	cloneCalcFixture(t, repo)
	machine := phase13Machine{state: filepath.Join(work, "state")}
	other := filepath.Join(work, "b", "calc")
	t.Cleanup(func() {
		for _, dir := range []string{repo, other} {
			command := exec.Command(specctl, "down")
			command.Dir = dir
			command.Env = append(os.Environ(), "SPECD_STATE_DIR="+machine.state, "SPECD_KUBECONFIG=", "SPECD_CLM_DOC_DIR=")
			_ = command.Run()
		}
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", other).Run()
	})

	outMain := filepath.Join(work, "main.json")
	machine.run(t, specctl, repo, "up", "--summarize=false", "--remote", "", "--specd", specd, "--clm-mod", "", "--out", outMain)
	phase13Wait(t, "the main checkout to be populated", func() bool {
		return strings.Contains(machine.run(t, specctl, repo, "status"), "Populated")
	})

	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "feature", other, "main").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	outFeature := filepath.Join(work, "feature.json")
	machine.run(t, specctl, other, "up", "--summarize=false", "--remote", "", "--specd", specd, "--clm-mod", "", "--out", outFeature)
	phase13Wait(t, "the worktree to be populated", func() bool {
		return strings.Contains(machine.run(t, specctl, other, "status"), "Populated")
	})

	mainRecord := branchSession(t, outMain)
	featureRecord := branchSession(t, outFeature)
	if mainRecord.KcpRoot == featureRecord.KcpRoot || mainRecord.KcpPort == featureRecord.KcpPort {
		t.Fatalf("the two worktrees share an instance: %+v %+v", mainRecord, featureRecord)
	}
	if mainRecord.Repository != featureRecord.Repository {
		t.Fatalf("the worktrees disagree about the repository: %q %q", mainRecord.Repository, featureRecord.Repository)
	}
	if !specdRunning(mainRecord.SpecdPid) || !specdRunning(featureRecord.SpecdPid) {
		t.Fatalf("the two specds do not run at once: %+v %+v", mainRecord, featureRecord)
	}
	if !kcpServing(mainRecord) || !kcpServing(featureRecord) {
		t.Fatal("both kcps must serve at once")
	}
	listed := machine.run(t, specctl, repo, "ls")
	if strings.Count(listed, "running, pid") != 2 {
		t.Fatalf("specctl ls does not show two running specds:\n%s", listed)
	}
}

func TestPhase14DownThenUpResumesOnTheSameRoot(t *testing.T) {
	requireLive(t, "git", "kcp", "kine")
	specctl, specd := buildSpecctlAndSpecd(t)
	work := t.TempDir()
	repo := filepath.Join(work, "calc")
	cloneCalcFixture(t, repo)
	machine := phase13Machine{state: filepath.Join(work, "state")}
	t.Cleanup(func() { downEveryBranch(t, specctl, repo, machine.state, []string{"main"}) })

	outFirst := filepath.Join(work, "first.json")
	machine.run(t, specctl, repo, "up", "--summarize=false", "--remote", "", "--specd", specd, "--clm-mod", "", "--out", outFirst)
	phase13Wait(t, "the checkout to be populated", func() bool {
		return strings.Contains(machine.run(t, specctl, repo, "status"), "Populated")
	})
	first := branchSession(t, outFirst)
	if !kcpServing(first) {
		t.Fatalf("the first up left no kcp: %+v", first)
	}
	phase13Wait(t, "the architecture branch", func() bool {
		return exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", "refs/heads/open-architecture/calc").Run() == nil
	})
	tip := phase13Git(t, repo, "rev-parse", "open-architecture/calc")

	down := machine.run(t, specctl, repo, "down")
	if !strings.Contains(down, "stopped") {
		t.Fatalf("specctl down did not stop the instance:\n%s", down)
	}
	if kcpServing(first) {
		t.Fatal("specctl down left the kcp serving")
	}

	outSecond := filepath.Join(work, "second.json")
	up := machine.run(t, specctl, repo, "up", "--summarize=false", "--remote", "", "--specd", specd, "--clm-mod", "", "--out", outSecond)
	if strings.Contains(up, "already running") {
		t.Fatalf("up adopted a kcp that was stopped:\n%s", up)
	}
	if !strings.Contains(up, "kcp already holds Repository calc") {
		t.Fatalf("the restart did not find the state the first up wrote:\n%s", up)
	}
	second := branchSession(t, outSecond)
	if second.KcpRoot != first.KcpRoot || second.KcpPort != first.KcpPort {
		t.Fatalf("the restart moved the instance: %+v, was %+v", second, first)
	}
	if !kcpServing(second) {
		t.Fatalf("the restarted kcp does not serve: %+v", second)
	}
	if got := phase13Git(t, repo, "rev-parse", "open-architecture/calc"); got != tip {
		t.Fatalf("the restart rewrote the architecture branch: %s, want %s", got, tip)
	}
}
