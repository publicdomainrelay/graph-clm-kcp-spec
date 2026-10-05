package realize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
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
	diff.Spec.Base = options.Base

	graphObject, err := policyObject(graph)
	if err != nil {
		return policy.Decision{}, policy.Report{}, err
	}
	diffObject, err := policyObject(diff)
	if err != nil {
		return policy.Decision{}, policy.Report{}, err
	}

	reviewed := []*unstructured.Unstructured{graphObject, diffObject}
	reviewed = append(reviewed, gate.Reviewed...)
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
		return policy.Decision{}, policy.Report{}, fmt.Errorf("realize: evaluate the policies: %w", err)
	}
	decision := policy.Decide(report, gate.Repository, gate.Overrides)
	return decision, report, nil
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
	return graph, nil
}

func policyObject(value any) (*unstructured.Unstructured, error) {
	document, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return policyeval.Unstructured(document)
}

func policyStatusOf(decision policy.Decision) *spec.PolicyGateStatus {
	status := &spec.PolicyGateStatus{
		Denied: policyViolations(decision.Denied),
		Warned: policyViolations(decision.Warned),
		DryRun: policyViolations(decision.DryRun),
		Waived: policyViolations(decision.Waived),
		Capped: policyViolations(decision.Capped),
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
