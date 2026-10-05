package policy

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func Unstructured(data []byte) (*unstructured.Unstructured, error) {
	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return nil, fmt.Errorf("policy: empty object")
	}
	return &unstructured.Unstructured{Object: decoded}, nil
}
