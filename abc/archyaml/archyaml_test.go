package archyaml

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "open-architecture", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func versions(t *testing.T) []string {
	t.Helper()
	return []string{"arch.yaml", "arch-e77a69f.yaml", "arch-85799e6.yaml"}
}

func TestParseFlattensNestedNodes(t *testing.T) {
	document, err := Parse(testdata(t, "arch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if document.Header["kind"] != "OpenArchitecture" {
		t.Fatalf("kind = %v", document.Header["kind"])
	}
	contexts := document.Section(SystemContexts)
	if contexts == nil || contexts.Form != FormList {
		t.Fatalf("system_contexts section = %+v", contexts)
	}
	if len(contexts.Roots()) != 7 {
		t.Fatalf("roots = %d, want 7", len(contexts.Roots()))
	}

	kind := document.Node("sc.kind.denopod")
	if kind == nil {
		t.Fatal("sc.kind.denopod is missing")
	}
	if kind.Parent != "sc.deno-kcp" {
		t.Fatalf("parent = %q, want sc.deno-kcp", kind.Parent)
	}
	if kind.Slot == "" {
		t.Fatal("a nested node must record the slot it sat in")
	}
	if kind.Section != SystemContexts || kind.Form != FormList {
		t.Fatalf("section/form = %q/%q", kind.Section, kind.Form)
	}

	upstream := document.Node("sc.kcp-local")
	if upstream == nil || upstream.Parent != "sc.deno-kcp" || upstream.Slot != "upstream" {
		t.Fatalf("inline upstream = %+v", upstream)
	}
	if upstream.Overlay[0] != "ov.kcp-local-config" {
		t.Fatalf("overlay refs = %v", upstream.Overlay)
	}
	if kine := document.Node("sc.kine-local"); kine == nil || kine.Parent != "sc.kcp-local" {
		t.Fatalf("nested upstream = %+v", kine)
	}

	if len(document.Section("upstreams").Roots()) != 12 {
		t.Fatalf("upstreams = %d", len(document.Section("upstreams").Roots()))
	}
	if len(document.Section("orchestrators").Roots()) != 15 {
		t.Fatalf("orchestrators = %d", len(document.Section("orchestrators").Roots()))
	}
	if len(document.Section("types").Roots()) != 6 {
		t.Fatalf("types = %d", len(document.Section("types").Roots()))
	}
	upstreamNode := document.Node("up.kcp")
	if upstreamNode == nil || upstreamNode.Section != "upstreams" || upstreamNode.Form != FormMap {
		t.Fatalf("up.kcp = %+v", upstreamNode)
	}
	if document.Node("tb.kcp-admin") == nil {
		t.Fatal("the threat model tree must be flattened")
	}
}

func TestParseKeepsTheOlderLayouts(t *testing.T) {
	document, err := Parse(testdata(t, "arch-85799e6.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"upstreams", "orchestrators", "system_contexts", "overlays", "trust_boundaries", "crossings", "flows"} {
		section := document.Section(key)
		if section == nil {
			t.Fatalf("section %s is missing", key)
		}
		if section.Form != FormList {
			t.Fatalf("section %s form = %q, want list", key, section.Form)
		}
	}
	if _, ok := document.Header["conventions"]; !ok {
		t.Fatal("conventions must stay in the header")
	}
	if len(document.Nodes()) < 80 {
		t.Fatalf("nodes = %d, want the whole document", len(document.Nodes()))
	}
}

func TestCodePaths(t *testing.T) {
	document, err := Parse(testdata(t, "arch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	permissions := document.Node("type.DenoPermissions")
	if permissions == nil {
		t.Fatal("type.DenoPermissions is missing")
	}
	want := []string{"api/v1alpha1/types_shared.go"}
	if !reflect.DeepEqual(permissions.Code, want) {
		t.Fatalf("code = %v, want %v", permissions.Code, want)
	}

	cases := []struct {
		name  string
		value any
		want  []string
	}{
		{"a symbol after a colon", "deploy/start-kcp.sh:KCP_BIN", []string{"deploy/start-kcp.sh"}},
		{"a mapping of paths", map[string]any{"type": "api/v1alpha1/types_shared.go", "default": "internal/x/y.go:Z"}, []string{"api/v1alpha1/types_shared.go", "internal/x/y.go"}},
		{"a command is not a path", "gofmt -l $(git ls-files '*.go')", nil},
		{"a bare word is not a path", "nohup", nil},
	}
	for _, entry := range cases {
		if got := codePaths(entry.value); !reflect.DeepEqual(got, entry.want) {
			t.Errorf("%s: codePaths = %v, want %v", entry.name, got, entry.want)
		}
	}
}

func TestMarshalRoundTripIsLossless(t *testing.T) {
	for _, name := range versions(t) {
		t.Run(name, func(t *testing.T) {
			original := testdata(t, name)
			document, err := Parse(original)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			reparsed, err := Parse(encoded)
			if err != nil {
				t.Fatalf("reparse: %v\n%s", err, encoded[:min(len(encoded), 2000)])
			}
			compare(t, name, document, reparsed)
			again, err := Marshal(reparsed)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(encoded) {
				t.Fatalf("%s: the second export differs from the first", name)
			}
		})
	}
}

func compare(t *testing.T, name string, left, right *Document) {
	t.Helper()
	if differences := Diff(left, right); len(differences) > 0 {
		t.Fatalf("%s: %s", name, strings.Join(differences, "\n"))
	}
	if len(left.Nodes()) != len(right.Nodes()) {
		t.Fatalf("%s: nodes = %d, want %d", name, len(right.Nodes()), len(left.Nodes()))
	}
}
