package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

func TestPolicyInitWithLibraryCopiesTheEmbeddedTemplates(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runWith("policy", "init", "--dir", dir, "--with-library")
	if code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "library:") {
		t.Errorf("stdout does not mention the library: %q", stdout)
	}
	for _, want := range []string{
		policy.PoliciesPath,
		policy.LibPath,
		policy.LibTestPath,
		policy.TemplateHeaderPath("security-disabled-verification"),
		policy.TemplateSourcePath("provisioning-new-guest-transport"),
		policy.ConstraintPath("requirement-text-has-machine-path"),
		policy.SuitePath("change-succeeded-with-failed-acceptance"),
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, policy.TemplatesDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 7 {
		t.Errorf("templates: got %d, want 7", len(entries))
	}
}

func TestPolicyInitFromADirectorySeedsAndRebuilds(t *testing.T) {
	source := filepath.Join("..", "..", "examples", "policies", "deno-kcp")
	dir := t.TempDir()
	code, stdout, stderr := runWith("policy", "init", "--dir", dir, "--repo", "deno-kcp", "--from", source)
	if code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "seeded from") {
		t.Errorf("stdout does not mention the seed: %q", stdout)
	}
	library, err := policyeval.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if library.Manifest.Repository != "deno-kcp" {
		t.Errorf("repository: got %q, want deno-kcp", library.Manifest.Repository)
	}
	if len(library.Templates) != 8 {
		t.Errorf("templates: got %d, want 8", len(library.Templates))
	}
	for _, want := range []string{
		policy.TemplateSourcePath("relay-only-ssh"),
		policy.TemplateTestPath("security-disabled-verification"),
		policy.SuitePath("relay-only-ssh") + "",
		policy.DistPath("relay-only-ssh"),
		policy.CataloguePath,
		policy.LibPath,
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, policy.DistDir, "nosuchtemplate.yaml")); !os.IsNotExist(err) {
		t.Errorf("the dist of another library was copied: %v", err)
	}
}

func TestPolicyInitWithoutLibraryCopiesNoTemplates(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runWith("policy", "init", "--dir", dir)
	if code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, policy.TemplatesDir)); !os.IsNotExist(err) {
		t.Errorf("templates/ exists without --with-library: %v", err)
	}
}
