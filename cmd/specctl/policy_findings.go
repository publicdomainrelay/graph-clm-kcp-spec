package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codediff"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

// codeEvaluation is one offline evaluation of a checkout: the same inputs
// `policy eval`, `policy findings` and `policy fix` build a report from.
type codeEvaluation struct {
	Repository string

	Branch string

	DefaultBranch string

	Path string

	CodeDir string

	Commit string

	Library policy.Library

	TestGlobs []string

	IndexInPlace bool

	DiffBase string

	CacheDir string

	Members memberPaths
}

func evaluateCode(ctx context.Context, e codeEvaluation) (policy.Report, error) {
	contexts := loadContexts(ctx, e.Path, e.Repository, e.Branch, e.DefaultBranch)
	graph, err := codegraphfacts.Build(ctx, e.CodeDir, codegraphfacts.Options{
		Repository:   e.Repository,
		Branch:       e.Branch,
		Commit:       e.Commit,
		TestGlobs:    e.TestGlobs,
		Contexts:     codegraphfacts.ContextsByFile(contexts),
		IndexInPlace: e.IndexInPlace,
	})
	if err != nil {
		return policy.Report{}, err
	}
	libraryClassifiers, classifierCleanup, err := policyeval.LibraryClassifierDirs(e.Library, e.CodeDir, "")
	if err != nil {
		return policy.Report{}, err
	}
	defer classifierCleanup()
	computed, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: classifierDirs(e.CodeDir, libraryClassifiers),
		IncludeExtras:   true,
	})
	if err != nil {
		return policy.Report{}, err
	}

	inventory := []*unstructured.Unstructured{}
	graphObject, err := policy.Unstructured(marshalObject(graph))
	if err != nil {
		return policy.Report{}, err
	}
	inventory = append(inventory, graphObject)
	reviewed := []*unstructured.Unstructured{graphObject}
	if e.DiffBase != "" {
		diff, err := codediff.Build(ctx, e.CodeDir, e.DiffBase, e.Commit)
		if err != nil {
			return policy.Report{}, err
		}
		diff.Spec.Repository = e.Repository
		diffObject, err := policy.Unstructured(marshalObject(policy.CodeDiffObject(diff)))
		if err != nil {
			return policy.Report{}, err
		}
		reviewed = append(reviewed, diffObject)
		inventory = append(inventory, diffObject)
	}

	members, err := policyeval.ResolveMembers(ctx, e.Library.Manifest.Members, e.Library, memberOptions(e.CacheDir, e.Members))
	if err != nil {
		return policy.Report{}, err
	}
	defer cleanupMembers(members)
	model, memberPins, err := policyeval.BuildEvaluationModel(ctx, policyeval.ModelRequest{
		Repository: e.Repository,
		Graph:      graph,
		Effects:    computed,
		Contexts:   modelContexts(contexts),
		Library:    e.Library,
		Members:    members,
	})
	if err != nil {
		return policy.Report{}, err
	}
	modelObject, err := policy.Unstructured(marshalObject(model))
	if err != nil {
		return policy.Report{}, err
	}
	inventory = append(inventory, modelObject)
	reviewed = append(reviewed, modelObject)
	for _, context := range contexts {
		object, err := policy.Unstructured(marshalObject(context))
		if err != nil {
			return policy.Report{}, err
		}
		inventory = append(inventory, object)
	}
	repositoryObject, err := policy.Unstructured(marshalObject(spec.Repository{
		TypeMeta:   typeMeta(specapi.RepositoryKind),
		ObjectMeta: objectMeta(e.Repository),
		Spec:       spec.RepositorySpec{Branch: defaultOr(e.Branch, e.DefaultBranch)},
	}))
	if err != nil {
		return policy.Report{}, err
	}
	inventory = append(inventory, repositoryObject)

	report, err := policyeval.Evaluate(ctx, policyeval.Evaluation{
		Library:    e.Library,
		Repository: e.Repository,
		Commit:     e.Commit,
		Reviewed:   reviewed,
		Inventory:  inventory,
	})
	if err != nil {
		return policy.Report{}, err
	}
	report.Members = memberPins
	return report, nil
}

// Finding is one violation with the three things a decision needs: which rule,
// which object, which site -- and its stable key, its status against the base
// and the durable exceptions, and the message.
type Finding struct {
	Key string `json:"key"`

	Status string `json:"status"`

	Constraint string `json:"constraint"`

	Policy string `json:"policy,omitempty"`

	Level string `json:"level,omitempty"`

	Object string `json:"object"`

	Site string `json:"site,omitempty"`

	Enforcement string `json:"enforcementAction,omitempty"`

	Message string `json:"msg"`

	Reason string `json:"reason,omitempty"`

	Owner string `json:"owner,omitempty"`
}

const (
	findingNew = "new"

	findingInherited = "inherited"

	findingWaived = "waived"
)

func findingsOf(report, base policy.Report, waivers []policy.Override, haveBase bool) []Finding {
	existing := map[string]bool{}
	if haveBase {
		existing = policy.BaselineKeys(base)
	}
	out := make([]Finding, 0, len(report.Violations))
	for _, violation := range report.Violations {
		key := policy.Key(violation)
		finding := Finding{
			Key:         key,
			Status:      findingNew,
			Constraint:  violation.Constraint,
			Policy:      violation.Policy,
			Level:       string(violation.Level),
			Object:      violation.Object.String(),
			Enforcement: string(violation.Enforcement),
			Message:     violation.Msg,
		}
		if file, line := policy.Site(violation); file != "" {
			finding.Site = fmt.Sprintf("%s:%d", file, line)
		}
		if waiver, ok := waiverFor(waivers, violation); ok {
			finding.Status = findingWaived
			finding.Reason = waiver.Reason
			finding.Owner = waiver.By
		} else if haveBase && existing[key] {
			finding.Status = findingInherited
			finding.Enforcement = string(policy.EnforcementWarn)
		}
		out = append(out, finding)
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Constraint != out[right].Constraint {
			return out[left].Constraint < out[right].Constraint
		}
		return out[left].Site < out[right].Site
	})
	return out
}

func waiverFor(waivers []policy.Override, violation policy.Violation) (policy.Override, bool) {
	for _, waiver := range waivers {
		if waiver.Matches(violation) {
			return waiver, true
		}
	}
	return policy.Override{}, false
}

type findingsOptions struct {
	target policyTarget

	worktree string

	commit string

	libraryDir string

	base string

	cacheDir string

	memberPaths memberPaths

	testGlobs stringsFlag

	output string
}

func addFindingsFlags(fs *flag.FlagSet, options *findingsOptions) *policyTarget {
	target := addPolicyTargetFlags(fs)
	fs.StringVar(&options.worktree, "worktree", "", "code checkout to index and evaluate")
	fs.StringVar(&options.commit, "commit", "", "commit of --path to export and evaluate")
	fs.StringVar(&options.libraryDir, "library", "", "read the policy library from this directory instead of the branch")
	fs.StringVar(&options.base, "base", "", "ref the change is on top of; a violation the base carried is inherited")
	fs.StringVar(&options.cacheDir, "cache-dir", defaultCacheDir(), "where a member repository is cloned")
	fs.Var(&options.memberPaths, "member", "clone the named member from a local path instead of its url (name=path); repeatable")
	fs.Var(&options.testGlobs, "test-glob", "test file glob; repeatable")
	fs.StringVar(&options.output, "o", "text", "text or json")
	return target
}

// resolve reads the library and the two reports: the head, and the base when a
// base ref is given.
func (o *findingsOptions) resolve(ctx context.Context) (policy.Library, policy.Report, *policy.Report, []policy.Override, []policy.Exception, error) {
	if o.target.repo == "" && o.worktree != "" {
		o.target.repo = repositoryForWorktree(o.worktree)
	}
	o.target.resolveRepo()
	if o.target.repo == "" {
		return policy.Library{}, policy.Report{}, nil, nil, nil, fmt.Errorf("--repo is required")
	}
	if o.worktree != "" && o.commit != "" {
		return policy.Library{}, policy.Report{}, nil, nil, nil, fmt.Errorf("--worktree and --commit are exclusive")
	}
	library, err := loadLibrary(ctx, o.target.repo, o.libraryDir, o.target.path, o.target.branch, o.target.defaultBranch)
	if err != nil {
		return policy.Library{}, policy.Report{}, nil, nil, nil, err
	}
	if len(library.Templates) == 0 {
		return policy.Library{}, policy.Report{}, nil, nil, nil, fmt.Errorf("no policies for %s; run specctl policy init --repo %s", o.target.repo, o.target.repo)
	}
	testGlobs := o.testGlobs
	if len(testGlobs) == 0 {
		testGlobs = library.Manifest.TestGlobs
	}

	codeDir := o.worktree
	resolved := ""
	cleanup := func() {}
	if o.commit != "" {
		codeDir, cleanup, err = exportCommit(ctx, o.target.path, o.commit)
		if err != nil {
			return policy.Library{}, policy.Report{}, nil, nil, nil, err
		}
		resolved = o.commit
	} else if codeDir == "" {
		codeDir = o.target.path
	}
	defer cleanup()
	if absolute, err := filepath.Abs(codeDir); err == nil {
		codeDir = absolute
	}
	if resolved == "" {
		resolved = codegraphfacts.GitCommit(ctx, codeDir)
	}

	report, err := evaluateCode(ctx, codeEvaluation{
		Repository:    o.target.repo,
		Branch:        o.target.branch,
		DefaultBranch: o.target.defaultBranch,
		Path:          o.target.path,
		CodeDir:       codeDir,
		Commit:        resolved,
		Library:       library,
		TestGlobs:     testGlobs,
		DiffBase:      o.base,
		CacheDir:      o.cacheDir,
		Members:       o.memberPaths,
	})
	if err != nil {
		return policy.Library{}, policy.Report{}, nil, nil, nil, err
	}
	waivers, expired := policyeval.Waivers(library, nil, time.Now())
	if o.base == "" {
		return library, report, nil, waivers, expired, nil
	}
	baseDir, baseCleanup, err := exportCommit(ctx, codeDir, o.base)
	if err != nil {
		return policy.Library{}, policy.Report{}, nil, nil, nil, err
	}
	defer baseCleanup()
	if absolute, err := filepath.Abs(baseDir); err == nil {
		baseDir = absolute
	}
	baseReport, err := evaluateCode(ctx, codeEvaluation{
		Repository:    o.target.repo,
		Branch:        o.target.branch,
		DefaultBranch: o.target.defaultBranch,
		Path:          o.target.path,
		CodeDir:       baseDir,
		Commit:        o.base,
		Library:       library,
		TestGlobs:     testGlobs,
		CacheDir:      o.cacheDir,
		Members:       o.memberPaths,
	})
	if err != nil {
		return policy.Library{}, policy.Report{}, nil, nil, nil, err
	}
	return library, report, &baseReport, waivers, expired, nil
}

// splitPositional pulls the one positional argument -- the finding key -- out
// of an argument list, so `policy waive <key> --reason R` parses whether the
// key comes before or after the flags. Every flag of the two subcommands takes
// a value, so the scan can skip each flag's value.
func splitPositional(args []string) (string, []string) {
	rest := make([]string, 0, len(args))
	key := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if key == "" && !strings.HasPrefix(arg, "-") {
			key = arg
			continue
		}
		rest = append(rest, arg)
		if key == "" && strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") {
			index++
			if index < len(args) {
				rest = append(rest, args[index])
			}
		}
	}
	return key, rest
}

func runPolicyFindings(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy findings", flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := findingsOptions{}
	target := addFindingsFlags(fs, &options)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	options.target = *target

	library, report, base, waivers, expired, err := options.resolve(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy findings: %v\n", err)
		return exitError
	}
	findings := findingsOf(report, derefReport(base), waivers, base != nil)
	for _, exception := range expired {
		fmt.Fprintf(stderr, "specctl policy findings: the exception for %s expired %s and is not honoured\n",
			exception.Constraint, exception.Expires)
	}
	stale := policy.StaleExceptions(library.Manifest.Exceptions, report.Violations, time.Now())
	for _, exception := range stale {
		fmt.Fprintf(stderr, "specctl policy findings: the exception %s for %s matches no violation; remove %s\n",
			exception.Key, exception.Constraint, filepath.ToSlash(filepath.Join(policy.ExceptionsDir, exception.Key+".yaml")))
	}
	if options.output == "json" {
		encoded, err := json.MarshalIndent(findings, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "specctl policy findings: %v\n", err)
			return exitError
		}
		fmt.Fprintln(stdout, string(encoded))
		return exitOK
	}
	printFindings(stdout, findings)
	return exitOK
}

func printFindings(stdout io.Writer, findings []Finding) {
	counts := map[string]int{}
	for _, finding := range findings {
		counts[finding.Status]++
	}
	fmt.Fprintf(stdout, "%-16s %-9s %-24s %-28s %s\n", "key", "status", "constraint", "site", "message")
	for _, finding := range findings {
		fmt.Fprintf(stdout, "%-16s %-9s %-24s %-28s %s\n",
			finding.Key, finding.Status, finding.Constraint, defaultOr(finding.Site, "-"), firstLine(finding.Message))
		if finding.Status == findingWaived && finding.Reason != "" {
			fmt.Fprintf(stdout, "%-16s %-9s reason: %s\n", "", "", finding.Reason)
		}
	}
	fmt.Fprintf(stdout, "findings: %d new, %d inherited, %d waived\n",
		counts[findingNew], counts[findingInherited], counts[findingWaived])
}

func firstLine(message string) string {
	if index := strings.IndexByte(message, '\n'); index >= 0 {
		return message[:index]
	}
	return message
}

func derefReport(report *policy.Report) policy.Report {
	if report == nil {
		return policy.Report{}
	}
	return *report
}

func runPolicyWaive(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy waive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := findingsOptions{}
	target := addFindingsFlags(fs, &options)
	reason := fs.String("reason", "", "why the violation is accepted")
	owner := fs.String("owner", "", "who accepted it")
	expires := fs.String("expires", "", "optional expiry, RFC3339 or YYYY-MM-DD")
	key, flags := splitPositional(args)
	if err := fs.Parse(flags); err != nil {
		return exitUsage
	}
	options.target = *target
	if key == "" {
		fmt.Fprintln(stderr, "specctl policy waive: a finding key is required")
		return exitUsage
	}
	if *reason == "" {
		fmt.Fprintln(stderr, "specctl policy waive: --reason is required; a waiver without a reason is not recorded")
		return exitUsage
	}
	if !policy.ValidExpiry(*expires) {
		fmt.Fprintf(stderr, "specctl policy waive: --expires %q is not an RFC3339 timestamp or a YYYY-MM-DD date\n", *expires)
		return exitUsage
	}

	ctx := context.Background()
	library, report, _, _, _, err := options.resolve(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy waive: %v\n", err)
		return exitError
	}
	violation, ok := violationByKey(report, key)
	if !ok {
		fmt.Fprintf(stderr, "specctl policy waive: no finding with key %s; run specctl policy findings --repo %s\n", key, options.target.repo)
		return exitError
	}
	file, line := policy.Site(violation)
	exception := policy.Exception{
		Constraint: violation.Constraint,
		Key:        key,
		Object:     violation.Object.String(),
		File:       file,
		Line:       line,
		Reason:     *reason,
		Owner:      *owner,
		Expires:    *expires,
	}
	document, err := yaml.Marshal(exception)
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy waive: %v\n", err)
		return exitError
	}
	name := filepath.ToSlash(filepath.Join(policy.ExceptionsDir, key+".yaml"))
	if _, err := options.target.apply(ctx, map[string][]byte{name: document}, nil,
		fmt.Sprintf("policy(%s): waive %s at %s\n", library.Manifest.Repository, violation.Constraint, defaultOr(file, "the model"))); err != nil {
		fmt.Fprintf(stderr, "specctl policy waive: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "waived %s at %s: wrote %s\n", violation.Constraint, defaultOr(file, "the model"), name)
	fmt.Fprintf(stdout, "run specctl policy build --dir %s (or without --dir) to render, then re-run policy findings\n", defaultOr(options.target.dir, options.target.path))
	return exitOK
}

func violationByKey(report policy.Report, key string) (policy.Violation, bool) {
	for _, violation := range report.Violations {
		if policy.Key(violation) == key {
			return violation, true
		}
	}
	return policy.Violation{}, false
}

func runPolicyFix(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specctl policy fix", flag.ContinueOnError)
	fs.SetOutput(stderr)
	options := findingsOptions{}
	target := addFindingsFlags(fs, &options)
	write := fs.String("write", "", "write the request to this file instead of stdout")
	key, flags := splitPositional(args)
	if err := fs.Parse(flags); err != nil {
		return exitUsage
	}
	options.target = *target
	if key == "" {
		fmt.Fprintln(stderr, "specctl policy fix: a finding key is required")
		return exitUsage
	}

	library, report, _, _, _, err := options.resolve(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy fix: %v\n", err)
		return exitError
	}
	violation, ok := violationByKey(report, key)
	if !ok {
		fmt.Fprintf(stderr, "specctl policy fix: no finding with key %s; run specctl policy findings --repo %s\n", key, options.target.repo)
		return exitError
	}
	request := policy.BuildFixRequest(library, violation)
	if options.output == "text" {
		fmt.Fprintln(stdout, request.Prompt)
		return exitOK
	}
	var document []byte
	if options.output == "json" {
		document, err = json.MarshalIndent(request, "", "  ")
	} else {
		document, err = yaml.Marshal(request)
	}
	if err != nil {
		fmt.Fprintf(stderr, "specctl policy fix: %v\n", err)
		return exitError
	}
	if *write == "" {
		fmt.Fprint(stdout, string(document))
		if !strings.HasSuffix(string(document), "\n") {
			fmt.Fprintln(stdout)
		}
		return exitOK
	}
	if err := os.WriteFile(*write, document, 0o644); err != nil {
		fmt.Fprintf(stderr, "specctl policy fix: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "wrote the SpecChange request for %s to %s\n", violation.Constraint, *write)
	return exitOK
}
