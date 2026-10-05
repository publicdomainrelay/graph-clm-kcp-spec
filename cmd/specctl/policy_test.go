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
	raw, err := policyeval.LoadRaw(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if raw.Manifest.Repository != "deno-kcp" {
		t.Errorf("repository: got %q, want deno-kcp", raw.Manifest.Repository)
	}
	// The seed carries the directory's own templates; the imported pack's five
	// are the pack's and resolve at build, not here.
	if len(raw.Templates) != 8 {
		t.Errorf("templates: got %d, want 8", len(raw.Templates))
	}
	if len(raw.Manifest.Imports) != 1 || raw.Manifest.Imports[0].Pack != "rfp-guest-isolation" {
		t.Errorf("the imports did not survive the seed: %+v", raw.Manifest.Imports)
	}
	for _, want := range []string{
		policy.LockPath,
		policy.TemplateSourcePath("relay-only-ssh"),
		policy.TemplateTestPath("security-disabled-verification"),
		policy.SuitePath("relay-only-ssh"),
		policy.DistPath("relay-only-ssh"),
		policy.CataloguePath,
		policy.LibPath,
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, policy.DistPath("rfp-host-reach-in"))); !os.IsNotExist(err) {
		t.Errorf("the pack's dist was rendered before the build: %v", err)
	}
	// The build resolves the import the seed kept: the library grows to the
	// eight own templates plus the pack's five, and the pack's dist appears.
	code, _, stderr = runWith("policy", "build", "--dir", dir)
	if code != exitOK {
		t.Fatalf("build: code %d, stderr %q", code, stderr)
	}
	library, err := policyeval.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Templates) != 13 {
		t.Errorf("templates after build: got %d, want 13", len(library.Templates))
	}
	if library.ImportedFrom("rfp-host-reach-in") == "" {
		t.Errorf("the pack's template is not marked imported: %v", library.Imported)
	}
	if _, err := os.Stat(filepath.Join(dir, policy.DistPath("rfp-host-reach-in"))); err != nil {
		t.Errorf("the pack's dist after build: %v", err)
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

func TestPolicyInitEnforcementWritesTheManifestOverride(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runWith("policy", "init", "--dir", dir, "--with-library",
		"--enforcement", "*=warn security-disabled-verification=deny")
	if code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	library, err := policyeval.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]policy.Enforcement{
		"security-disabled-verification": policy.EnforcementDeny,
		"provisioning-container-in-test": policy.EnforcementWarn,
	} {
		constraint, ok := library.Constraint(name)
		if !ok {
			t.Fatalf("no constraint %s", name)
		}
		if constraint.Enforcement != want {
			t.Errorf("%s: got %q, want %q", name, constraint.Enforcement, want)
		}
	}
	if code, _, _ := runWith("policy", "init", "--dir", t.TempDir(), "--with-library", "--enforcement", "nonsense"); code != exitUsage {
		t.Errorf("a bad enforcement spec: code %d, want %d", code, exitUsage)
	}
	// The build path reads the library raw and resolves it itself, so it must
	// apply the rules too.
	if code, _, stderr := runWith("policy", "build", "--dir", dir); code != exitOK {
		t.Fatalf("build: code %d, stderr %q", code, stderr)
	}
	catalogue, err := os.ReadFile(filepath.Join(dir, policy.CataloguePath))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"security-disabled-verification (deny)",
		"provisioning-container-in-test (warn)",
	} {
		if !strings.Contains(string(catalogue), want) {
			t.Errorf("the catalogue does not carry %q:\n%s", want, catalogue)
		}
	}
}

func TestPolicyTestFindsGatorOnPath(t *testing.T) {
	stubDir := t.TempDir()
	log := filepath.Join(stubDir, "gator.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(stubDir, "gator"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir)
	t.Setenv("SPECD_GATOR", "")

	library := filepath.Join("..", "..", "examples", "policies", "market-mini")
	code, _, stderr := runWith("policy", "test", "--dir", library, "--gator")
	if code != exitOK {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the gator on PATH was not run: %v", err)
	}
	if !strings.HasPrefix(string(raw), "verify ") {
		t.Errorf("gator was run as %q, want verify <suites>", raw)
	}
}

func TestPolicyTestNamesWhereItLookedForGator(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	t.Setenv("SPECD_GATOR", "")

	library := filepath.Join("..", "..", "examples", "policies", "market-mini")
	code, _, stderr := runWith("policy", "test", "--dir", library, "--gator")
	if code != exitError {
		t.Fatalf("code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "gator not found in") || !strings.Contains(stderr, "PATH") {
		t.Errorf("stderr does not name the search: %q", stderr)
	}
}
