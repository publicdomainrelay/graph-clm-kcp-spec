package policyeval

import (
	"io/fs"
	"os"
	"path"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

// Load reads a policy tree and resolves the packs it imports. A directory with
// a policies.yaml is a repository's library; a directory with a pack.yaml is a
// pack.
func Load(dir string) (policy.Library, error) {
	return LoadFS(os.DirFS(dir))
}

func LoadFS(fsys fs.FS) (policy.Library, error) {
	library, err := LoadRaw(fsys)
	if err != nil {
		return policy.Library{}, err
	}
	if len(library.Manifest.Imports) == 0 {
		return library, nil
	}
	resolved, _, err := ResolveImports(library, ImportOptions{})
	if err != nil {
		return policy.Library{}, err
	}
	return resolved, nil
}

// LoadRaw reads a policy tree without resolving its imports.
func LoadRaw(fsys fs.FS) (policy.Library, error) {
	if data, err := fs.ReadFile(fsys, policy.PoliciesPath); err == nil {
		var manifest policy.PolicyLibrary
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return policy.Library{}, err
		}
		return loadTree(fsys, manifest, policy.PoliciesPath, data)
	}
	if data, err := fs.ReadFile(fsys, policy.PackManifestPath); err == nil {
		var manifest policy.PackManifest
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return policy.Library{}, err
		}
		library, err := loadTree(fsys, policy.PolicyLibrary{
			Repository: manifest.Name,
			Version:    manifest.Version,
		}, policy.PackManifestPath, data)
		if err != nil {
			return policy.Library{}, err
		}
		library.Pack = &manifest
		return library, nil
	}
	// A tree with neither manifest still loads: its templates, constraints and
	// library are the pack's own, with no repository and no binding.
	return loadTree(fsys, policy.PolicyLibrary{}, "", nil)
}

func loadTree(fsys fs.FS, manifest policy.PolicyLibrary, manifestPath string, manifestData []byte) (policy.Library, error) {
	library := policy.Library{Manifest: manifest, Files: map[string][]byte{}}
	if manifestPath != "" {
		library.Files[manifestPath] = manifestData
	}

	if data, err := fs.ReadFile(fsys, policy.LibPath); err == nil {
		library.Lib = string(data)
	}

	entries, err := fs.ReadDir(fsys, policy.TemplatesDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			template, err := loadTemplate(fsys, entry.Name())
			if err != nil {
				return policy.Library{}, err
			}
			library.Templates = append(library.Templates, template)
		}
	}

	entries, err = fs.ReadDir(fsys, policy.ConstraintsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			file := path.Join(policy.ConstraintsDir, entry.Name())
			data, err := fs.ReadFile(fsys, file)
			if err != nil {
				return policy.Library{}, err
			}
			library.Files[file] = data
			constraint, err := policy.ParseConstraint(data, "")
			if err != nil {
				return policy.Library{}, err
			}
			constraint.Path = file
			if template, ok := library.TemplateOfKind(constraint.Kind); ok {
				constraint.Template = template.Name
			}
			library.Constraints = append(library.Constraints, constraint)
		}
	}

	library.Sort()
	return library, nil
}

func loadTemplate(fsys fs.FS, name string) (policy.Template, error) {
	headerPath := policy.TemplateHeaderPath(name)
	header, err := fs.ReadFile(fsys, headerPath)
	if err != nil {
		return policy.Template{}, err
	}
	rego, err := fs.ReadFile(fsys, policy.TemplateSourcePath(name))
	if err != nil {
		rego = nil
	}
	template, err := policy.ParseTemplate(header, rego)
	if err != nil {
		return policy.Template{}, err
	}
	template.Slug = name
	if template.Name == "" {
		template.Name = name
	}
	template.Path = policy.TemplateDir(name)
	return template, nil
}

func TemplateTest(fsys fs.FS, name string) ([]byte, bool) {
	data, err := fs.ReadFile(fsys, policy.TemplateTestPath(name))
	if err != nil {
		return nil, false
	}
	return data, true
}

func SplitDocuments(data []byte) [][]byte {
	out := [][]byte{}
	for _, doc := range strings.Split(string(data), "\n---") {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" || trimmed == "---" {
			continue
		}
		out = append(out, []byte(trimmed))
	}
	return out
}
