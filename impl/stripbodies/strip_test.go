package stripbodies_test

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/stripbodies"
)

func write(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStripGoKeepsSignaturesAndPrunesUnusedImports(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "pkg/calc.go", `package calc

import (
	"fmt"
	"strings"
)

type Pair struct {
	Name string
}

func Add(a, b int) int {
	return a + b
}

func (p Pair) Label() string {
	return fmt.Sprintf("%s", strings.ToUpper(p.Name))
}
`)
	result, err := stripbodies.Strip(dir, []string{"pkg/calc.go"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 || result.Stripped != 2 {
		t.Fatalf("result = %+v, want two bodies in one file", result)
	}
	stripped, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(stripped)
	for _, want := range []string{"func Add(a, b int) int {", "func (p Pair) Label() string {", `panic("unimplemented")`, "type Pair struct"} {
		if !strings.Contains(text, want) {
			t.Errorf("the stripped file lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"fmt"`) || strings.Contains(text, `"strings"`) {
		t.Errorf("an import only the removed bodies used survived:\n%s", text)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, stripped, 0); err != nil {
		t.Errorf("the stripped file does not parse: %v", err)
	}
}

// TestStripGoKeepsAnImportAKeptDeclarationUses is the other half of the import
// pruning: a package a signature or a variable still names must survive.
func TestStripGoKeepsAnImportAKeptDeclarationUses(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "pkg/err.go", `package pkg

import "errors"

var ErrMissing = errors.New("missing")

func Lookup(id int) error {
	return ErrMissing
}
`)
	if _, err := stripbodies.Strip(dir, []string{"pkg/err.go"}); err != nil {
		t.Fatal(err)
	}
	stripped, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stripped), `"errors"`) {
		t.Errorf("the import a kept variable uses was pruned:\n%s", stripped)
	}
}

func TestStripTypeScriptReplacesFunctionAndMethodBodies(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "mod.ts", `export interface Greeting {
  name: string;
}

export function greet(name: string): Greeting {
  return { name, text: "hello, " + name };
}

export function shout(name: string): string {
  return greet(name).text.toUpperCase();
}

export class Greeter {
  constructor(private readonly prefix: string) {}

  greeting(name: string): string {
    return this.prefix + " " + name;
  }
}
`)
	result, err := stripbodies.Strip(dir, []string{"mod.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stripped != 4 {
		t.Fatalf("result = %+v, want four bodies", result)
	}
	stripped, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(stripped)
	if strings.Count(text, `throw new Error("unimplemented")`) != 4 {
		t.Errorf("the bodies were not all replaced:\n%s", text)
	}
	for _, want := range []string{
		"export function greet(name: string): Greeting",
		"export function shout(name: string): string",
		"greeting(name: string): string",
		"export interface Greeting",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the stripped file lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "toUpperCase") || strings.Contains(text, "this.prefix") {
		t.Errorf("a body survived:\n%s", text)
	}
}

// TestStripRealFixturesStillTypeCheck is the fixture-shaped check: the greet
// module uses template literals and a regular expression, and the todo store is
// Go with an import a removed body used. Both have to survive the strip and
// still compile, or the sufficiency measure builds a tree that fails before the
// agent starts.
func TestStripRealFixturesStillTypeCheck(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "fixtures")); err != nil {
		t.Skipf("the fixtures are not here: %v", err)
	}

	ts := t.TempDir()
	for _, name := range []string{"mod.ts", "format/mod.ts"} {
		contents, err := os.ReadFile(filepath.Join(root, "fixtures/greet", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, ts, name, string(contents))
	}
	result, err := stripbodies.Strip(ts, []string{"mod.ts", "format/mod.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stripped != 6 {
		t.Fatalf("greet stripped %d bodies, want four functions and two methods", result.Stripped)
	}
	if deno, err := exec.LookPath("deno"); err == nil {
		command := exec.Command(deno, "check", "--quiet", "mod.ts", "format/mod.ts")
		command.Dir = ts
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("the stripped TypeScript does not type check: %v: %s", err, output)
		}
	}

	gos := t.TempDir()
	for _, name := range []string{"todo/todo.go", "todo/todo_test.go", "go.mod"} {
		contents, err := os.ReadFile(filepath.Join(root, "fixtures/todo", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, gos, filepath.FromSlash(name), string(contents))
	}
	result, err = stripbodies.Strip(gos, []string{"todo/todo.go"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stripped != 5 {
		t.Fatalf("the todo store stripped %d bodies, want NewStore, Add, List, Get and Complete", result.Stripped)
	}
	stripped, err := os.ReadFile(filepath.Join(gos, "todo/todo.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stripped), `"errors"`) {
		t.Errorf("the errors import a kept variable uses was pruned:\n%s", stripped)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestStripLeavesTestsAndUnknownLanguagesAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pkg/calc.go", "package pkg\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	write(t, dir, "pkg/calc_test.go", "package pkg\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n")
	write(t, dir, "README.md", "# readme\n")
	result, err := stripbodies.Strip(dir, []string{"pkg/calc.go", "pkg/calc_test.go", "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 || result.Stripped != 1 {
		t.Fatalf("result = %+v", result)
	}
	test, err := os.ReadFile(filepath.Join(dir, "pkg/calc_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(test), "Add(1, 2) != 3") {
		t.Error("the test file was stripped")
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "README.md" {
		t.Errorf("skipped = %v", result.Skipped)
	}
}
