package policyeval

import (
	"context"

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
	for _, object := range evaluation.Reviewed {
		report.Reviewed = append(report.Reviewed, ObjectRefOf(object))
		violations, err := engine.Review(ctx, object)
		if err != nil {
			return policy.Report{}, err
		}
		report.Violations = append(report.Violations, violations...)
	}
	report.Sort()
	report.Tally()
	return report, nil
}

func EvaluateLibrary(ctx context.Context, library policy.Library, reviewed, inventory []*unstructured.Unstructured) (policy.Report, error) {
	return Evaluate(ctx, Evaluation{
		Library:    library,
		Repository: library.Manifest.Repository,
		Reviewed:   reviewed,
		Inventory:  inventory,
	})
}
