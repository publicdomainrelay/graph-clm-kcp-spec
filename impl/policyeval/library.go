package policyeval

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func Load(dir string) (policy.Library, error) {
	return LoadFS(os.DirFS(dir))
}

func LoadFS(fsys fs.FS) (policy.Library, error) {
	library, err := LoadRaw(fsys)
	if err != nil {
		return policy.Library{}, err
	}
	if len(library.Manifest.Imports) == 0 {
		library.ApplyEnforcement()
		return library, nil
	}
	resolved, _, err := ResolveImports(library, ImportOptions{})
	if err != nil {
		return policy.Library{}, err
	}
	return resolved, nil
}

func LoadRaw(fsys fs.FS) (policy.Library, error) {
	packData, packErr := fs.ReadFile(fsys, policy.PackManifestPath)
	policiesData, policiesErr := fs.ReadFile(fsys, policy.PoliciesPath)
	if packErr == nil {
		var pack policy.PackManifest
		if err := yaml.Unmarshal(packData, &pack); err != nil {
			return policy.Library{}, err
		}
		manifest := policy.PolicyLibrary{Repository: pack.Name, Version: pack.Version}
		if policiesErr == nil {
			if err := yaml.Unmarshal(policiesData, &manifest); err != nil {
				return policy.Library{}, err
			}
			if manifest.Repository == "" {
				manifest.Repository = pack.Name
			}
		}
		library, err := loadTree(fsys, manifest, policy.PackManifestPath, packData)
		if err != nil {
			return policy.Library{}, err
		}
		if policiesErr == nil {
			library.Files[policy.PoliciesPath] = policiesData
		}
		library.Pack = &pack
		return library, nil
	}
	if policiesErr == nil {
		var manifest policy.PolicyLibrary
		if err := yaml.Unmarshal(policiesData, &manifest); err != nil {
			return policy.Library{}, err
		}
		return loadTree(fsys, manifest, policy.PoliciesPath, policiesData)
	}
	return loadTree(fsys, policy.PolicyLibrary{}, "", nil)
}

func loadTree(fsys fs.FS, manifest policy.PolicyLibrary, manifestPath string, manifestData []byte) (policy.Library, error) {
	for _, rule := range manifest.Enforcement {
		if strings.TrimSpace(rule.Name) == "" {
			return policy.Library{}, fmt.Errorf("policyeval: an enforcement rule names no constraint")
		}
		if !rule.Action.Known() {
			return policy.Library{}, fmt.Errorf("policyeval: the enforcement rule %s asks for %q, which is not deny, warn or dryrun", rule.Name, rule.Action)
		}
	}
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

	entries, err = fs.ReadDir(fsys, policy.ClassifiersDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			file := path.Join(policy.ClassifiersDir, entry.Name())
			data, err := fs.ReadFile(fsys, file)
			if err != nil {
				return policy.Library{}, err
			}
			library.Files[file] = data
		}
	}

	entries, err = fs.ReadDir(fsys, policy.ExceptionsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			file := path.Join(policy.ExceptionsDir, entry.Name())
			data, err := fs.ReadFile(fsys, file)
			if err != nil {
				return policy.Library{}, err
			}
			var exception policy.Exception
			if err := yaml.UnmarshalStrict(data, &exception); err != nil {
				return policy.Library{}, fmt.Errorf("policyeval: %s: %w", file, err)
			}
			if strings.TrimSpace(exception.Constraint) == "" {
				return policy.Library{}, fmt.Errorf("policyeval: %s names no constraint", file)
			}
			if strings.TrimSpace(exception.Reason) == "" {
				return policy.Library{}, fmt.Errorf("policyeval: %s records no reason", file)
			}
			switch exception.ScopeOf() {
			case policy.ExceptionScopeSite:
				if strings.TrimSpace(exception.Key) == "" {
					return policy.Library{}, fmt.Errorf("policyeval: %s names no key; a waiver is site-scoped by its key, or carries scope: rule to waive the whole constraint", file)
				}
			case policy.ExceptionScopeRule:
				if strings.TrimSpace(exception.Key) != "" {
					return policy.Library{}, fmt.Errorf("policyeval: %s carries scope: rule and a key; a rule-scoped waiver names no site", file)
				}
			default:
				return policy.Library{}, fmt.Errorf("policyeval: %s has scope %q, which is not %s or %s", file, exception.Scope, policy.ExceptionScopeSite, policy.ExceptionScopeRule)
			}
			library.Files[file] = data
			library.Manifest.Exceptions = append(library.Manifest.Exceptions, exception)
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
