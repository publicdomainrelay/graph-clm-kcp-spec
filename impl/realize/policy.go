package realize

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/gitrepo"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

// PolicyGateOptions is everything the realize gate needs: the library, the
// repository's cap, the operator's one-shot overrides, and the objects the
// evaluation reviews and reads besides the code graph and the diff.
type PolicyGateOptions struct {
	Library policy.Library

	Repository policy.RepositoryPolicy

	Overrides []policy.Override

	Commit string

	Branch string

	TestGlobs []string

	Contexts map[string]string

	// ModelContexts are the SystemContexts the ArchitectureModel is built
	// from: a pack rule reads their labels and their declared interactions, so
	// a realize reviews the same model an offline evaluation does.
	ModelContexts []policy.ModelContext

	// MemberCacheDir is where the repositories the library names as members are
	// cloned. Empty keeps every clone temporary.
	MemberCacheDir string

	Reviewed []*unstructured.Unstructured

	Inventory []*unstructured.Unstructured
}

type PolicyError struct {
	Decision policy.Decision

	Report policy.Report

	Messages []string
}

func (e *PolicyError) Error() string {
	kind := "policy denied"
	if len(e.Messages) == 0 {
		return kind
	}
	return kind + ": " + strings.Join(e.Messages, "; ")
}

// runPolicyGate evaluates the library against the worktree head and the
// change's diff. The SpecChange is a reviewed object, so a policy may match it,
// and it is in inventory, so a policy may read it.
//
// The gate is change-scoped: when the head denies, the base commit is
// evaluated too and only a violation the base did not carry blocks. The base
// sees the code facts of the base commit and not the diff or the SpecChanges,
// which describe the change itself and are new by definition.
func runPolicyGate(ctx context.Context, options Options, dir string) (policy.Decision, policy.Report, error) {
	gate := options.Policy
	graph, err := buildGateGraph(ctx, options, dir)
	if err != nil {
		return policy.Decision{}, policy.Report{}, err
	}
	patch, err := gitrepo.DiffWorktree(ctx, dir, options.Base)
	if err != nil {
		return policy.Decision{}, policy.Report{}, fmt.Errorf("realize: diff the worktree for the policy gate: %w", err)
	}
	diff := policy.ParseUnifiedDiff(patch)
	diff.Metadata.Name = options.Change
	diff.Spec.Change = options.Change
	diff.Spec.Repository = options.Repository.Name
	diff.Spec.Base = options.Base

	diffObject, err := policyObject(diff)
	if err != nil {
		return policy.Decision{}, policy.Report{}, err
	}
	members, err := policyeval.ResolveMembers(ctx, gate.Library.Manifest.Members, gate.Library, policyeval.MemberOptions{
		CacheDir: gate.MemberCacheDir,
		Lock:     gateLock(gate.Library),
	})
	if err != nil {
		return policy.Decision{}, policy.Report{}, fmt.Errorf("realize: resolve the policy members: %w", err)
	}
	defer func() {
		for _, member := range members {
			member.Cleanup()
		}
	}()
	changeObjects := append([]*unstructured.Unstructured{diffObject}, gate.Reviewed...)
	report, err := gateReport(ctx, options, graph, changeObjects, members)
	if err != nil {
		return policy.Decision{}, policy.Report{}, err
	}
	overrides, _ := policyeval.Waivers(gate.Library, gate.Overrides, time.Now())
	decision := policy.Decide(report, gate.Repository, overrides)
	if !decision.Blocked || options.Base == "" || !gate.Repository.BaselineEnabled() {
		return decision, report, nil
	}
	baseReport, err := baseGateReport(ctx, options, dir, members)
	if err != nil {
		return policy.Decision{}, policy.Report{}, err
	}
	return policy.DecideBaseline(report, baseReport, gate.Repository, overrides), report, nil
}

// gateReport builds the architecture model from one graph and evaluates the
// library against it. changeObjects are reviewed and in inventory besides the
// graph and the model.
func gateReport(ctx context.Context, options Options, graph policy.CodeGraph, changeObjects []*unstructured.Unstructured, members []policyeval.ResolvedMember) (policy.Report, error) {
	gate := options.Policy
	model, memberPins, err := policyeval.BuildEvaluationModel(ctx, policyeval.ModelRequest{
		Repository: options.Repository.Name,
		Graph:      graph,
		Effects:    graph.Spec.Effects,
		Contexts:   gate.ModelContexts,
		Library:    gate.Library,
		Members:    members,
		Tool:       options.Tool,
	})
	if err != nil {
		return policy.Report{}, fmt.Errorf("realize: build the architecture model: %w", err)
	}
	graphObject, err := policyObject(graph)
	if err != nil {
		return policy.Report{}, err
	}
	modelObject, err := policyObject(model)
	if err != nil {
		return policy.Report{}, err
	}
	reviewed := []*unstructured.Unstructured{graphObject, modelObject}
	reviewed = append(reviewed, changeObjects...)
	inventory := append([]*unstructured.Unstructured{}, reviewed...)
	inventory = append(inventory, gate.Inventory...)

	report, err := policyeval.Evaluate(ctx, policyeval.Evaluation{
		Library:    gate.Library,
		Repository: options.Repository.Name,
		Commit:     options.Base,
		Reviewed:   reviewed,
		Inventory:  inventory,
	})
	if err != nil {
		return policy.Report{}, fmt.Errorf("realize: evaluate the policies: %w", err)
	}
	report.Members = memberPins
	return report, nil
}

// baseGateReport evaluates the library against the code facts of the base
// commit, from a detached worktree of it. The members are the ones the head
// evaluation resolved: they are pinned, so both reports read the same ones.
func baseGateReport(ctx context.Context, options Options, dir string, members []policyeval.ResolvedMember) (policy.Report, error) {
	baseDir, cleanup, err := baseWorktree(ctx, dir, options.Base)
	if err != nil {
		return policy.Report{}, err
	}
	defer cleanup()
	graph, err := buildGateGraph(ctx, options, baseDir)
	if err != nil {
		return policy.Report{}, err
	}
	return gateReport(ctx, options, graph, nil, members)
}

// baseWorktree checks the base commit out into a temporary detached worktree,
// so the gate can read the facts of the code before the change.
func baseWorktree(ctx context.Context, dir, base string) (string, func(), error) {
	parent, err := os.MkdirTemp("", "specd-policy-base-")
	if err != nil {
		return "", func() {}, err
	}
	target := filepath.Join(parent, "tree")
	cleanup := func() {
		_ = exec.Command("git", "-C", dir, "worktree", "remove", "--force", target).Run()
		_ = os.RemoveAll(parent)
	}
	command := exec.CommandContext(ctx, "git", "-C", dir, "worktree", "add", "--detach", target, base)
	if output, err := command.CombinedOutput(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("realize: check out the policy gate base %s: %w: %s",
			base, err, strings.TrimSpace(string(output)))
	}
	return target, cleanup, nil
}

// gateLock is the member pins the library's own policies.lock carries.
func gateLock(library policy.Library) *policy.PackLock {
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

// buildGateGraph indexes the worktree head. The index lives in a .codegraph
// directory the gate removes again when it created it, so a policy never lands
// an artefact in the change it judges.
func buildGateGraph(ctx context.Context, options Options, dir string) (policy.CodeGraph, error) {
	indexDir := filepath.Join(dir, ".codegraph")
	_, statErr := os.Stat(indexDir)
	created := os.IsNotExist(statErr)
	graph, err := codegraphfacts.Build(ctx, dir, codegraphfacts.Options{
		Repository: options.Repository.Name,
		Branch:     options.Branch,
		Commit:     options.Base,
		Namespace:  options.Namespace,
		TestGlobs:  options.Policy.TestGlobs,
		Contexts:   options.Policy.Contexts,
		Tool:       options.Tool,
	})
	if created {
		_ = os.RemoveAll(indexDir)
	}
	if err != nil {
		return policy.CodeGraph{}, fmt.Errorf("realize: index the worktree for the policy gate: %w", err)
	}
	if _, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: effects.Dirs(dir),
		IncludeExtras:   true,
	}); err != nil {
		return policy.CodeGraph{}, fmt.Errorf("realize: compute the effects for the policy gate: %w", err)
	}
	return graph, nil
}

func policyObject(value any) (*unstructured.Unstructured, error) {
	document, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return policy.Unstructured(document)
}

func policyStatusOf(decision policy.Decision) *spec.PolicyGateStatus {
	status := &spec.PolicyGateStatus{
		Denied:    policyViolations(decision.Denied),
		Warned:    policyViolations(decision.Warned),
		DryRun:    policyViolations(decision.DryRun),
		Waived:    policyViolations(decision.Waived),
		Capped:    policyViolations(decision.Capped),
		Inherited: policyViolations(decision.Inherited),
	}
	if len(decision.Denied) > 0 {
		status.Message = fmt.Sprintf("%d deny violation(s)", len(decision.Denied))
	} else if len(decision.Warned) > 0 {
		status.Message = fmt.Sprintf("%d warn violation(s)", len(decision.Warned))
	}
	return status
}

// policyOverrideSteps names the overrides a decision consumed, as the
// acceptance override steps specd removes once the commit lands.
func policyOverrideSteps(decision policy.Decision) []string {
	constraints := decision.WaivedConstraints()
	out := make([]string, 0, len(constraints))
	for _, constraint := range constraints {
		out = append(out, policy.Override{Constraint: constraint}.Step())
	}
	return out
}

func policyViolations(violations []policy.Violation) []spec.PolicyViolation {
	out := make([]spec.PolicyViolation, 0, len(violations))
	for _, violation := range violations {
		entry := spec.PolicyViolation{
			Policy:      violation.Policy,
			Constraint:  violation.Constraint,
			Enforcement: string(violation.Enforcement),
			Severity:    string(violation.Severity),
			Msg:         violation.Msg,
			Object:      violation.Object.String(),
		}
		if violation.Location != nil {
			entry.File = violation.Location.File
			entry.Line = violation.Location.Line
		}
		out = append(out, entry)
	}
	return out
}
