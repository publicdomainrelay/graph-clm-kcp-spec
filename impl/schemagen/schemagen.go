// Package schemagen turns the CustomResourceDefinitions under deploy/crds into
// the APIResourceSchema objects a kcp APIExport publishes. The CRDs stay the one
// source of truth for the group: the single workspace mode applies them
// directly, and the multi workspace mode exports the schemas derived from them.
//
// A drift here would mean the API a tenant binds and the API the single
// workspace serves had grown apart, so a test regenerates every schema and
// fails when the checked-in copy differs.
package schemagen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Revision is the APIResourceSchema revision this generator writes. kcp names a
// schema <version>-<revision>.<plural>.<group>, and an APIResourceSchema is
// immutable, so a changed CRD that must not break a bound consumer gets the
// next revision instead of an edit in place. deploy/specs-apiexport.yaml names
// the revision it publishes, and a test holds the two together.
//
// 1: the first published API.
// 2: the interface list's `name` descriptions say a method is keyed by its
//
//	receiver, which is what the wire format already meant.
const Revision = 2

// Name is the APIResourceSchema name for a CustomResourceDefinition.
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

// FromCRD converts one CustomResourceDefinition into one APIResourceSchema.
// Only the CRD spec crosses over: the version's openAPIV3Schema becomes the
// schema of the APIResourceSchema version, and the subresources and printer
// columns are carried so a tenant that binds the export sees the same API the
// single workspace serves.
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

// Read loads one CRD or APIResourceSchema manifest.
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

// Marshal writes a manifest the way the checked-in files are written: the JSON
// key order sigs.k8s.io/yaml produces, which is stable across runs.
func Marshal(object map[string]any) ([]byte, error) {
	data, err := yaml.Marshal(object)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// FileName is the deploy/apiresourceschemas file name for a CRD file name.
func FileName(crdFile string) string {
	return strings.TrimSuffix(filepath.Base(crdFile), ".yaml") + ".yaml"
}

// CRDFiles lists the CRDs the schemas are generated from, in name order.
func CRDFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}
