package yamlx

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

func TestMarshalSortsKeysTheSameWayEveryTime(t *testing.T) {
	value := map[string]any{}
	refs := map[string]string{}
	for index := 0; index < 40; index++ {
		digest := sha1.Sum([]byte(fmt.Sprintf("symbol-%d", index)))
		refs[fmt.Sprintf("function:%s", hex.EncodeToString(digest[:]))] = fmt.Sprintf("Sym%d@f.go:%d", index, index)
	}
	labels := map[string]string{}
	for index := 0; index < 20; index++ {
		labels[fmt.Sprintf("app.kubernetes.io/part-%d", index)] = "v"
	}
	nodes := map[string]any{}
	for index := 0; index < 20; index++ {
		nodes[fmt.Sprintf("k:%08x", index*2654435761)] = index
	}
	value["refs"] = refs
	value["metadata"] = map[string]any{"labels": labels}
	value["arch"] = map[string]any{"node": nodes}

	first, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 50; attempt++ {
		next, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(next) != string(first) {
			t.Fatalf("two renders of one state differ:\n%s\n---\n%s", first, next)
		}
	}
	if _, err := sigsyaml.Marshal(value); err != nil {
		t.Fatal(err)
	}
}

func TestMarshalMatchesSigsYamlForKeyOrderThatIsStable(t *testing.T) {
	value := map[string]any{
		"apiVersion": "v1",
		"metadata":   map[string]any{"name": "calc", "labels": map[string]string{"app": "calc", "tier": "1"}},
		"spec": []any{
			map[string]any{"id": "r.add", "level": "MUST", "codeRefs": []any{"function:Add"}},
			map[string]any{"id": "r.mul", "level": "SHOULD"},
		},
		"count": 3,
		"ratio": 1.5,
		"blank": "",
		"flags": []any{true, false},
	}
	ours, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := sigsyaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(ours) != string(theirs) {
		t.Fatalf("yamlx differs from sigs.k8s.io/yaml:\n--- yamlx\n%s\n--- sigs\n%s", ours, theirs)
	}
}

func TestMarshalRendersAnEmptyObjectAndArray(t *testing.T) {
	value := map[string]any{"empty": map[string]any{}, "list": []any{}}
	got, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	want := "empty: {}\nlist: []\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
