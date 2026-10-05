package main

import (
	"fmt"
	"io/fs"
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

// TestDefaultSlugIsKebabAndBounded pins the slug a generate derives when the
// operator names none: the prompt's first meaningful words, kebab-cased.
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

// TestProposeInteractionsDropsAnUnknownPeer pins plan 0010 D4: a flow whose
// target role is unknown names no context, so the proposal must drop it rather
// than emit `peer: unknown`.
func TestProposeInteractionsDropsAnUnknownPeer(t *testing.T) {
	model := policy.ArchitectureModel{
		Spec: policy.ArchitectureModelSpec{
			Components: []policy.ModelComponent{
				{Name: "lib-requester-xrpc", Context: "lib-requester-xrpc", Source: policy.SourceBoth},
			},
			Flows: []policy.ModelFlow{
				{
					From: "lib-requester-xrpc", To: policy.RoleUnknown,
					Purpose: "network-discovery", Source: policy.SourceObserved,
				},
				{
					From: "lib-requester-xrpc", To: "lib-market-bidder",
					Purpose: "relay", Source: policy.SourceObserved,
				},
			},
		},
	}
	output := proposeInteractions(model)
	if strings.Contains(output, "peer: "+policy.RoleUnknown) {
		t.Errorf("the proposal emits an unknown peer:\n%s", output)
	}
	if !strings.Contains(output, "peer: lib-market-bidder") {
		t.Errorf("the proposal drops the resolvable peer:\n%s", output)
	}
}

// TestRepositoryForWorktreeUsesTheWorktreeNotTheWorkingDirectory pins plan
// 0010 D4 (0004 bug 4): the same absolute worktree resolves to the same
// repository wherever specctl was launched.
func TestRepositoryForWorktreeUsesTheWorktreeNotTheWorkingDirectory(t *testing.T) {
	worktree, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "market-mini", "compliant"))
	if err != nil {
		t.Fatal(err)
	}
	want := "market-mini"
	if got := repositoryForWorktree(worktree); got != want {
		t.Errorf("from the repository root: got %q, want %q", got, want)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(previous) })
	if got := repositoryForWorktree(worktree); got != want {
		t.Errorf("from an unrelated directory: got %q, want %q", got, want)
	}
}

// TestPolicyTestWithGatorRefusesAnEmptySuiteSet pins plan 0010 D4: a --gator
// run with no suites must say so, not pass silently.
func TestPolicyTestWithGatorRefusesAnEmptySuiteSet(t *testing.T) {
	source := filepath.Join("..", "..", "examples", "policies", "market-mini")
	dir := filepath.Join(t.TempDir(), "market-mini")
	if err := copyTreeForTest(source, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, policy.TestsDir)); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runWith("policy", "test", "--dir", dir, "--gator")
	if code == exitOK {
		t.Errorf("a gator run with no suites passed: stderr %q", stderr)
	}
	if !strings.Contains(stderr, "no suites") {
		t.Errorf("stderr does not say there were no suites: %q", stderr)
	}
}

func copyTreeForTest(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		to := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, info.Mode().Perm())
	})
}

// TestPolicyTestLeavesThePolicyDirectoryAlone pins that `policy test` reads:
// the dist, the catalogue and the lock are rendered into a scratch copy, so
// the tree the test judges is exactly as it was before the run.
func TestPolicyTestLeavesThePolicyDirectoryAlone(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runWith("policy", "init", "--dir", dir, "--repo", "calc", "--with-library"); code != exitOK {
		t.Fatalf("init: code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, policy.DistDir)); !os.IsNotExist(err) {
		t.Fatalf("init wrote dist/ before the test ran: %v", err)
	}
	before := treeFingerprint(t, dir)

	code, stdout, stderr := runWith("policy", "test", "--dir", dir)
	if code != exitOK {
		t.Fatalf("policy test: code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, policy.DistDir)); !os.IsNotExist(err) {
		t.Errorf("policy test wrote dist/ into the policy directory: %v", err)
	}
	if !strings.Contains(stdout, "suites: ") {
		t.Errorf("policy test ran no suites: %q", stdout)
	}
	after := treeFingerprint(t, dir)
	for name, stamp := range after {
		if before[name] != stamp {
			t.Errorf("policy test rewrote %s", name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			t.Errorf("policy test removed %s", name)
		}
	}
}

// treeFingerprint records every file under dir with its size and modification
// time, so a rewrite that leaves identical content still shows.
func treeFingerprint(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[relative] = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
