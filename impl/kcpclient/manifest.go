package kcpclient

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

func Decode(data []byte) ([]*unstructured.Unstructured, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	objects := []*unstructured.Unstructured{}
	for {
		raw := map[string]any{}
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("kcpclient: decode manifest: %w", err)
		}
		if len(raw) == 0 {
			continue
		}
		object := &unstructured.Unstructured{Object: raw}
		if object.GetKind() == "" {
			return nil, fmt.Errorf("kcpclient: manifest has no kind")
		}
		if _, err := specapi.GVRForKind(object.GetKind()); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("kcpclient: manifest holds no objects")
	}
	return objects, nil
}

func Encode(object *unstructured.Unstructured) ([]byte, error) {
	encoded, err := yaml.Marshal(object.Object)
	if err != nil {
		return nil, fmt.Errorf("kcpclient: encode %s: %w", object.GetName(), err)
	}
	return encoded, nil
}

func Typed(object *unstructured.Unstructured) (any, error) {
	switch object.GetKind() {
	case specapi.RepositoryKind:
		out := &spec.Repository{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, out); err != nil {
			return nil, fmt.Errorf("kcpclient: decode Repository %s: %w", object.GetName(), err)
		}
		return out, nil
	case specapi.SystemContextKind:
		out := &spec.SystemContext{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, out); err != nil {
			return nil, fmt.Errorf("kcpclient: decode SystemContext %s: %w", object.GetName(), err)
		}
		return out, nil
	case specapi.SpecChangeKind:
		out := &spec.SpecChange{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, out); err != nil {
			return nil, fmt.Errorf("kcpclient: decode SpecChange %s: %w", object.GetName(), err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("kcpclient: kind %q is not a spec object", object.GetKind())
}

func Unstructured(value any) (*unstructured.Unstructured, error) {
	converted, err := runtime.DefaultUnstructuredConverter.ToUnstructured(value)
	if err != nil {
		return nil, fmt.Errorf("kcpclient: encode object: %w", err)
	}
	return &unstructured.Unstructured{Object: converted}, nil
}
