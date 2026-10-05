package library_test

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/policies/library"
)

const expectedTemplates = 7

func TestEmbeddedLibraryLoads(t *testing.T) {
	loaded, err := policyeval.LoadFS(library.FS())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Templates) != expectedTemplates {
		t.Fatalf("templates: got %d, want %d", len(loaded.Templates), expectedTemplates)
	}
	if len(loaded.Constraints) != expectedTemplates {
		t.Fatalf("constraints: got %d, want %d", len(loaded.Constraints), expectedTemplates)
	}
	for _, template := range loaded.Templates {
		slug := policy.TemplateSlug(template)
		if template.Kind == "" || template.Name != strings.ToLower(template.Kind) {
			t.Errorf("%s: template name %q is not the lower case of kind %q", slug, template.Name, template.Kind)
		}
		if len(loaded.ConstraintsFor(template)) == 0 {
			t.Errorf("%s: no constraint selects the template", slug)
		}
		if _, err := fs.Stat(library.FS(), path.Join(policy.TestsDir, slug, policy.SuiteName)); err != nil {
			t.Errorf("%s: no gator suite: %v", slug, err)
		}
	}
	if _, err := policyeval.Dist(loaded); err != nil {
		t.Errorf("dist: %v", err)
	}
}

func TestEveryTemplateDeclaresItsOrigin(t *testing.T) {
	loaded, err := policyeval.LoadFS(library.FS())
	if err != nil {
		t.Fatal(err)
	}
	for _, template := range loaded.Templates {
		if template.Annotations["specs.publicdomainrelay.dev/origin"] == "" {
			t.Errorf("%s: no origin annotation", policy.TemplateSlug(template))
		}
		if template.Annotations["specs.publicdomainrelay.dev/calibration"] == "" {
			t.Errorf("%s: no calibration annotation", policy.TemplateSlug(template))
		}
	}
}
