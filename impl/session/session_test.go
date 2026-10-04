package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRecordRoundTripAndDiscovery(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("SPECD_STATE_DIR", t.TempDir())
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	top, err := TopLevel(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ForDir(repo); ok {
		t.Fatal("a session was found before up")
	}
	if err := Save(Record{Repo: top, Repository: "calc", Workspace: "root:calc"}); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	record, ok := ForDir(nested)
	if !ok || record.Workspace != "root:calc" {
		t.Fatalf("record = %+v, %v", record, ok)
	}
	if rel, err := filepath.Rel(repo, Path(top)); err == nil && !filepath.IsAbs(rel) && rel[:2] != ".." {
		t.Fatalf("the session record is inside the project tree: %s", Path(top))
	}
	if err := Remove(top); err != nil {
		t.Fatal(err)
	}
	if _, ok := ForDir(repo); ok {
		t.Fatal("the session survived down")
	}
}

func TestExtractDeployWritesTheScriptsAndCRDs(t *testing.T) {
	t.Setenv("SPECD_STATE_DIR", t.TempDir())
	dir, err := ExtractDeploy()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"start-kcp.sh", "install-specs.sh", "stop-kcp.sh", "crds/specs.publicdomainrelay.dev_repositories.yaml"} {
		info, err := os.Stat(filepath.Join(dir, path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if filepath.Ext(path) == ".sh" && info.Mode()&0o100 == 0 {
			t.Fatalf("%s is not executable", path)
		}
	}
}
