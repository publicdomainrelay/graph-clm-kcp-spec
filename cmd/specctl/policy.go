package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/oagit"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policygit"
	specdlib "github.com/publicdomainrelay/graph-clm-kcp-spec/policies/library"
)

const defaultGator = "bin/gator"

func runPolicy(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, policyUsage)
		return exitUsage
	}
	subcommand, rest := args[0], args[1:]
	switch subcommand {
	case "init":
		return runPolicyInit(rest, stdout, stderr)
	case "new":
		return runPolicyNew(rest, stdout, stderr)
	case "build":
		return runPolicyBuild(rest, stdout, stderr)
	case "test":
		return runPolicyTest(rest, stdout, stderr)
	case "eval":
		return runPolicyEval(rest, stdout, stderr)
	case "effects":
		return runPolicyEffects(rest, stdout, stderr)
	case "model":
		return runPolicyModel(rest, stdout, stderr)
	case "generate":
		return runPolicyGenerate(rest, stdout, stderr)
	case "bind":
		return runPolicyBind(rest, stdout, stderr)
	case "accept":
		return runPolicyAccept(rest, stdout, stderr)
	case "changes":
		return runPolicyChanges(rest, stdout, stderr)
	case "apply":
		return runPolicyApply(rest, stdout, stderr)
	case "ls":
		return runPolicyLs(rest, stdout, stderr)
	case "report":
		return runPolicyReport(rest, stdout, stderr)
	case "findings":
		return runPolicyFindings(rest, stdout, stderr)
	case "waive":
		return runPolicyWaive(rest, stdout, stderr)
	case "fix":
		return runPolicyFix(rest, stdout, stderr)
	case "restore":
		return runPolicyRestore(rest, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, policyUsage)
		return exitOK
	}
	fmt.Fprintf(stderr, "specctl policy: unknown subcommand %q\n", subcommand)
	fmt.Fprint(stderr, policyUsage)
	return exitUsage
}

const policyUsage = `specctl policy manages policies: Gatekeeper templates and constraints over
the spec objects and the code.

usage:
  specctl policy init [--repo X] [--dir D | --path <git repo>] [--branch B] [--default-branch main]
      [--with-library | --from DIR] [--enforcement SPEC]
      create the policy tree: policies.yaml, lib/specd.rego, lib/specd_test.rego.
      --dir writes a plain directory; --path writes the orphan branch
      open-policy/X[--<branch slug>]; --with-library also copies the embedded
      policy library: the ported change-integrity, spec-structure, provisioning
      and disabled-verification templates with their constraints and gator suites;
      --from DIR seeds the tree from an existing policy directory instead: the
      directory's own templates, constraints and suites are copied and its
      imports and policies.lock are kept -- an imported pack is not copied, it
      resolves when 'policy build' runs -- the manifest takes --repo as its
      repository, and lib/, dist/ (the directory's own templates) and
      CATALOGUE.md are rebuilt;
      --enforcement SPEC writes the manifest's enforcement overrides, as
      whitespace- or comma-separated <name glob>=<deny|warn|dryrun> entries
      applied in order, the last match winning; a load applies them to the
      library's own constraints and to an imported pack's alike
  specctl policy new <name> --kind <Kind> [--title T] [--level MUST] [--pattern P]
      [--dir D | --path <git repo>] [--repo X]
      scaffold src.rego, src_test.rego, template.yaml, the constraint and a
      gator suite (one allowed case, one denied case)
  specctl policy build [--dir D | --path <git repo> --repo X] [--relock]
      [--cache-dir DIR] [--member NAME=PATH]
      write dist/, refresh lib/specd.rego, render CATALOGUE.md, and pin the
      imported packs and the members policies.yaml names
  specctl policy test [--dir D] [--gator] [--gator-bin <path>]
      opa unit tests and gator suites through the built-in engine; --gator also
      runs the real gator binary (--gator-bin, else $SPECD_GATOR, else bin/gator,
      else the first gator on PATH)
  specctl policy eval --repo X [--worktree P | --commit C] [--path <git repo>]
      [--library D] [--branch B] [--diff-base REF] [--test-glob G]
      [--cache-dir DIR] [--member NAME=PATH] [-o text|json] [--strict]
      one-off audit of a checkout or a commit against the policy branch;
      --diff-base also derives a CodeDiff between REF and the evaluated commit,
      so the provisioning and disabled-verification templates have an object
      to review
  specctl policy eval --repo X --specs-only [--path <git repo>] [--library D] [-o text|json]
      [--strict]
      the spec-time half of the gate, offline: build the ArchitectureModel from
      the SystemContexts on the open-architecture branch, declared facts only,
      and evaluate the library against it. No code is indexed
  specctl policy effects [--worktree P | --commit C] [--repo X] [--path <git repo>]
      [--classifiers DIR] [--kind K] [--no-extras] [-o text|json]
      classify the code into the fixed effect vocabulary (net.dial, ssh.connect,
      proc.exec, http.handle, ...) and print the effects grouped by
      context and file. Classifier packs ship with specctl; a repository adds
      its own in <worktree>/classifiers/*.yaml or in --classifiers DIR
  specctl policy model [--worktree P | --commit C] [--repo X] [--path <git repo>]
      [--library D] [--classifiers DIR] [--test-glob G] [--cache-dir DIR]
      [--member NAME=PATH] [-o text|json]
      build the ArchitectureModel (components, roles, effects, flows, triggers)
      from the CodeGraph, the effects, the SystemContexts and the roles and
      vocabulary of policies.yaml, plus every member repository the library
      names (--member NAME=PATH clones one from a local checkout instead)
  specctl policy apply -f F | --library D [--prune]
      write ConstraintTemplates, their constraint CRDs and their constraints
      into kcp
  specctl policy restore --repo X [--path <git repo>] [--branch B] [--prune]
      load the policy branch open-policy/X[--<branch slug>] into kcp; specd
      does the same when a Repository is created
  specctl policy generate --repo X --prompt "..." [--requirement ctx#id] [--systemcontext C]
      [--enforcement deny|warn|dryrun] [--slug S] [--apply] [--wait] [--timeout D]
      create a PolicyChange: specd's harness authors a template over the
      ArchitectureModel, checks it (compile, opa tests, gator suite, mutation
      check, head evaluation) and records it as Evaluated. --apply commits it to
      the open-policy branch and applies it to kcp
  specctl policy bind --repo X --pack P [--pack-version V] [--systemcontext C]
      [--apply] [--wait] [--timeout D]
      create a PolicyChange in bind mode: the harness proposes the roles and the
      vocabulary of policies.yaml for the pack, and specd checks that every
      required role selects a component, that every vocabulary class matches an
      effect or a spec term, that the pack's suites pass and that the pack
      denies a case derived from the bound model
  specctl policy accept <policychange> [--wait] [--timeout D]
      set apply on an evaluated PolicyChange; specd commits and applies it
  specctl policy changes [--repo X] [-o table|json|name]
      the PolicyChanges kcp holds
  specctl policy ls [-o table|json|name]
      the templates and constraints kcp holds, and the PolicyChanges
  specctl policy report --repo X [-o text|json]
      the last audit: the policy commit, the evaluated commit, the totals and
      the first violations of Repository.status.policy
  specctl policy findings --repo X [--worktree P | --commit C] [--base REF]
      [--library D] [-o text|json]
      every violation of the checkout, each with its stable key (constraint,
      object, site) and its status: new, inherited (the --base ref already
      carried it) or waived (a durable exception on the policy branch covers
      it). This is how a repository finds what it already violates
  specctl policy waive <key> --reason R [--owner O] [--expires DATE]
      [--repo X --worktree P | --commit C] [--dir D | --path P]
      write a durable, site-scoped exception for one finding onto the policy
      branch, under exceptions/<key>.yaml. Every gate, the audit and an
      offline evaluation honour it and report the violation as waived
  specctl policy fix <key> [--repo X --worktree P | --commit C] [-o yaml|json|text]
      [--write FILE] [--apply [--dry-run] --system-context S --spec-hash H]
      turn one finding into a SpecChange request: the violation, its site and
      the instruction the normal spec flow hands to the agent. -o text prints
      the prompt alone. --apply creates the SpecChange itself -- a spec-to-code
      change on the named SystemContext, with the prompt on its annotations --
      and --dry-run prints that object without creating it

gator suite paths: a suite in <dir>/tests/<name>/suite.yaml references the
built template as ../../dist/<name>.yaml, so run 'policy build' before
'policy test' or 'gator verify'.
`

type policyTarget struct {
	dir string

	path string

	repo string

	branch string

	defaultBranch string
}

func (t policyTarget) onBranch() bool {
	return t.dir == ""
}

func (t policyTarget) ref() string {
	return policy.RefFor(t.repo, t.branch, t.defaultBranch)
}

func (t policyTarget) apply(ctx context.Context, add map[string][]byte, remove []string, message string) (string, error) {
	if !t.onBranch() {
		for _, name := range remove {
			if err := os.Remove(filepath.Join(t.dir, filepath.FromSlash(name))); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		for name, data := range add {
			path := filepath.Join(t.dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return "", err
			}
		}
		return "", nil
	}
	store := oagit.Store{Repo: t.path}
	return policygit.Update(ctx, store, t.ref(), add, remove, message)
}

func (t policyTarget) loadRaw(ctx context.Context) (policy.Library, error) {
	if !t.onBranch() {
		library, err := policyeval.LoadRaw(os.DirFS(t.dir))
		if err != nil {
			return policy.Library{}, err
		}
		if data, err := os.ReadFile(filepath.Join(t.dir, policy.LockPath)); err == nil {
			library.Files[policy.LockPath] = data
		}
		return library, nil
	}
	store := oagit.Store{Repo: t.path}
	library, _, err := policygit.Read(ctx, store, t.ref())
	return library, err
}

func (t policyTarget) load(ctx context.Context) (policy.Library, error) {
	library, err := t.loadRaw(ctx)
	if err != nil {
		return policy.Library{}, err
	}
	resolved, _, err := resolveLibraryImports(library, false)
	return resolved, err
}

// resolveLibraryImports merges the packs a library imports. The lock the
// library carries pins them; verify refuses a pack that moved. The manifest's
// enforcement rules are applied either way, so a library that imports nothing
// still gets them.
func resolveLibraryImports(library policy.Library, verify bool) (policy.Library, []policy.LockEntry, error) {
	if len(library.Manifest.Imports) == 0 {
		library.ApplyEnforcement()
		return library, nil, nil
	}
	lock := policy.PackLock{}
	pinned := false
	if data, ok := library.Files[policy.LockPath]; ok {
		parsed, err := policyeval.ParseLock(data)
		if err != nil {
			return policy.Library{}, nil, err
		}
		lock = parsed
		pinned = true
	}
	return policyeval.ResolveImports(library, policyeval.ImportOptions{Lock: &lock, Verify: verify && pinned})
}

func addPolicyTargetFlags(fs *flag.FlagSet) *policyTarget {
	target := &policyTarget{}
	fs.StringVar(&target.dir, "dir", "", "policy directory to read and write")
	fs.StringVar(&target.path, "path", ".", "git repository whose policy branch to read and write")
	fs.StringVar(&target.repo, "repo", "", "repository name (default: the base name of --path)")
	fs.StringVar(&target.branch, "branch", "", "code branch whose policy branch to use")
	fs.StringVar(&target.defaultBranch, "default-branch", "main", "default code branch")
	return target
}

func (t *policyTarget) resolveRepo() {
	if t.repo == "" {
		absolute, err := filepath.Abs(t.path)
		if err == nil {
			t.repo = filepath.Base(absolute)
		}
	}
}

func runPolicyInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := addPolicyTargetFlags(fs)
	testGlobs := stringsFlag{}
	fs.Var(&testGlobs, "test-glob", "test file glob for the manifest; repeatable")
	withLibrary := fs.Bool("with-library", false, "copy the embedded policy library into the new policy tree")
	from := fs.String("from", "", "seed the new policy tree from an existing policy directory")
	enforcement := fs.String("enforcement", "", "enforcement the library's constraints get, as <name glob>=<deny|warn|dryrun> entries applied in order, the last match winning")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	target.resolveRepo()
	if target.onBranch() && !flagSet(fs, "repo") {
		fmt.Fprintln(stderr, "specctl policy init: --repo is required when writing a policy branch")
		return exitUsage
	}
	rules, err := parseEnforcementSpec(*enforcement)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
		return exitUsage
	}

	ctx := context.Background()
	before, err := policygit.Tip(ctx, oagit.Store{Repo: target.path}, target.ref())
	if !target.onBranch() {
		before = ""
		err = nil
	}
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
		return exitError
	}

	manifest := policy.PolicyLibrary{
		Repository:         target.repo,
		Version:            "1",
		TestGlobs:          testGlobs,
		DefaultEnforcement: policy.EnforcementDryRun,
	}
	add := map[string][]byte{}
	copied := 0
	seeded := policy.Library{}
	switch {
	case *from != "":
		seeded, err = policyeval.LoadRaw(os.DirFS(*from))
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy init: read %s: %v\n", *from, err)
			return exitError
		}
		if len(seeded.Templates) == 0 {
			fmt.Fprintf(stderr, "specctl policy init: %s holds no template\n", *from)
			return exitError
		}
		add, err = policyFiles(*from)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
			return exitError
		}
		copied = len(add)
		manifest = seeded.Manifest
		manifest.Repository = target.repo
		seeded.Manifest = manifest
	case *withLibrary:
		files, err := specdlib.Files()
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
			return exitError
		}
		maps.Copy(add, files)
		copied = len(files)
	}
	if flagSet(fs, "enforcement") {
		manifest.Enforcement = rules
		seeded.Manifest = manifest
	}
	doc, err := yaml.Marshal(manifest)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
		return exitError
	}
	add[policy.PoliciesPath] = doc
	add[policy.LibPath] = []byte(policyeval.Lib())
	add[policy.LibTestPath] = []byte(policyeval.LibTest())
	add[policy.GitAttributesPath] = []byte(policygit.GitAttributes)
	if len(seeded.Templates) > 0 {
		dist, err := policyeval.Dist(seeded)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
			return exitError
		}
		maps.Copy(add, dist)
		add[policy.CataloguePath] = policyeval.Catalogue(seeded)
	}
	message := fmt.Sprintf("policy(%s): init\n", target.repo)
	commit, err := target.apply(ctx, add, nil, message)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
		return exitError
	}
	if target.onBranch() {
		fmt.Fprintf(stdout, "open-policy/%s ready at %s\n", target.repo, shortCommit(commit))
		if commit == before {
			fmt.Fprintln(stdout, "no change")
		}
	} else {
		fmt.Fprintf(stdout, "%s ready\n", target.dir)
	}
	if *from != "" {
		fmt.Fprintf(stdout, "seeded from %s: %d files copied; dist/ and CATALOGUE.md are rendered\n", *from, copied)
	} else if *withLibrary {
		fmt.Fprintf(stdout, "library: %d files copied; run specctl policy build to render dist/ and CATALOGUE.md\n", copied)
	}
	return exitOK
}

// parseEnforcementSpec reads the --enforcement value: whitespace- or
// comma-separated <name glob>=<deny|warn|dryrun> entries, applied in order, the
// last match winning. It is the same syntax scripts/example-pr.sh passes.
func parseEnforcementSpec(spec string) ([]policy.EnforcementRule, error) {
	rules := []policy.EnforcementRule{}
	for _, token := range strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		name, action, found := strings.Cut(token, "=")
		if !found || name == "" || !policy.Enforcement(action).Known() {
			return nil, fmt.Errorf("%q is not <name glob>=<deny|warn|dryrun>", token)
		}
		rules = append(rules, policy.EnforcementRule{Name: name, Action: policy.Enforcement(action)})
	}
	return rules, nil
}

// policyFiles reads a policy directory: every file a user authored, and none
// of the generated ones. lib/, dist/, CATALOGUE.md, .gitattributes and the
// manifest are written by the caller, so an imported tree is rebuilt the same
// way specctl policy build would rebuild it. policies.lock is authored -- it
// pins the imports -- and is copied as it stands.
func policyFiles(dir string) (map[string][]byte, error) {
	add := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasPrefix(rel, policy.DistDir+"/"), strings.HasPrefix(rel, policy.ReportsDir+"/"):
			return nil
		case rel == policy.CataloguePath, rel == policy.GitAttributesPath:
			return nil
		case rel == policy.PoliciesPath, rel == policy.LibPath, rel == policy.LibTestPath:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		add[rel] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return add, nil
}

func runPolicyNew(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := addPolicyTargetFlags(fs)
	kind := fs.String("kind", "", "constraint kind, for example RelayOnly")
	title := fs.String("title", "", "one line title for the template metadata")
	level := fs.String("level", "MUST", "MUST, SHOULD or MAY")
	pattern := fs.String("pattern", "guest", "regular expression the rule looks for")
	sample := fs.String("sample", "", "text the denied fixture contains (default: the pattern without backslashes)")
	globs := stringsFlag{}
	fs.Var(&globs, "glob", "file glob the rule selects; repeatable (default **/*.ts)")
	enforcement := fs.String("enforcement", "dryrun", "deny, warn or dryrun")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "specctl policy new: exactly one name is required")
		return exitUsage
	}
	slug := positional[0]
	if *kind == "" {
		*kind = kindOf(slug)
	}
	templateName := strings.ToLower(*kind)
	if *title == "" {
		*title = slug
	}
	if !policy.KnownLevel(policy.Level(*level)) {
		fmt.Fprintf(stderr, "specctl policy new: unknown level %q\n", *level)
		return exitUsage
	}
	if !policy.Enforcement(*enforcement).Known() {
		fmt.Fprintf(stderr, "specctl policy new: unknown enforcement %q\n", *enforcement)
		return exitUsage
	}
	target.resolveRepo()
	if target.onBranch() && !flagSet(fs, "repo") {
		fmt.Fprintln(stderr, "specctl policy new: --repo is required when writing a policy branch")
		return exitUsage
	}
	selected := []string(globs)
	if len(selected) == 0 {
		selected = []string{"**/*.ts"}
	}

	sampleText := *sample
	if sampleText == "" {
		sampleText = strings.ReplaceAll(*pattern, "\\", "")
	}
	header, constraint, source, test, suite, cases := scaffold(scaffoldOptions{
		Slug:         slug,
		TemplateName: templateName,
		Kind:         *kind,
		Title:        *title,
		Level:        *level,
		Pattern:      *pattern,
		Sample:       sampleText,
		Path:         samplePath(selected[0]),
		Globs:        selected,
		Enforcement:  *enforcement,
	})
	add := map[string][]byte{
		policy.TemplateHeaderPath(slug): header,
		policy.TemplateSourcePath(slug): source,
		policy.TemplateTestPath(slug):   test,
		policy.ConstraintPath(slug):     constraint,
		policy.SuitePath(slug):          suite,
	}
	for path, data := range cases {
		add[path] = data
	}
	ctx := context.Background()
	message := fmt.Sprintf("policy(%s): new %s\n", target.repo, slug)
	if _, err := target.apply(ctx, add, nil, message); err != nil {
		fmt.Fprintf(stderr, "specctl policy new: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "policy %s scaffolded (template %s, kind %s)\n", slug, templateName, *kind)
	fmt.Fprintf(stdout, "next: specctl policy build && specctl policy test\n")
	return exitOK
}

func kindOf(name string) string {
	parts := strings.FieldsFunc(name, func(char rune) bool {
		return char == '-' || char == '_' || char == '.' || char == '/'
	})
	builder := strings.Builder{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	if builder.Len() == 0 {
		return "Policy"
	}
	return builder.String()
}

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	if commit == "" {
		return "working tree"
	}
	return commit
}

func runPolicyBuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := addPolicyTargetFlags(fs)
	relock := fs.Bool("relock", false, "write the resolved pack digests into "+policy.LockPath+" even when a pin moved")
	cacheDir := fs.String("cache-dir", defaultCacheDir(), "where a member repository is cloned")
	memberPathFlags := memberPaths{}
	fs.Var(memberPathFlags, "member", "clone the named member from a local path instead of its url (name=path); repeatable")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	target.resolveRepo()
	if target.onBranch() && !flagSet(fs, "repo") {
		fmt.Fprintln(stderr, "specctl policy build: --repo is required when writing a policy branch")
		return exitUsage
	}
	return buildTarget(context.Background(), target, stdout, stderr, *relock, *cacheDir, memberPathFlags)
}

func buildTarget(ctx context.Context, target *policyTarget, stdout, stderr io.Writer, relock bool, cacheDir string, memberPathFlags memberPaths) int {
	raw, err := target.loadRaw(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
		return exitError
	}
	library, entries, err := resolveLibraryImports(raw, !relock)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
		return exitError
	}
	members, err := policyeval.ResolveMembers(ctx, memberRefs(library, cacheDir), library, policyeval.MemberOptions{
		CacheDir: cacheDir,
		Lock:     lockOf(library),
		Verify:   !relock,
		Paths:    memberPathFlags,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
		return exitError
	}
	defer cleanupMembers(members)
	if len(library.Templates) == 0 && !target.onBranch() {
		fmt.Fprintf(stderr, "specctl policy build: %s has no templates\n", target.dir)
		return exitError
	}

	add := map[string][]byte{
		policy.LibPath:     []byte(policyeval.Lib()),
		policy.LibTestPath: []byte(policyeval.LibTest()),
	}
	written := []string{policy.LibPath}
	dist, err := policyeval.Dist(library)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
		return exitError
	}
	for path, encoded := range dist {
		add[path] = encoded
		written = append(written, path)
	}
	add[policy.CataloguePath] = policyeval.Catalogue(library)
	written = append(written, policy.CataloguePath)
	// A build that resolved no member -- `policy test`, which renders the
	// library and reads no other repository -- keeps the pins the lock already
	// carries instead of dropping them.
	pins := []policy.MemberLock{}
	if existing := lockOf(library); existing != nil {
		pins = append(pins, existing.Members...)
	}
	for _, member := range members {
		pins = append(pins, member.Lock())
	}
	if len(entries) > 0 || len(pins) > 0 {
		encoded, err := policyeval.EncodeLockWithMembers(entries, pins)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
			return exitError
		}
		add[policy.LockPath] = encoded
		written = append(written, policy.LockPath)
	}

	message := fmt.Sprintf("policy(%s): build\n", target.repo)
	if _, err := target.apply(ctx, add, staleDist(target, library, add), message); err != nil {
		fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
		return exitError
	}
	sort.Strings(written)
	for _, path := range written {
		fmt.Fprintln(stdout, path)
	}
	for _, entry := range entries {
		fmt.Fprintf(stdout, "pack %s@%s %s %s\n", entry.Pack, entry.Version, shortDigest(entry.SHA256), entry.Source)
	}
	return exitOK
}

// staleDist is the rendered templates a build no longer produces: the dist
// file of a template that was deleted. Left behind, a gator suite could still
// reference it and pass against a rule the library no longer holds.
func staleDist(target *policyTarget, library policy.Library, add map[string][]byte) []string {
	existing := []string{}
	if target.onBranch() {
		for path := range library.Files {
			existing = append(existing, path)
		}
	} else {
		entries, err := os.ReadDir(filepath.Join(target.dir, policy.DistDir))
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			existing = append(existing, policy.DistDir+"/"+entry.Name())
		}
	}
	out := []string{}
	for _, path := range existing {
		if !strings.HasPrefix(path, policy.DistDir+"/") || add[path] != nil {
			continue
		}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func shortDigest(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

func runPolicyTest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := addPolicyTargetFlags(fs)
	withGator := fs.Bool("gator", false, "also run the real gator binary over the suites")
	gatorBin := fs.String("gator-bin", "", "path to gator (default $SPECD_GATOR, bin/gator, then PATH)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if target.onBranch() {
		fmt.Fprintln(stderr, "specctl policy test: --dir is required; test a checkout of the policy branch")
		return exitUsage
	}
	target.resolveRepo()
	ctx := context.Background()

	// A test reads: the dist, the catalogue and the lock are rendered into a
	// scratch copy of the directory and the suites run there, so a test never
	// rewrites the tree it judges. `policy build` is what writes.
	dir, library, cleanup, err := renderTestTree(ctx, target)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
		return exitError
	}
	defer cleanup()

	failed := 0
	modules := map[string]string{policy.LibPath: policyeval.Lib(), policy.LibTestPath: policyeval.LibTest()}
	for _, template := range library.Templates {
		slug := policy.TemplateSlug(template)
		// The template came from the pack registry when the repository imports
		// it, so the source the library resolved is the source to test.
		modules[policy.TemplateSourcePath(slug)] = template.Rego
		if test, ok := policyeval.TemplateTest(os.DirFS(dir), slug); ok {
			modules[policy.TemplateTestPath(slug)] = string(test)
		}
	}
	unitResults, err := policyeval.RunOpaTests(ctx, modules)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
		return exitError
	}
	passedUnits := 0
	for _, result := range unitResults {
		if result.Passed() {
			passedUnits++
			continue
		}
		failed++
		fmt.Fprintf(stderr, "FAIL %s.%s: %s\n", result.Package, result.Name, result.Error)
	}
	fmt.Fprintf(stdout, "opa: %d/%d passed\n", passedUnits, len(unitResults))

	suites, err := filepath.Glob(filepath.Join(dir, policy.TestsDir, "*", policy.SuiteName))
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
		return exitError
	}
	sort.Strings(suites)
	passedCases, totalCases := 0, 0
	for _, suite := range suites {
		relative, err := filepath.Rel(dir, suite)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
			return exitError
		}
		result, err := policyeval.RunSuite(ctx, os.DirFS(dir), filepath.ToSlash(relative))
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy test: %s: %v\n", relative, err)
			return exitError
		}
		totalCases += result.Cases()
		for _, test := range result.Tests {
			for _, item := range test.Cases {
				if item.Error == "" {
					passedCases++
					continue
				}
				failed++
				fmt.Fprintf(stderr, "FAIL %s/%s/%s: %s\n", result.Name, test.Name, item.Name, item.Error)
			}
		}
		if !result.Passed() {
			for _, test := range result.Tests {
				if test.Error != "" {
					failed++
					fmt.Fprintf(stderr, "FAIL %s/%s: %s\n", result.Name, test.Name, test.Error)
				}
			}
		}
	}
	fmt.Fprintf(stdout, "suites: %d/%d cases passed\n", passedCases, totalCases)

	if *withGator {
		if len(suites) == 0 {
			fmt.Fprintf(stderr, "specctl policy test: gator: no suites under %s, nothing was verified\n",
				filepath.Join(dir, policy.TestsDir))
			failed++
		} else {
			bin, looked := gatorBinary(*gatorBin)
			if bin == "" {
				fmt.Fprintf(stderr, "specctl policy test: gator not found in %s; run scripts/install-policy-tools.sh\n", strings.Join(looked, ", "))
				failed++
			} else if code := runGator(bin, filepath.Join(dir, policy.TestsDir), stdout, stderr); code != exitOK {
				failed++
			}
		}
	}

	if failed > 0 {
		return exitError
	}
	return exitOK
}

// renderTestTree copies the policy directory to a scratch tree and renders the
// files a build would write -- lib, dist, the catalogue -- into the copy, so
// the suites read the rules the library resolves while the directory itself is
// left exactly as it was.
func renderTestTree(ctx context.Context, target *policyTarget) (string, policy.Library, func(), error) {
	raw, err := target.loadRaw(ctx)
	if err != nil {
		return "", policy.Library{}, func() {}, err
	}
	library, _, err := resolveLibraryImports(raw, true)
	if err != nil {
		return "", policy.Library{}, func() {}, err
	}
	dist, err := policyeval.Dist(library)
	if err != nil {
		return "", policy.Library{}, func() {}, err
	}
	add := map[string][]byte{
		policy.LibPath:     []byte(policyeval.Lib()),
		policy.LibTestPath: []byte(policyeval.LibTest()),
	}
	maps.Copy(add, dist)
	add[policy.CataloguePath] = policyeval.Catalogue(library)

	dir, err := os.MkdirTemp("", "specctl-policy-test-")
	if err != nil {
		return "", policy.Library{}, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := os.CopyFS(dir, os.DirFS(target.dir)); err != nil {
		cleanup()
		return "", policy.Library{}, func() {}, err
	}
	for name, data := range add {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			cleanup()
			return "", policy.Library{}, func() {}, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			cleanup()
			return "", policy.Library{}, func() {}, err
		}
	}
	return dir, library, cleanup, nil
}

func gatorBinary(explicit string) (string, []string) {
	looked := []string{}
	for _, candidate := range []string{explicit, os.Getenv("SPECD_GATOR"), defaultGator} {
		if candidate == "" {
			continue
		}
		looked = append(looked, candidate)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, looked
		}
	}
	looked = append(looked, "PATH")
	if found, err := exec.LookPath("gator"); err == nil {
		return found, looked
	}
	return "", looked
}

func runGator(bin, suites string, stdout, stderr io.Writer) int {
	command := exec.Command(bin, "verify", suites)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(stderr, "specctl policy test: gator verify: %v\n", err)
		return exitError
	}
	return exitOK
}

func runPolicyEval(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "repository name")
	worktree := fs.String("worktree", "", "code checkout to index and evaluate")
	commit := fs.String("commit", "", "commit of --path to export and evaluate")
	path := fs.String("path", ".", "git repository holding the policy branch")
	branch := fs.String("branch", "", "code branch")
	defaultBranch := fs.String("default-branch", "main", "default code branch")
	libraryDir := fs.String("library", "", "read the policy library from this directory instead of the branch")
	diffBase := fs.String("diff-base", "", "ref to diff the evaluated commit against; builds a CodeDiff")
	specsOnly := fs.Bool("specs-only", false, "evaluate the declared state alone: the ArchitectureModel from the specs, no code and no effects")
	output := fs.String("o", "text", "text or json")
	strict := fs.Bool("strict", false, "exit 1 when a deny violation survives the cap")
	inherited := fs.Bool("inherited", false, "evaluate --diff-base too and report what it already carried as inherited")
	indexInPlace := fs.Bool("index-in-place", false, "write the codegraph index into the checkout instead of a copy")
	cacheDir := fs.String("cache-dir", defaultCacheDir(), "where a member repository is cloned")
	memberPathFlags := memberPaths{}
	fs.Var(memberPathFlags, "member", "clone the named member from a local path instead of its url (name=path); repeatable")
	testGlobs := stringsFlag{}
	fs.Var(&testGlobs, "test-glob", "test file glob; repeatable")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *repository == "" && *worktree != "" {
		*repository = repositoryForWorktree(*worktree)
	}
	if *repository == "" {
		fmt.Fprintln(stderr, "specctl policy eval: --repo is required")
		return exitUsage
	}
	if *worktree != "" && *commit != "" {
		fmt.Fprintln(stderr, "specctl policy eval: --worktree and --commit are exclusive")
		return exitUsage
	}

	ctx := context.Background()
	library, err := loadLibrary(ctx, *repository, *libraryDir, *path, *branch, *defaultBranch)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	if len(library.Templates) == 0 {
		fmt.Fprintf(stderr, "specctl policy eval: no policies for %s; run specctl policy init --repo %s\n", *repository, *repository)
		return exitError
	}

	if *specsOnly {
		contexts := loadContexts(ctx, *path, *repository, *branch, *defaultBranch)
		result, err := policyeval.CheckSpecs(ctx, policyeval.SpecInput{
			Repository: *repository,
			Contexts:   contexts,
			Binding:    library.Manifest.Binding(),
			Library:    library,
		})
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		store := oagit.Store{Repo: *path}
		if tip, tipErr := store.Tip(ctx, oabranch.RefFor(*repository, *branch, *defaultBranch)); tipErr == nil {
			result.Report.Commit = tip
		}
		if *output == "json" {
			encoded, err := json.MarshalIndent(evalReport{Report: result.Report, Waived: waivedRefs(result.Decision.Waived)}, "", "  ")
			if err != nil {
				fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
				return exitError
			}
			fmt.Fprintln(stdout, string(encoded))
		} else {
			printReportDecision(stdout, result.Report, result.Decision.Waived)
			fmt.Fprintf(stdout, "spec gate: %s\n", specGateVerdict(result.Decision))
		}
		if *strict && result.Decision.Blocked {
			return exitError
		}
		return exitOK
	}

	codeDir := *worktree
	resolved := ""
	cleanup := func() {}
	if *commit != "" {
		codeDir, cleanup, err = exportCommit(ctx, *path, *commit)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		resolved = *commit
	} else if codeDir == "" {
		codeDir = *path
	}
	defer cleanup()
	if absolute, err := filepath.Abs(codeDir); err == nil {
		codeDir = absolute
	}
	if resolved == "" {
		resolved = codegraphfacts.GitCommit(ctx, codeDir)
	}
	if len(testGlobs) == 0 {
		testGlobs = library.Manifest.TestGlobs
	}

	report, err := evaluateCode(ctx, codeEvaluation{
		Repository:    *repository,
		Branch:        *branch,
		DefaultBranch: *defaultBranch,
		Path:          *path,
		CodeDir:       codeDir,
		Commit:        resolved,
		Library:       library,
		TestGlobs:     testGlobs,
		IndexInPlace:  *indexInPlace,
		DiffBase:      *diffBase,
		CacheDir:      *cacheDir,
		Members:       memberPathFlags,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}

	if *inherited {
		return printInherited(ctx, inheritedOptions{
			stdout: stdout, stderr: stderr, strict: *strict, output: *output,
			library: library, report: report, evaluation: codeEvaluation{
				Repository:    *repository,
				Branch:        *branch,
				DefaultBranch: *defaultBranch,
				Path:          *path,
				CodeDir:       codeDir,
				Library:       library,
				TestGlobs:     testGlobs,
				CacheDir:      *cacheDir,
				Members:       memberPathFlags,
			},
			base: *diffBase,
		})
	}

	waivers := evalWaivers(library, stderr)
	decision := policy.Decide(report, policy.RepositoryPolicy{}, waivers)
	if *output == "json" {
		encoded, err := json.MarshalIndent(evalReport{Report: report, Waived: waivedRefs(decision.Waived)}, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
	} else {
		printReportDecision(stdout, report, decision.Waived)
	}
	if *strict && (decision.Blocked || memberPolicyFailed(report)) {
		return exitError
	}
	return exitOK
}

// memberPolicyFailed reports whether a submodule's own policy branch denied, or
// could not be evaluated: the org root must not pass over either.
func memberPolicyFailed(report policy.Report) bool {
	for _, row := range report.MemberPolicies {
		if row.Message != "" || row.Totals[policy.EnforcementDeny] > 0 {
			return true
		}
	}
	return false
}

// evalReport is `policy eval -o json`: the report the branches and kcp carry,
// plus the violations a durable exception waives. The extra key is additive, so
// a reader of policy.Report still reads the same object.
type evalReport struct {
	policy.Report

	Waived []waivedRef `json:"waived,omitempty"`
}

type waivedRef struct {
	Key string `json:"key"`

	Constraint string `json:"constraint"`

	Site string `json:"site,omitempty"`

	Reason string `json:"reason,omitempty"`

	Owner string `json:"owner,omitempty"`
}

func waivedRefs(violations []policy.Violation) []waivedRef {
	out := make([]waivedRef, 0, len(violations))
	for _, violation := range violations {
		file, line := policy.Site(violation)
		site := ""
		if file != "" {
			site = fmt.Sprintf("%s:%d", file, line)
		}
		out = append(out, waivedRef{
			Key:        policy.Key(violation),
			Constraint: violation.Constraint,
			Site:       site,
		})
	}
	return out
}

// evalWaivers is the durable exceptions of a library as the overrides an
// evaluation reads. An exception that has expired is reported, never dropped
// silently.
func evalWaivers(library policy.Library, stderr io.Writer) []policy.Override {
	waivers, expired := policyeval.Waivers(library, nil, time.Now())
	for _, exception := range expired {
		fmt.Fprintf(stderr, "specctl policy eval: the exception for %s expired %s and is not honoured\n",
			exception.Constraint, exception.Expires)
	}
	return waivers
}

type inheritedOptions struct {
	stdout io.Writer

	stderr io.Writer

	strict bool

	output string

	library policy.Library

	report policy.Report

	evaluation codeEvaluation

	base string
}

// printInherited is `policy eval --inherited`: the change-scoped reading of the
// offline evaluation. The base ref is evaluated too, so a violation the base
// already carried is reported as inherited (warn) and only a new one blocks.
func printInherited(ctx context.Context, options inheritedOptions) int {
	if options.base == "" {
		fmt.Fprintln(options.stderr, "specctl policy eval: --inherited needs --diff-base REF, the ref the change is on top of")
		return exitUsage
	}
	baseDir, cleanup, err := exportCommit(ctx, options.evaluation.CodeDir, options.base)
	if err != nil {
		fmt.Fprintf(options.stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	defer cleanup()
	if absolute, err := filepath.Abs(baseDir); err == nil {
		baseDir = absolute
	}
	evaluation := options.evaluation
	evaluation.CodeDir = baseDir
	evaluation.Commit = options.base
	evaluation.DiffBase = ""
	baseReport, err := evaluateCode(ctx, evaluation)
	if err != nil {
		fmt.Fprintf(options.stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	waivers, expired := policyeval.Waivers(options.library, nil, time.Now())
	for _, exception := range expired {
		fmt.Fprintf(options.stderr, "specctl policy eval: the exception for %s expired %s and is not honoured\n",
			exception.Constraint, exception.Expires)
	}
	findings := findingsOf(options.report, baseReport, waivers, true)
	if options.output == "json" {
		encoded, err := json.MarshalIndent(findings, "", "  ")
		if err != nil {
			fmt.Fprintf(options.stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		fmt.Fprintln(options.stdout, string(encoded))
	} else {
		printFindings(options.stdout, findings)
	}
	if options.strict {
		for _, finding := range findings {
			if finding.Status == findingNew && finding.Enforcement == string(policy.EnforcementDeny) {
				return exitError
			}
		}
	}
	return exitOK
}

func specGateVerdict(decision policy.Decision) string {
	if !decision.Blocked {
		return fmt.Sprintf("allowed (%d warned, %d dryrun)", len(decision.Warned), len(decision.DryRun))
	}
	return "denied: " + strings.Join(decision.Messages(), "; ")
}

func runPolicyEffects(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy effects", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repo", "", "repository name")
	worktree := fs.String("worktree", "", "code checkout to index and classify")
	commit := fs.String("commit", "", "commit of --path to export and classify")
	path := fs.String("path", ".", "git repository to read a commit from")
	branch := fs.String("branch", "", "code branch")
	defaultBranch := fs.String("default-branch", "main", "default code branch")
	output := fs.String("o", "text", "text or json")
	kind := fs.String("kind", "", "only print effects of one kind")
	noExtras := fs.Bool("no-extras", false, "disable classifier rules marked extra")
	classifiers := stringsFlag{}
	fs.Var(&classifiers, "classifiers", "directory of extra classifier packs; repeatable")
	testGlobs := stringsFlag{}
	fs.Var(&testGlobs, "test-glob", "test file glob; repeatable")
	indexInPlace := fs.Bool("index-in-place", false, "write the codegraph index into the checkout instead of a copy")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *worktree != "" && *commit != "" {
		fmt.Fprintln(stderr, "specctl policy effects: --worktree and --commit are exclusive")
		return exitUsage
	}
	if *kind != "" && !policy.KnownEffectKind(policy.EffectKind(*kind)) {
		fmt.Fprintf(stderr, "specctl policy effects: unknown effect kind %q\n", *kind)
		return exitUsage
	}

	ctx := context.Background()
	codeDir := *worktree
	cleanup := func() {}
	var err error
	if *commit != "" {
		codeDir, cleanup, err = exportCommit(ctx, *path, *commit)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy effects: %v\n", err)
			return exitError
		}
	} else if codeDir == "" {
		codeDir = *path
	}
	defer cleanup()
	if absolute, absErr := filepath.Abs(codeDir); absErr == nil {
		codeDir = absolute
	}
	if *repository == "" {
		*repository = filepath.Base(codeDir)
	}
	resolved := *commit
	if resolved == "" {
		resolved = codegraphfacts.GitCommit(ctx, codeDir)
	}

	contexts := loadContexts(ctx, *path, *repository, *branch, *defaultBranch)
	graph, err := codegraphfacts.Build(ctx, codeDir, codegraphfacts.Options{
		Repository:   *repository,
		Branch:       *branch,
		Commit:       resolved,
		TestGlobs:    testGlobs,
		Contexts:     codegraphfacts.ContextsByFile(contexts),
		IndexInPlace: *indexInPlace,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy effects: %v\n", err)
		return exitError
	}
	computed, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: classifierDirs(codeDir, classifiers),
		IncludeExtras:   !*noExtras,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy effects: %v\n", err)
		return exitError
	}
	if *kind != "" {
		computed = policy.EffectsOf(computed, policy.EffectKind(*kind))
	}

	if *output == "json" {
		encoded, err := json.MarshalIndent(computed, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy effects: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
		return exitOK
	}
	printEffects(stdout, *repository, resolved, computed)
	return exitOK
}

func loadLibrary(ctx context.Context, repository, libraryDir, path, branch, defaultBranch string) (policy.Library, error) {
	if libraryDir != "" {
		return policyeval.Load(libraryDir)
	}
	store := oagit.Store{Repo: path}
	library, _, err := policygit.Read(ctx, store, policy.RefFor(repository, branch, defaultBranch))
	if err != nil {
		return policy.Library{}, err
	}
	if len(library.Templates) == 0 && repository != "" {
		fallback := filepath.Join("examples", "policies", repository)
		if _, statErr := os.Stat(filepath.Join(fallback, policy.PoliciesPath)); statErr == nil {
			return policyeval.Load(fallback)
		}
	}
	resolved, _, err := resolveLibraryImports(library, false)
	return resolved, err
}

func classifierDirs(worktree string, explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	return effects.Dirs(worktree)
}

func printEffects(out io.Writer, repository, commit string, computed []policy.Effect) {
	fmt.Fprintf(out, "repository: %s  commit: %s\n", repository, shortCommit(commit))
	fmt.Fprintf(out, "effects: %d\n", len(computed))
	counts := effects.Counts(computed)
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		fmt.Fprintf(out, "  %-16s %d\n", kind, counts[policy.EffectKind(kind)])
	}
	if len(computed) == 0 {
		return
	}
	fmt.Fprintln(out)
	names := []string{}
	byContext := map[string][]policy.Effect{}
	for _, effect := range computed {
		name := effect.Component
		if name == "" {
			name = "(no context)"
		}
		if _, ok := byContext[name]; !ok {
			names = append(names, name)
		}
		byContext[name] = append(byContext[name], effect)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "context %s\n", name)
		for _, file := range effectFiles(byContext[name]) {
			fmt.Fprintf(out, "  %s\n", file)
			for _, effect := range byContext[name] {
				if effect.File != file {
					continue
				}
				fmt.Fprintf(out, "    %5d  %-16s %s\n", effect.Line, effect.Kind, effectAttrs(effect))
			}
		}
	}
}

func effectFiles(effects []policy.Effect) []string {
	files := []string{}
	seen := map[string]bool{}
	for _, effect := range effects {
		if seen[effect.File] {
			continue
		}
		seen[effect.File] = true
		files = append(files, effect.File)
	}
	sort.Strings(files)
	return files
}

func effectAttrs(effect policy.Effect) string {
	keys := make([]string, 0, len(effect.Attrs))
	for key := range effect.Attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+effect.Attrs[key])
	}
	return strings.Join(parts, " ")
}

func repositoryForWorktree(worktree string) string {
	absolute, err := filepath.Abs(worktree)
	if err != nil {
		return filepath.Base(worktree)
	}
	base := filepath.Base(absolute)
	parent := filepath.Base(filepath.Dir(absolute))
	if findExampleLibrary(absolute, base) != "" {
		return base
	}
	if parent != "." && parent != string(filepath.Separator) && findExampleLibrary(absolute, parent) != "" {
		return parent
	}
	return base
}

// findExampleLibrary walks up from the worktree looking for the checkout that
// carries examples/policies/<repository>, so the repository a worktree belongs
// to does not depend on the process working directory.
func findExampleLibrary(dir, repository string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "examples", "policies", repository, policy.PoliciesPath)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// defaultCacheDir is the state directory a member repository is cloned under,
// the same one specd reads a Repository git source into.
func defaultCacheDir() string {
	if fromEnv := os.Getenv("SPECD_CACHE_DIR"); fromEnv != "" {
		return fromEnv
	}
	return ".kcp-specd/cache"
}

// memberPaths is the repeatable `--member name=path` override: it clones a
// named member from a local checkout, so an offline run reads the same commit
// the declared ref names without reaching its url.
type memberPaths map[string]string

func (m memberPaths) String() string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+m[name])
	}
	return strings.Join(parts, ",")
}

func (m memberPaths) Set(value string) error {
	name, path, ok := strings.Cut(value, "=")
	if !ok || name == "" || path == "" {
		return fmt.Errorf("--member wants name=path")
	}
	m[name] = path
	return nil
}

// memberOptions says where a member checkout is cached. An empty cache dir
// keeps the clone in a temporary directory the caller removes.
func memberOptions(cacheDir string, paths memberPaths) policyeval.MemberOptions {
	return policyeval.MemberOptions{CacheDir: cacheDir, Paths: paths}
}

// memberRefs is the members a build reads. `specctl policy test` builds
// without a cache: it renders the library and never reads another repository,
// so it pins nothing.
func memberRefs(library policy.Library, cacheDir string) []policy.Member {
	if cacheDir == "" {
		return nil
	}
	if config := library.Manifest.Submodules; config != nil && config.Members {
		// In an org root a member with no url only overlays roles on a
		// submodule; the gitlink is its pin, so nothing is cloned or locked
		// here and the member list is derived where a checkout is evaluated.
		members := []policy.Member{}
		for _, member := range library.Manifest.Members {
			if member.URL != "" {
				members = append(members, member)
			}
		}
		return members
	}
	return library.Manifest.Members
}

// lockOf is the lock a library carries, for the member pins a build verifies.
func lockOf(library policy.Library) *policy.PackLock {
	data, ok := library.Files[policy.LockPath]
	if !ok {
		return nil
	}
	lock, err := policyeval.ParseLock(data)
	if err != nil {
		return nil
	}
	return &lock
}

func cleanupMembers(members []policyeval.ResolvedMember) {
	for _, member := range members {
		member.Cleanup()
	}
}

func marshalObject(value any) []byte {
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func typeMeta(kind string) metav1.TypeMeta {
	return metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: kind}
}

func objectMeta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: specapi.DefaultNamespace}
}

func defaultOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func printReport(out io.Writer, report policy.Report) {
	printReportDecision(out, report, nil)
}

// printReportDecision is printReport with the decision's durable exceptions
// folded in: a waived violation is printed as waived and not counted as a deny,
// so the offline evaluation reads the same way the gates do.
func printReportDecision(out io.Writer, report policy.Report, waived []policy.Violation) {
	waivedKeys := map[string]bool{}
	for _, violation := range waived {
		waivedKeys[policy.Key(violation)] = true
	}
	fmt.Fprintf(out, "repository: %s  commit: %s\n", report.Repository, shortCommit(report.Commit))
	fmt.Fprintf(out, "templates: %d  constraints: %d\n", report.Templates, report.Constraints)
	for _, member := range report.Members {
		fmt.Fprintf(out, "member: %s %s at %s\n", member.Name, member.Ref, shortCommit(member.Commit))
	}
	for _, row := range report.MemberPolicies {
		switch {
		case row.Message != "":
			fmt.Fprintf(out, "member policy: %s at %s: not evaluated: %s\n", row.Name, shortCommit(row.Commit), row.Message)
		default:
			fmt.Fprintf(out, "member policy: %s at %s: %d violation(s) (deny %d, warn %d)\n", row.Name, shortCommit(row.Commit),
				len(row.Violations), row.Totals[policy.EnforcementDeny], row.Totals[policy.EnforcementWarn])
		}
	}
	denies, waivedCount := 0, 0
	for _, violation := range report.Violations {
		if !waivedKeys[policy.Key(violation)] {
			if violation.Enforcement == policy.EnforcementDeny {
				denies++
			}
			continue
		}
		waivedCount++
	}
	fmt.Fprintf(out, "violations: %d (deny %d, warn %d, dryrun %d, waived %d)\n",
		len(report.Violations),
		denies,
		report.Totals[policy.EnforcementWarn],
		report.Totals[policy.EnforcementDryRun],
		waivedCount,
	)
	if len(report.Violations) == 0 {
		fmt.Fprintln(out, "clean")
		return
	}
	fmt.Fprintln(out)
	for _, violation := range report.Violations {
		action := string(violation.Enforcement)
		if waivedKeys[policy.Key(violation)] {
			action = "waived"
		}
		fmt.Fprintf(out, "%-8s %-10s %s  %s\n", action, violation.Severity, violation.Constraint, violation.Object)
		if violation.Location != nil {
			fmt.Fprintf(out, "         %s:%d\n", violation.Location.File, violation.Location.Line)
		}
		fmt.Fprintf(out, "         %s\n", violation.Msg)
	}
}

func loadContexts(ctx context.Context, path, repository, branch, defaultBranch string) []spec.SystemContext {
	store := oagit.Store{Repo: path}
	commit, err := store.Tip(ctx, oabranch.RefFor(repository, branch, defaultBranch))
	if err != nil || commit == "" {
		return nil
	}
	files, err := store.ReadFiles(ctx, commit)
	if err != nil {
		return nil
	}
	contexts, err := oabranch.SpecFiles(files)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(contexts))
	for name := range contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]spec.SystemContext, 0, len(names))
	for _, name := range names {
		context := contexts[name]
		context.TypeMeta = typeMeta(specapi.SystemContextKind)
		out = append(out, context)
	}
	return out
}

func exportCommit(ctx context.Context, repo, commit string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "specctl-policy-eval-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	archive := exec.CommandContext(ctx, "git", "-C", repo, "archive", commit)
	tar := exec.CommandContext(ctx, "tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	tar.Stdin = pipe
	if err := tar.Start(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := archive.Run(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("git archive %s: %w", commit, err)
	}
	if err := tar.Wait(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("tar: %w", err)
	}
	return dir, cleanup, nil
}
