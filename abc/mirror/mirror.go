// Package mirror is the pure half of the git-native spec mirror: one YAML
// document per SystemContext under `.specs/` in the managed repository, so a
// pull request carries the spec and the code together.
//
// The document is the spec and nothing else: no status, no conditions, no
// runtime metadata. Its key order comes from the Go struct field order, so the
// same spec always renders to the same bytes and a pull that changed nothing
// leaves the file alone.
//
// Sync semantics are decided here, with no I/O, so the conflict rule is a unit
// test: a file and kcp are only in conflict when both moved since the last sync
// recorded in the `synced-hash` annotation.
package mirror

import (
	"errors"
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// Dir is where the mirror lives inside the managed repository.
const Dir = ".specs"

// ErrConflict is both sides of one context having moved since the last sync.
var ErrConflict = errors.New("mirror: kcp and the file both changed since the last sync")

type Metadata struct {
	Name string `json:"name"`

	Namespace string `json:"namespace,omitempty"`
}

// Document is one `.specs/<name>.yaml`.
type Document struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata Metadata `json:"metadata"`

	Spec spec.SystemContextSpec `json:"spec"`
}

// Render writes one context's spec as the mirror document. The spec is
// canonicalized first, so the keyed lists come out in their key order and a
// reordered manifest does not change the file. The code refs ingest derived
// from the tree are left out: they are an observation, not a decision, and
// including them would rewrite every file on every commit and drown a pull
// request in spec churn.
func Render(name, namespace string, contextSpec spec.SystemContextSpec) ([]byte, error) {
	declared := spec.Canonicalize(contextSpec)
	declared.CodeRefs = nil
	document := Document{
		APIVersion: specapi.Group + "/" + specapi.Version,
		Kind:       specapi.SystemContextKind,
		Metadata:   Metadata{Name: name, Namespace: namespace},
		Spec:       declared,
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("mirror: render %s: %w", name, err)
	}
	return data, nil
}

// Parse reads one mirror document and validates it as a spec a human could
// have applied to kcp.
func Parse(name string, data []byte) (spec.SystemContext, error) {
	document := Document{}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return spec.SystemContext{}, fmt.Errorf("mirror: parse %s: %w", name, err)
	}
	if document.Kind != "" && document.Kind != specapi.SystemContextKind {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s is a %s, want %s", name, document.Kind, specapi.SystemContextKind)
	}
	if document.Metadata.Name != "" && document.Metadata.Name != name {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s names the context %q", name, document.Metadata.Name)
	}
	if want := specapi.Group + "/" + specapi.Version; document.APIVersion != "" && document.APIVersion != want {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s has apiVersion %q, want %q", name, document.APIVersion, want)
	}
	out := spec.SystemContext{}
	out.Name = name
	out.Namespace = document.Metadata.Namespace
	out.Spec = document.Spec
	if result := spec.ValidateSystemContext(&out); !result.OK() {
		return spec.SystemContext{}, fmt.Errorf("mirror: %s does not validate: %v", name, result.Err())
	}
	return out, nil
}
