package kcpclient

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const multiDocument = `
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Repository
metadata: {name: calc, namespace: default}
spec: {path: examples/calc}
---
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SystemContext
metadata: {name: calc, namespace: default}
spec:
  repository: calc
  requirements:
    - {id: r.add, level: MUST, text: add two integers}
`

func TestDecodeSplitsDocuments(t *testing.T) {
	objects, err := Decode([]byte(multiDocument))
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 {
		t.Fatalf("objects = %d", len(objects))
	}
	if objects[0].GetKind() != specapi.RepositoryKind || objects[1].GetKind() != specapi.SystemContextKind {
		t.Fatalf("kinds = %s, %s", objects[0].GetKind(), objects[1].GetKind())
	}
}

func TestDecodeRejectsUnknownKindsAndEmptyInput(t *testing.T) {
	if _, err := Decode([]byte("apiVersion: v1\nkind: Widget\nmetadata: {name: w}\n")); err == nil {
		t.Fatal("an unknown kind must not decode")
	}
	if _, err := Decode([]byte("apiVersion: v1\nmetadata: {name: w}\n")); err == nil {
		t.Fatal("a manifest with no kind must not decode")
	}
	if _, err := Decode([]byte("# nothing\n")); err == nil {
		t.Fatal("an empty manifest must not decode")
	}
}

func TestTypedAndUnstructuredRoundTrip(t *testing.T) {
	objects, err := Decode([]byte(multiDocument))
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		typed, err := Typed(object)
		if err != nil {
			t.Fatal(err)
		}
		spec.SetDefaults(typed)
		if result := spec.ValidateAny(typed); !result.OK() {
			t.Fatalf("%s is invalid: %v", object.GetKind(), result.Err())
		}
		back, err := Unstructured(typed)
		if err != nil {
			t.Fatal(err)
		}
		if back.GetKind() != object.GetKind() || back.GetName() != object.GetName() {
			t.Fatalf("round trip = %s/%s, want %s/%s", back.GetKind(), back.GetName(), object.GetKind(), object.GetName())
		}
		if back.GetKind() == specapi.RepositoryKind {
			repository, ok := back.Object["spec"].(map[string]any)
			if !ok || repository["path"] != "examples/calc" {
				t.Fatalf("spec = %+v", back.Object["spec"])
			}
		}
		if back.GetKind() == specapi.SystemContextKind && back.GetNamespace() != "default" {
			t.Fatalf("namespace = %q", back.GetNamespace())
		}
	}
}

func TestTypedReportsTheOffendingFieldPath(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": specapi.APIVersion,
		"kind":       specapi.SystemContextKind,
		"metadata":   map[string]any{"name": "calc"},
		"spec": map[string]any{
			"repository": "calc",
			"requirements": []any{map[string]any{
				"id": "r.add", "level": "MUST", "text": "add", "codeRefs": "not-a-list",
			}},
		},
	}}
	_, err := Typed(object)
	if err == nil {
		t.Fatal("a wrong field type must not decode")
	}
	if !strings.Contains(err.Error(), "spec.requirements.codeRefs") {
		t.Fatalf("the error must name the field path: %v", err)
	}
}

func TestTypedRejectsANonSpecKind(t *testing.T) {
	objects, err := Decode([]byte(multiDocument))
	if err != nil {
		t.Fatal(err)
	}
	objects[0].SetKind("Deployment")
	if _, err := Typed(objects[0]); err == nil {
		t.Fatal("a non spec kind must not convert")
	}
}

func TestEncodeProducesReadableYAML(t *testing.T) {
	objects, err := Decode([]byte(multiDocument))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(objects[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "kind: Repository") || !strings.Contains(string(encoded), "path: examples/calc") {
		t.Fatalf("yaml = %s", encoded)
	}
}

func TestApplySendsTheDecodedObjects(t *testing.T) {
	client, requests := newClient(t, func(w http.ResponseWriter, _ *http.Request, index int) {
		if index%2 == 0 {
			writeJSON(w, 404, map[string]any{"kind": "Status", "reason": "NotFound", "code": 404})
			return
		}
		writeJSON(w, 201, map[string]any{
			"apiVersion": specapi.APIVersion,
			"kind":       specapi.RepositoryKind,
			"metadata":   map[string]any{"name": "calc", "namespace": "default"},
		})
	})
	objects, err := Decode([]byte(multiDocument))
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if _, err := client.Apply(context.Background(), object); err != nil {
			t.Fatal(err)
		}
	}
	if len(*requests) != 4 {
		t.Fatalf("requests = %d", len(*requests))
	}
	if !strings.Contains((*requests)[1].Path, "/repositories") || !strings.Contains((*requests)[3].Path, "/systemcontexts") {
		t.Fatalf("paths = %s, %s", (*requests)[1].Path, (*requests)[3].Path)
	}
}
