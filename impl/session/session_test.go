package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("SPECD_STATE_DIR", t.TempDir())
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	top, err := TopLevel(repo)
	if err != nil {
		t.Fatal(err)
	}
	return top
}

func TestRecordRoundTripPerBranchAndDiscovery(t *testing.T) {
	top := initRepo(t)
	if _, ok := ForDir(top); ok {
		t.Fatal("a session was found before up")
	}
	if err := Save(Record{Repo: top, Repository: "calc", Branch: "feature/x", Workspace: "root:calc"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ForDir(top); ok {
		t.Fatal("ForDir found a session for a branch the checkout is not on")
	}
	if err := Save(Record{Repo: top, Repository: "calc", Branch: "main", Workspace: "root:calc"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ForDir(top); !ok {
		t.Fatal("the main branch's session was not found")
	}
	if rel, err := filepath.Rel(top, Path(top, "main")); err == nil && !filepath.IsAbs(rel) && rel[:2] != ".." {
		t.Fatalf("the session record is inside the project tree: %s", Path(top, "main"))
	}
	if Path(top, "main") == Path(top, "feature/x") || KcpRoot(top, "main") == KcpRoot(top, "feature/x") {
		t.Fatal("two branches share a session record or a kcp root")
	}
	if got := BranchSlug("feature/x"); got != "feature-x" {
		t.Fatalf("slug = %q", got)
	}
	records, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("List = %+v", records)
	}
	if err := Remove(top, "main"); err != nil {
		t.Fatal(err)
	}
	records, err = List()
	if err != nil || len(records) != 1 || records[0].Branch != "feature/x" {
		t.Fatalf("after removing main: %+v, %v", records, err)
	}
}

func TestLegacyRecordIsAdoptedOnTheDefaultBranch(t *testing.T) {
	top := initRepo(t)
	if err := Save(Record{Repo: top, Repository: "calc", Workspace: "root:calc", KcpRoot: filepath.Join(statedirPath(t), "repos", "old", "kcp")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LegacyPath(top)); err != nil {
		t.Fatalf("the legacy record was not written: %v", err)
	}
	record, ok, err := Load(top, "feature", "main")
	if err != nil || ok {
		t.Fatalf("a legacy record was adopted on another branch: %+v, %v, %v", record, ok, err)
	}
	record, ok, err = Load(top, "main", "main")
	if err != nil || !ok {
		t.Fatalf("the legacy record was not adopted on the default branch: %v, %v", ok, err)
	}
	if record.Branch != "main" || record.KcpRoot != filepath.Join(statedirPath(t), "repos", "old", "kcp") {
		t.Fatalf("the adopted record lost its state: %+v", record)
	}
	if _, err := os.Stat(LegacyPath(top)); !os.IsNotExist(err) {
		t.Fatalf("the legacy record survived adoption: %v", err)
	}
	if _, err := os.Stat(Path(top, "main")); err != nil {
		t.Fatalf("the branch record was not written: %v", err)
	}
}

func statedirPath(t *testing.T) string {
	t.Helper()
	return os.Getenv("SPECD_STATE_DIR")
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
