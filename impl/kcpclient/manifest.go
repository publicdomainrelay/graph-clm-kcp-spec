package kcpclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

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
		return decode(object, func() any { return &spec.Repository{} })
	case specapi.SystemContextKind:
		return decode(object, func() any { return &spec.SystemContext{} })
	case specapi.SpecChangeKind:
		return decode(object, func() any { return &spec.SpecChange{} })
	}
	return nil, fmt.Errorf("kcpclient: kind %q is not a spec object", object.GetKind())
}

func decode(object *unstructured.Unstructured, factory func() any) (any, error) {
	out := factory()
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, out); err != nil {
		return nil, fmt.Errorf("kcpclient: decode %s %s: %s: %w", object.GetKind(), object.GetName(), fieldPath(object, factory), err)
	}
	return out, nil
}

func fieldPath(object *unstructured.Unstructured, factory func() any) string {
	encoded, err := json.Marshal(object.Object)
	if err != nil {
		return "spec"
	}
	if err := json.Unmarshal(encoded, factory()); err != nil {
		if match := jsonPathPattern.FindStringSubmatch(err.Error()); len(match) == 2 {
			return match[1]
		}
	}
	return "spec"
}

var jsonPathPattern = regexp.MustCompile(`Go struct field [A-Za-z0-9_]+\.([A-Za-z0-9_.\[\]]+) of type`)

func Unstructured(value any) (*unstructured.Unstructured, error) {
	converted, err := runtime.DefaultUnstructuredConverter.ToUnstructured(value)
	if err != nil {
		return nil, fmt.Errorf("kcpclient: encode object: %w", err)
	}
	return &unstructured.Unstructured{Object: converted}, nil
}
