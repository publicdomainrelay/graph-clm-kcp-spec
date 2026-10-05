package schemagen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

const Revision = 6

func Name(crd map[string]any) (string, error) {
	version, err := firstVersion(crd)
	if err != nil {
		return "", err
	}
	spec := specOf(crd)
	group, _ := spec["group"].(string)
	names, _ := spec["names"].(map[string]any)
	plural, _ := names["plural"].(string)
	name, _ := version["name"].(string)
	if group == "" || plural == "" || name == "" {
		return "", fmt.Errorf("schemagen: the CRD has no group, plural or version")
	}
	return fmt.Sprintf("%s-%d.%s.%s", name, Revision, plural, group), nil
}

func FromCRD(crd map[string]any) (map[string]any, error) {
	spec := specOf(crd)
	if len(spec) == 0 {
		return nil, fmt.Errorf("schemagen: the manifest is not a CustomResourceDefinition")
	}
	name, err := Name(crd)
	if err != nil {
		return nil, err
	}
	group, _ := spec["group"].(string)
	names, _ := spec["names"].(map[string]any)
	scope, _ := spec["scope"].(string)
	if group == "" || len(names) == 0 || scope == "" {
		return nil, fmt.Errorf("schemagen: %s has no group, names or scope", name)
	}
	versions, _ := spec["versions"].([]any)
	if len(versions) == 0 {
		return nil, fmt.Errorf("schemagen: %s has no versions", name)
	}
	out := make([]any, 0, len(versions))
	for i, raw := range versions {
		version, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schemagen: %s version %d is not a mapping", name, i)
		}
		converted, err := convertVersion(name, version)
		if err != nil {
			return nil, err
		}
		out = append(out, converted)
	}
	return map[string]any{
		"apiVersion": "apis.kcp.io/v1alpha1",
		"kind":       "APIResourceSchema",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"group":    group,
			"names":    names,
			"scope":    scope,
			"versions": out,
		},
	}, nil
}

func convertVersion(schema string, version map[string]any) (map[string]any, error) {
	crdSchema, _ := version["schema"].(map[string]any)
	openAPI, _ := crdSchema["openAPIV3Schema"].(map[string]any)
	if len(openAPI) == 0 {
		return nil, fmt.Errorf("schemagen: %s has a version without an openAPIV3Schema", schema)
	}
	out := map[string]any{
		"name":    version["name"],
		"served":  version["served"],
		"storage": version["storage"],
		"schema":  openAPI,
	}
	for _, key := range []string{"subresources", "additionalPrinterColumns", "deprecated", "deprecationWarning"} {
		if value, ok := version[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func specOf(crd map[string]any) map[string]any {
	kind, _ := crd["kind"].(string)
	if kind != "CustomResourceDefinition" {
		return nil
	}
	spec, _ := crd["spec"].(map[string]any)
	return spec
}

func firstVersion(crd map[string]any) (map[string]any, error) {
	spec := specOf(crd)
	versions, _ := spec["versions"].([]any)
	if len(versions) == 0 {
		return nil, fmt.Errorf("schemagen: the CRD has no versions")
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schemagen: the CRD's first version is not a mapping")
	}
	return version, nil
}

func Read(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("schemagen: read %s: %w", path, err)
	}
	return out, nil
}

func Marshal(object map[string]any) ([]byte, error) {
	data, err := yaml.Marshal(object)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func FileName(crdFile string) string {
	return strings.TrimSuffix(filepath.Base(crdFile), ".yaml") + ".yaml"
}

func CRDFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}
