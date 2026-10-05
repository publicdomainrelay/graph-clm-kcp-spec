package fixture

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func Root(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the repository root")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func Copy(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join(Root(t), "fixtures", name)
	target := filepath.Join(t.TempDir(), name)
	if err := copyTree(source, target); err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	run(t, target, "git", "init", "-q", "-b", "main")
	run(t, target, "git", "add", "-A")
	run(t, target, "git", "-c", "user.email=fixture@example.com", "-c", "user.name=fixture", "commit", "-qm", "fixture")
	return target
}

func CopyAs(t *testing.T, name, as string) string {
	t.Helper()
	source := filepath.Join(Root(t), "fixtures", name)
	target := filepath.Join(t.TempDir(), as)
	if err := copyTree(source, target); err != nil {
		t.Fatalf("copy fixture %s as %s: %v", name, as, err)
	}
	run(t, target, "git", "init", "-q", "-b", "main")
	run(t, target, "git", "add", "-A")
	run(t, target, "git", "-c", "user.email=fixture@example.com", "-c", "user.name=fixture", "commit", "-qm", "fixture")
	return target
}

func CopyTree(t *testing.T, name string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), name)
	Stage(t, name, target)
	return target
}

func Stage(t *testing.T, name, target string) {
	t.Helper()
	source := filepath.Join(Root(t), "fixtures", name)
	if err := copyTree(source, target); err != nil {
		t.Fatalf("stage fixture %s at %s: %v", name, target, err)
	}
}

func Commit(t *testing.T, dir, message string) string {
	t.Helper()
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "-c", "user.email=fixture@example.com", "-c", "user.name=fixture", "commit", "-qm", message)
	head, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(head))
}

func Require(t *testing.T, tools ...string) {
	t.Helper()
	if testing.Short() && os.Getenv("SPECD_REQUIRE_LIVE") != "1" {
		t.Skip("live test skipped in short mode")
	}
	missing := []string{}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		return
	}
	if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
		t.Fatalf("SPECD_REQUIRE_LIVE=1 but these tools are missing: %s", strings.Join(missing, ", "))
	}
	t.Skipf("missing tools: %s", strings.Join(missing, ", "))
}

func run(t *testing.T, dir, command string, args ...string) {
	t.Helper()
	process := exec.Command(command, args...)
	process.Dir = dir
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v: %s", command, strings.Join(args, " "), err, output)
	}
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == ".codegraph") {
			return filepath.SkipDir
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		return copyFile(path, destination)
	})
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	return os.Chmod(target, info.Mode())
}
