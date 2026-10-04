package schemagen

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

func generate(t *testing.T, name string) []byte {
	t.Helper()
	crd, err := Read(filepath.Join("..", "..", "deploy", "crds", name))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := FromCRD(crd)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEveryCRDHasItsAPIResourceSchema(t *testing.T) {
	crdDir := filepath.Join("..", "..", "deploy", "crds")
	outDir := filepath.Join("..", "..", "deploy", "apiresourceschemas")
	files, err := CRDFiles(crdDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no CRDs under deploy/crds")
	}
	for _, file := range files {
		name := FileName(file)
		got := generate(t, name)
		path := filepath.Join(outDir, name)
		if os.Getenv("SPECD_UPDATE_GOLDEN") == "1" {
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (regenerate with SPECD_UPDATE_GOLDEN=1 go test ./impl/schemagen)", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("deploy/apiresourceschemas/%s is stale; regenerate with SPECD_UPDATE_GOLDEN=1 go test ./impl/schemagen", name)
		}
	}
}

func TestTheProviderExportPublishesEverySchema(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "specs-apiexport.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exported := map[string]map[string]any{}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		object := map[string]any{}
		if err := decoder.Decode(&object); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("%s: %v", path, err)
		}
		if object["kind"] != "APIExport" {
			continue
		}
		spec, _ := object["spec"].(map[string]any)
		resources, _ := spec["resources"].([]any)
		for _, raw := range resources {
			resource, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := resource["name"].(string)
			exported[name] = resource
		}
	}
	if len(exported) == 0 {
		t.Fatalf("%s has no APIExport resources", path)
	}
	files, err := CRDFiles(filepath.Join("..", "..", "deploy", "crds"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		crd, err := Read(filepath.Join("..", "..", "deploy", "crds", file))
		if err != nil {
			t.Fatal(err)
		}
		name, err := Name(crd)
		if err != nil {
			t.Fatal(err)
		}
		spec := specOf(crd)
		group, _ := spec["group"].(string)
		names, _ := spec["names"].(map[string]any)
		plural, _ := names["plural"].(string)
		resource, ok := exported[plural]
		if !ok {
			t.Errorf("%s does not export the %s resource", path, plural)
			continue
		}
		if got, _ := resource["schema"].(string); got != name {
			t.Errorf("the export names schema %q for %s, want %q", got, plural, name)
		}
		if got, _ := resource["group"].(string); got != group {
			t.Errorf("the export names group %q for %s, want %q", got, plural, group)
		}
	}
}

func TestFromCRDRejectsSomethingElse(t *testing.T) {
	if _, err := FromCRD(map[string]any{"kind": "ConfigMap"}); err == nil {
		t.Fatal("a manifest that is not a CRD was accepted")
	}
	crd, err := Read(filepath.Join("..", "..", "deploy", "crds", "specs.publicdomainrelay.dev_repositories.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	delete(crd["spec"].(map[string]any), "versions")
	if _, err := FromCRD(crd); err == nil {
		t.Fatal("a CRD without versions was accepted")
	}
}

func TestName(t *testing.T) {
	crd, err := Read(filepath.Join("..", "..", "deploy", "crds", "specs.publicdomainrelay.dev_systemcontexts.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	name, err := Name(crd)
	if err != nil {
		t.Fatal(err)
	}
	if want := "v1alpha1-4.systemcontexts.specs.publicdomainrelay.dev"; name != want {
		t.Fatalf("name = %q, want %q", name, want)
	}
}
