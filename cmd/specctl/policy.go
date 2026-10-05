package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codediff"
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
	case "apply":
		return runPolicyApply(rest, stdout, stderr)
	case "ls":
		return runPolicyLs(rest, stdout, stderr)
	case "report":
		return runPolicyReport(rest, stdout, stderr)
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
      [--with-library]
      create the policy tree: policies.yaml, lib/specd.rego, lib/specd_test.rego.
      --dir writes a plain directory; --path writes the orphan branch
      open-policy/X[--<branch slug>]; --with-library also copies the embedded
      policy library: the ported change-integrity, spec-structure, provisioning
      and disabled-verification templates with their constraints and gator suites
  specctl policy new <name> --kind <Kind> [--title T] [--level MUST] [--pattern P]
      [--dir D | --path <git repo>] [--repo X]
      scaffold src.rego, src_test.rego, template.yaml, the constraint and a
      gator suite (one allowed case, one denied case)
  specctl policy build [--dir D | --path <git repo> --repo X]
      write dist/, refresh lib/specd.rego, render CATALOGUE.md
  specctl policy test [--dir D] [--gator] [--gator-bin <path>]
      opa unit tests and gator suites through the built-in engine; --gator also
      runs the real gator binary
  specctl policy eval --repo X [--worktree P | --commit C] [--path <git repo>]
      [--library D] [--branch B] [--diff-base REF] [--test-glob G]
      [-o text|json] [--strict]
      one-off audit of a checkout or a commit against the policy branch;
      --diff-base also derives a CodeDiff between REF and the evaluated commit,
      so the provisioning and disabled-verification templates have an object
      to review
  specctl policy effects [--worktree P | --commit C] [--repo X] [--path <git repo>]
      [--classifiers DIR] [--kind K] [--no-extras] [-o text|json]
      classify the code into the fixed effect vocabulary (net.dial, ssh.connect,
      proc.exec, http.handle, ...) and print the effects grouped by
      context and file. Classifier packs ship with specctl; a repository adds
      its own in <worktree>/classifiers/*.yaml or in --classifiers DIR
  specctl policy model [--worktree P | --commit C] [--repo X] [--path <git repo>]
      [--library D] [--classifiers DIR] [--test-glob G] [-o text|json]
      build the ArchitectureModel (components, roles, effects, flows, triggers)
      from the CodeGraph, the effects, the SystemContexts and the roles and
      vocabulary of policies.yaml
  specctl policy apply -f F | --library D [--prune]
      write ConstraintTemplates, their constraint CRDs and their constraints
      into kcp
  specctl policy restore --repo X [--path <git repo>] [--branch B] [--prune]
      load the policy branch open-policy/X[--<branch slug>] into kcp; specd
      does the same when a Repository is created
  specctl policy ls [-o table|json|name]
      the templates and constraints kcp holds
  specctl policy report --repo X [-o text|json]
      the last audit: the policy commit, the evaluated commit, the totals and
      the first violations of Repository.status.policy

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

func (t policyTarget) load(ctx context.Context) (policy.Library, error) {
	if !t.onBranch() {
		return policyeval.Load(t.dir)
	}
	store := oagit.Store{Repo: t.path}
	library, _, err := policygit.Read(ctx, store, t.ref())
	return library, err
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
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	target.resolveRepo()
	if target.onBranch() && !flagSet(fs, "repo") {
		fmt.Fprintln(stderr, "specctl policy init: --repo is required when writing a policy branch")
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
	doc, err := yaml.Marshal(manifest)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
		return exitError
	}
	add := map[string][]byte{
		policy.PoliciesPath:      doc,
		policy.LibPath:           []byte(policyeval.Lib()),
		policy.LibTestPath:       []byte(policyeval.LibTest()),
		policy.GitAttributesPath: []byte(policygit.GitAttributes),
	}
	copied := 0
	if *withLibrary {
		files, err := specdlib.Files()
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy init: %v\n", err)
			return exitError
		}
		maps.Copy(add, files)
		copied = len(files)
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
	if *withLibrary {
		fmt.Fprintf(stdout, "library: %d files copied; run specctl policy build to render dist/ and CATALOGUE.md\n", copied)
	}
	return exitOK
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
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	target.resolveRepo()
	if target.onBranch() && !flagSet(fs, "repo") {
		fmt.Fprintln(stderr, "specctl policy build: --repo is required when writing a policy branch")
		return exitUsage
	}
	return buildTarget(context.Background(), target, stdout, stderr)
}

func buildTarget(ctx context.Context, target *policyTarget, stdout, stderr io.Writer) int {
	library, err := target.load(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
		return exitError
	}
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

	message := fmt.Sprintf("policy(%s): build\n", target.repo)
	if _, err := target.apply(ctx, add, nil, message); err != nil {
		fmt.Fprintf(stderr, "specctl policy build: %v\n", err)
		return exitError
	}
	sort.Strings(written)
	for _, path := range written {
		fmt.Fprintln(stdout, path)
	}
	return exitOK
}

func runPolicyTest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := addPolicyTargetFlags(fs)
	withGator := fs.Bool("gator", false, "also run the real gator binary over the suites")
	gatorBin := fs.String("gator-bin", "", "path to gator (default $SPECD_GATOR or bin/gator)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if target.onBranch() {
		fmt.Fprintln(stderr, "specctl policy test: --dir is required; test a checkout of the policy branch")
		return exitUsage
	}
	target.resolveRepo()
	ctx := context.Background()
	if code := buildTarget(ctx, target, stdout, stderr); code != exitOK {
		return code
	}

	library, err := policyeval.Load(target.dir)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
		return exitError
	}

	failed := 0
	modules := map[string]string{policy.LibPath: policyeval.Lib(), policy.LibTestPath: policyeval.LibTest()}
	for _, template := range library.Templates {
		slug := policy.TemplateSlug(template)
		source, err := os.ReadFile(filepath.Join(target.dir, filepath.FromSlash(policy.TemplateSourcePath(slug))))
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
			return exitError
		}
		modules[policy.TemplateSourcePath(slug)] = string(source)
		if test, ok := policyeval.TemplateTest(os.DirFS(target.dir), slug); ok {
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

	suites, err := filepath.Glob(filepath.Join(target.dir, policy.TestsDir, "*", policy.SuiteName))
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
		return exitError
	}
	sort.Strings(suites)
	passedCases, totalCases := 0, 0
	for _, suite := range suites {
		relative, err := filepath.Rel(target.dir, suite)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy test: %v\n", err)
			return exitError
		}
		result, err := policyeval.RunSuite(ctx, os.DirFS(target.dir), filepath.ToSlash(relative))
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
		bin := *gatorBin
		if bin == "" {
			bin = os.Getenv("SPECD_GATOR")
		}
		if bin == "" {
			bin = defaultGator
		}
		if code := runGator(bin, filepath.Join(target.dir, policy.TestsDir), stdout, stderr); code != exitOK {
			failed++
		}
	}

	if failed > 0 {
		return exitError
	}
	return exitOK
}

func runGator(bin, suites string, stdout, stderr io.Writer) int {
	if _, err := os.Stat(bin); err != nil {
		fmt.Fprintf(stderr, "specctl policy test: %s not found; run scripts/install-policy-tools.sh\n", bin)
		return exitError
	}
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
	output := fs.String("o", "text", "text or json")
	strict := fs.Bool("strict", false, "exit 1 when a deny violation survives the cap")
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

	contexts := loadContexts(ctx, *path, *repository, *branch, *defaultBranch)
	graph, err := codegraphfacts.Build(ctx, codeDir, codegraphfacts.Options{
		Repository: *repository,
		Branch:     *branch,
		Commit:     resolved,
		TestGlobs:  testGlobs,
		Contexts:   codegraphfacts.ContextsByFile(contexts),
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	computed, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: classifierDirs(codeDir, nil),
		IncludeExtras:   true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}

	inventory := []*unstructured.Unstructured{}
	graphObject, err := policyeval.Unstructured(marshalObject(graph))
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	inventory = append(inventory, graphObject)
	reviewed := []*unstructured.Unstructured{graphObject}
	if *diffBase != "" {
		diffRepo := codeDir
		if *commit != "" {
			diffRepo = *path
		}
		diff, err := codediff.Build(ctx, diffRepo, *diffBase, resolved)
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		diffObject, err := policyeval.Unstructured(marshalObject(policy.CodeDiffObject(diff)))
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		reviewed = append(reviewed, diffObject)
		inventory = append(inventory, diffObject)
	}

	model, err := policy.BuildModel(policy.ModelInput{
		Repository: *repository,
		Graph:      graph,
		Effects:    computed,
		Contexts:   modelContexts(contexts),
		Binding:    library.Manifest.Binding(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	modelObject, err := policyeval.Unstructured(marshalObject(model))
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	inventory = append(inventory, modelObject)
	reviewed = append(reviewed, modelObject)
	for _, context := range contexts {
		object, err := policyeval.Unstructured(marshalObject(context))
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		inventory = append(inventory, object)
	}
	repositoryObject, err := policyeval.Unstructured(marshalObject(spec.Repository{
		TypeMeta:   typeMeta(specapi.RepositoryKind),
		ObjectMeta: objectMeta(*repository),
		Spec:       spec.RepositorySpec{Branch: defaultOr(*branch, *defaultBranch)},
	}))
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}
	inventory = append(inventory, repositoryObject)

	report, err := policyeval.Evaluate(ctx, policyeval.Evaluation{
		Library:    library,
		Repository: *repository,
		Commit:     resolved,
		Reviewed:   reviewed,
		Inventory:  inventory,
	})
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
		return exitError
	}

	if *output == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy eval: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
	} else {
		printReport(stdout, report)
	}
	if *strict {
		decision := policy.Decide(report, policy.RepositoryPolicy{}, nil)
		if decision.Blocked {
			return exitError
		}
	}
	return exitOK
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
		Repository: *repository,
		Branch:     *branch,
		Commit:     resolved,
		TestGlobs:  testGlobs,
		Contexts:   codegraphfacts.ContextsByFile(contexts),
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
	if len(library.Templates) > 0 || repository == "" {
		return library, nil
	}
	fallback := filepath.Join("examples", "policies", repository)
	if _, statErr := os.Stat(filepath.Join(fallback, policy.PoliciesPath)); statErr == nil {
		return policyeval.Load(fallback)
	}
	return library, nil
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
	if hasExampleLibrary(base) {
		return base
	}
	if parent != "." && parent != string(filepath.Separator) && hasExampleLibrary(parent) {
		return parent
	}
	return base
}

func hasExampleLibrary(repository string) bool {
	_, err := os.Stat(filepath.Join("examples", "policies", repository, policy.PoliciesPath))
	return err == nil
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
	fmt.Fprintf(out, "repository: %s  commit: %s\n", report.Repository, shortCommit(report.Commit))
	fmt.Fprintf(out, "templates: %d  constraints: %d\n", report.Templates, report.Constraints)
	fmt.Fprintf(out, "violations: %d (deny %d, warn %d, dryrun %d)\n",
		len(report.Violations),
		report.Totals[policy.EnforcementDeny],
		report.Totals[policy.EnforcementWarn],
		report.Totals[policy.EnforcementDryRun],
	)
	if len(report.Violations) == 0 {
		fmt.Fprintln(out, "clean")
		return
	}
	fmt.Fprintln(out)
	for _, violation := range report.Violations {
		fmt.Fprintf(out, "%-8s %-10s %s  %s\n", violation.Enforcement, violation.Severity, violation.Constraint, violation.Object)
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
