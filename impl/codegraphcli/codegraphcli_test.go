package codegraphcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stub(t *testing.T, output string) (command, argsFile, project string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	project = filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nprintf '%s\\n' " + "'" + output + "'" + "\n"
	command = filepath.Join(dir, "codegraph")
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return command, argsFile, project
}

func TestContextBuildsTheTaskFromTheContextAndItsInterfaces(t *testing.T) {
	command, argsFile, project := stub(t, "### relevant symbols")
	runner := Runner{Tool: command, Dir: project}

	text, err := runner.Context(context.Background(), "calc Add Multiply", 8)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(text) != "### relevant symbols" {
		t.Errorf("text = %q", text)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "context\n-p\n" + project + "\n-f\nmarkdown\n-n\n8\ncalc\nAdd\nMultiply"
	if got := strings.TrimSpace(string(args)); got != want {
		t.Errorf("argv =\n%s\nwant\n%s", got, want)
	}
}

func TestNodeAsksForOneSymbol(t *testing.T) {
	command, argsFile, project := stub(t, "func Add()")
	runner := Runner{Tool: command, Dir: project}

	if _, err := runner.Node(context.Background(), "Add"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(args)); got != "node\n-p\n"+project+"\nAdd" {
		t.Errorf("argv = %q", got)
	}
}

func TestAMissingToolIsAnErrorNotASilentEmptyExcerpt(t *testing.T) {
	runner := Runner{Tool: "definitely-not-a-real-codegraph", Dir: "/tmp/calc"}
	if _, err := runner.Node(context.Background(), "Add"); err == nil {
		t.Fatal("a missing tool was accepted")
	} else if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("error = %v", err)
	}
}

func TestAFailingCommandCarriesItsStderr(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "codegraph")
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'no index here' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Tool: command, Dir: dir}
	if _, err := runner.Context(context.Background(), "calc", 0); err == nil {
		t.Fatal("a failing command was accepted")
	} else if !strings.Contains(err.Error(), "no index here") {
		t.Errorf("error = %v", err)
	}
}

func TestDefaultToolIsCodegraph(t *testing.T) {
	if got := (Runner{}).tool(); got != DefaultTool {
		t.Errorf("tool = %q", got)
	}
}
