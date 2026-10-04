package mirror

import (
	"errors"
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const LegacyDir = ".specs"

var ErrConflict = errors.New("mirror: kcp and the file both changed since the last sync")

type Metadata struct {
	Name string `json:"name"`

	Namespace string `json:"namespace,omitempty"`
}

type Document struct {
	APIVersion string `json:"apiVersion"`

	Kind string `json:"kind"`

	Metadata Metadata `json:"metadata"`

	Spec spec.SystemContextSpec `json:"spec"`
}

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
