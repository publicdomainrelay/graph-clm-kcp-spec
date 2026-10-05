package policyeval

import (
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
	library := policy.Library{Files: map[string][]byte{}}

	if data, err := fs.ReadFile(fsys, policy.PoliciesPath); err == nil {
		library.Files[policy.PoliciesPath] = data
		if err := yaml.Unmarshal(data, &library.Manifest); err != nil {
			return policy.Library{}, err
		}
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
