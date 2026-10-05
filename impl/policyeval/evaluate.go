package policyeval

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

type Evaluation struct {
	Library policy.Library

	Repository string

	Commit string

	Reviewed []*unstructured.Unstructured

	Inventory []*unstructured.Unstructured

	Libs []string
}

func Evaluate(ctx context.Context, evaluation Evaluation) (policy.Report, error) {
	engine, err := NewEngine(ctx, evaluation.Library, evaluation.Libs)
	if err != nil {
		return policy.Report{}, err
	}
	if err := engine.AddInventory(ctx, evaluation.Inventory); err != nil {
		return policy.Report{}, err
	}

	report := policy.Report{
		Repository:  evaluation.Repository,
		Commit:      evaluation.Commit,
		Templates:   len(evaluation.Library.Templates),
		Constraints: len(evaluation.Library.Constraints),
		Reviewed:    []policy.ObjectRef{},
		Violations:  []policy.Violation{},
	}
	graph, haveGraph := codeGraphOf(evaluation.Reviewed, evaluation.Inventory)
	for _, object := range evaluation.Reviewed {
		report.Reviewed = append(report.Reviewed, ObjectRefOf(object))
		violations, err := engine.Review(ctx, object)
		if err != nil {
			return policy.Report{}, err
		}
		if haveGraph {
			for index := range violations {
				violations[index] = policy.Anchor(violations[index], graph)
			}
		}
		report.Violations = append(report.Violations, violations...)
	}
	report.Sort()
	report.Tally()
	return report, nil
}

// codeGraphOf finds the CodeGraph of an evaluation, so a violation's site can
// be anchored to the declaration that encloses it rather than to a line that
// moves. The graph is a reviewed object or in inventory, depending on who
// called.
func codeGraphOf(lists ...[]*unstructured.Unstructured) (policy.CodeGraph, bool) {
	for _, objects := range lists {
		for _, object := range objects {
			if object.GetKind() != policy.CodeGraphKind {
				continue
			}
			encoded, err := json.Marshal(object.Object)
			if err != nil {
				continue
			}
			var graph policy.CodeGraph
			if err := json.Unmarshal(encoded, &graph); err != nil {
				continue
			}
			return graph, true
		}
	}
	return policy.CodeGraph{}, false
}

func EvaluateLibrary(ctx context.Context, library policy.Library, reviewed, inventory []*unstructured.Unstructured) (policy.Report, error) {
	return Evaluate(ctx, Evaluation{
		Library:    library,
		Repository: library.Manifest.Repository,
		Reviewed:   reviewed,
		Inventory:  inventory,
	})
}
