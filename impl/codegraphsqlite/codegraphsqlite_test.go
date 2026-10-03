package codegraphsqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	specsync "github.com/publicdomainrelay/graph-clm-kcp-spec/abc/sync"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphsqlite"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/test/fixture"
)

func openFixture(t *testing.T) (*codegraphsqlite.DB, string) {
	t.Helper()
	fixture.Require(t, "codegraph", "git")
	repo := fixture.Copy(t, "calc")
	ctx := context.Background()
	dbPath, err := codegraphsqlite.Ensure(ctx, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if dbPath != filepath.Join(repo, ".codegraph", "codegraph.db") {
		t.Fatalf("index path = %s", dbPath)
	}
	database, err := codegraphsqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database, repo
}

func TestEnsureIndexesTheRepository(t *testing.T) {
	database, repo := openFixture(t)
	ctx := context.Background()
	files, err := database.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %+v, want the three fixture files", files)
	}
	again, err := codegraphsqlite.Ensure(ctx, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(again); err != nil {
		t.Fatalf("a second ensure must keep the index: %v", err)
	}
	repoPath, found, err := codegraphsqlite.OpenRepo(repo)
	if err != nil || !found {
		t.Fatalf("OpenRepo = %v, %v", found, err)
	}
	repoPath.Close()
}

func TestFactsCarryExportedSymbols(t *testing.T) {
	database, _ := openFixture(t)
	ctx := context.Background()
	facts, err := database.Facts(ctx, "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Commit != "deadbeef" {
		t.Errorf("commit = %q", facts.Commit)
	}
	if len(facts.Files) != 3 {
		t.Errorf("files = %+v", facts.Files)
	}
	partitions := specsync.PartitionFacts(facts, "calc")
	if len(partitions) != 2 {
		t.Fatalf("partitions = %+v", partitions)
	}
	observed := specsync.Observed(partitions[0])
	if len(observed.Interfaces) != 2 {
		t.Fatalf("interfaces = %+v, want Add and Multiply", observed.Interfaces)
	}
	for _, want := range []string{"Add", "Multiply"} {
		found := false
		for _, observedInterface := range observed.Interfaces {
			if observedInterface.Name == want {
				found = true
				if observedInterface.CodegraphID == "" {
					t.Errorf("%s has no codegraph id", want)
				}
				if observedInterface.File != "calc/calc.go" {
					t.Errorf("%s file = %q", want, observedInterface.File)
				}
			}
		}
		if !found {
			t.Errorf("%s was not observed", want)
		}
	}
	if len(observed.Interfaces) > 0 && observed.Interfaces[0].Line == 0 {
		t.Error("observed interfaces must carry a line")
	}
}

// TestFactsCarryExportedMethods is the Go half of the observed surface: the
// index reports is_exported false for every method node, so a reader that
// trusted it would see no exported method at all and the spec of a service
// would be missing most of what it offers.
func TestFactsCarryExportedMethods(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repo := fixture.Copy(t, "todo")
	ctx := context.Background()
	dbPath, err := codegraphsqlite.Ensure(ctx, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	database, err := codegraphsqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	facts, err := database.Facts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, symbol := range facts.Symbols {
		names[symbol.Name] = true
	}
	for _, want := range []string{"Add", "List", "Get", "Complete", "NewStore"} {
		if !names[want] {
			t.Errorf("%s is not an observed symbol", want)
		}
	}
}

// indexTree writes a small tree and indexes it, so a visibility or keying rule
// can be proven against source written for the test rather than against a
// fixture whose shape another test also depends on.
func indexTree(t *testing.T, files map[string]string) *codegraphsqlite.DB {
	t.Helper()
	fixture.Require(t, "codegraph")
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dbPath, err := codegraphsqlite.Ensure(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	database, err := codegraphsqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// TestFactsQualifyMethodsByReceiver is the keying half of the observed surface:
// two types that both offer a method named List are two symbols with two keys,
// because a surface keyed by bare name could hold only one of them.
func TestFactsQualifyMethodsByReceiver(t *testing.T) {
	database := indexTree(t, map[string]string{
		"store.go": `package store

type Indexer struct{}

func (i *Indexer) List() []string { return nil }

func (i *Indexer) Add(item string) {}

type Set struct{}

func (s *Set) List() []string { return nil }

func (s *Set) Add(item string) {}

type SetAlias = Set
`,
	})
	facts, err := database.Facts(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, symbol := range facts.Symbols {
		key := specsync.InterfaceKey(symbol)
		if _, exists := keys[key]; exists {
			t.Errorf("two symbols share the key %q", key)
		}
		keys[key] = symbol.Name
	}
	for _, want := range []string{"Indexer.List", "Set.List", "Indexer.Add", "Set.Add"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("%s was not observed; keys = %v", want, keys)
		}
	}
	// A ref the spec stores is keyed the same way, so it has to resolve to the
	// one node: the index spells the receiver with `::` and the spec with `.`.
	nodes, err := database.Resolve(context.Background(), "Indexer.List")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "List" || nodes[0].FilePath != "store.go" {
		t.Errorf("Resolve(Indexer.List) = %+v, want the one Indexer method", nodes)
	}
}

// TestFactsReportTypeScriptMemberVisibility is the TypeScript half of the
// observed surface: the index reports every class member unexported, but a
// member is public unless it says private or protected, or carries a # name.
func TestFactsReportTypeScriptMemberVisibility(t *testing.T) {
	database := indexTree(t, map[string]string{
		"sample.ts": `export class Sample {
  constructor(private readonly label: string) {}
  open(): string { return this.label; }
  protected guarded(): string { return "g"; }
  private hidden(): string { return "h"; }
  #secret(): string { return "s"; }
}

class Local {
  visible(): string { return "v"; }
}
`,
	})
	facts, err := database.Facts(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{}
	for _, symbol := range facts.Symbols {
		symbols[specsync.InterfaceKey(symbol)] = true
	}
	for _, want := range []string{"Sample", "Sample.open", "Sample.constructor"} {
		if !symbols[want] {
			t.Errorf("%s is not observed; symbols = %v", want, symbols)
		}
	}
	for _, unwanted := range []string{"Sample.guarded", "Sample.hidden", "Sample.#secret", "Local.visible", "Local"} {
		if symbols[unwanted] {
			t.Errorf("%s must not be observed; symbols = %v", unwanted, symbols)
		}
	}
}

// TestFactsObserveTheGreetClassMembers is the fixture case for the TypeScript
// half: the greet module exports a class, and its public members belong to the
// observed surface exactly as its functions do. No scenario is involved: this
// is the index and the reader alone.
func TestFactsObserveTheGreetClassMembers(t *testing.T) {
	fixture.Require(t, "codegraph", "git")
	repo := fixture.Copy(t, "greet")
	dbPath, err := codegraphsqlite.Ensure(context.Background(), repo, "")
	if err != nil {
		t.Fatal(err)
	}
	database, err := codegraphsqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	facts, err := database.Facts(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]string{}
	for _, symbol := range facts.Symbols {
		symbols[specsync.InterfaceKey(symbol)] = symbol.File
	}
	for _, want := range []string{"Greeter", "Greeter.constructor", "Greeter.greeting", "greet", "shout", "Greeting"} {
		if _, ok := symbols[want]; !ok {
			t.Errorf("%s is not observed; symbols = %v", want, symbols)
		}
	}
	if symbols["Greeter.greeting"] != "mod.ts" {
		t.Errorf("Greeter.greeting file = %q", symbols["Greeter.greeting"])
	}
}

func TestResolveNeverComputesAnID(t *testing.T) {
	database, _ := openFixture(t)
	ctx := context.Background()
	nodes, err := database.Resolve(ctx, "Add")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("Resolve(Add) = %+v", nodes)
	}
	if nodes[0].FilePath != "calc/calc.go" || nodes[0].Signature != "(a, b int) int" {
		t.Fatalf("node = %+v", nodes[0])
	}
	byID, err := database.Resolve(ctx, "file:calc/calc.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(byID) != 1 || byID[0].Kind != "file" {
		t.Fatalf("Resolve(file:calc/calc.go) = %+v", byID)
	}
	byPath, err := database.Resolve(ctx, "calc/calc.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(byPath) != 1 || byPath[0].ID != byID[0].ID {
		t.Fatalf("Resolve(calc/calc.go) = %+v", byPath)
	}
	nodes, err = database.Resolve(ctx, "no-such-symbol")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Fatalf("Resolve(no-such-symbol) = %+v, want nothing", nodes)
	}
}

func TestEdgesReadback(t *testing.T) {
	database, _ := openFixture(t)
	edges, err := database.Edges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Fatal("no edges")
	}
	kinds := map[string]bool{}
	for _, edge := range edges {
		kinds[edge.Kind] = true
		if edge.Source == "" || edge.Target == "" {
			t.Fatalf("edge without endpoints: %+v", edge)
		}
	}
	if !kinds["contains"] && !kinds["calls"] && !kinds["imports"] {
		t.Fatalf("edge kinds = %+v", kinds)
	}
}
