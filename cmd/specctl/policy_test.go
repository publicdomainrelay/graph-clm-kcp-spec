package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
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

func TestPolicyGenerateRequiresAPrompt(t *testing.T) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	if code := runPolicyGenerate([]string{"--repo", "calc"}, stdout, stderr); code != exitUsage {
		t.Fatalf("a generate without a prompt returned %d", code)
	}
	if !strings.Contains(stderr.String(), "--prompt is required") {
		t.Errorf("the usage message reads %q", stderr.String())
	}
}

func TestPolicyGenerateRefusesAMalformedRequirement(t *testing.T) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	code := runPolicyGenerate([]string{"--repo", "calc", "--prompt", "a sentence", "--requirement", "no-id"}, stdout, stderr)
	if code != exitUsage {
		t.Fatalf("a malformed requirement returned %d", code)
	}
	if !strings.Contains(stderr.String(), "ctx#id") {
		t.Errorf("the usage message reads %q", stderr.String())
	}
}

func TestPolicyBindRequiresARepositoryAndAPack(t *testing.T) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	if code := runPolicyBind([]string{"--repo", "calc"}, stdout, stderr); code != exitUsage {
		t.Fatalf("a bind without a pack returned %d", code)
	}
	if !strings.Contains(stderr.String(), "--repo and --pack are required") {
		t.Errorf("the usage message reads %q", stderr.String())
	}
}

func TestDefaultSlugIsKebabAndBounded(t *testing.T) {
	slug := defaultSlug("", "integration tests with bidder and requester MUST always make ssh connections over the relay")
	if strings.Contains(slug, "_") || strings.Contains(slug, " ") || strings.Contains(slug, "must") {
		t.Errorf("the slug is %q", slug)
	}
	if len(slug) > 40 {
		t.Errorf("the slug is %d characters: %q", len(slug), slug)
	}
	if slug != defaultSlug("", "integration tests with bidder and requester MUST always make ssh connections over the relay") {
		t.Error("the slug is not deterministic")
	}
	if given := defaultSlug("relay-only-ssh", "anything"); given != "relay-only-ssh" {
		t.Errorf("an explicit slug became %q", given)
	}
}
