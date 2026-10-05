package policyeval

import (
	"context"
	"fmt"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
)

// ModelRequest is everything an evaluation needs to build the
// ArchitectureModel: the repository's own graph, effects and contexts, plus
// the other repositories the library names, already cloned and pinned.
type ModelRequest struct {
	Repository string

	Graph policy.CodeGraph

	Effects []policy.Effect

	Contexts []policy.ModelContext

	Library policy.Library

	Members []ResolvedMember

	Tool string
}

// BuildEvaluationModel builds the ArchitectureModel of the repository and of
// every member, and returns the member pins the report records. The member's
// own roles merge into the library's binding, so a portable rule reads the
// same abstract roles across repositories.
func BuildEvaluationModel(ctx context.Context, request ModelRequest) (policy.ArchitectureModel, []policy.ReportMember, error) {
	models := make([]policy.ModelMember, 0, len(request.Members))
	pins := make([]policy.ReportMember, 0, len(request.Members))
	for _, member := range request.Members {
		built, err := memberModel(ctx, member, request)
		if err != nil {
			return policy.ArchitectureModel{}, nil, err
		}
		models = append(models, built)
		pins = append(pins, member.Report())
	}
	model, err := policy.BuildModel(policy.ModelInput{
		Repository: request.Repository,
		Graph:      request.Graph,
		Effects:    request.Effects,
		Contexts:   request.Contexts,
		Binding:    request.Library.Manifest.Binding(),
		Members:    models,
	})
	if err != nil {
		return policy.ArchitectureModel{}, nil, err
	}
	return model, pins, nil
}

func memberModel(ctx context.Context, member ResolvedMember, request ModelRequest) (policy.ModelMember, error) {
	testGlobs := member.Member.TestGlobs
	if len(testGlobs) == 0 {
		testGlobs = request.Library.Manifest.TestGlobs
	}
	graph, err := codegraphfacts.Build(ctx, member.Dir, codegraphfacts.Options{
		Repository: member.Member.Name,
		Branch:     member.Member.Ref,
		Commit:     member.Commit,
		TestGlobs:  testGlobs,
		Tool:       request.Tool,
	})
	if err != nil {
		return policy.ModelMember{}, fmt.Errorf("policyeval: index member %s: %w", member.Member.Name, err)
	}
	computed, err := effects.Apply(&graph, effects.Options{
		ClassifiersDirs: classifierDirsFor(member),
		IncludeExtras:   true,
	})
	if err != nil {
		return policy.ModelMember{}, fmt.Errorf("policyeval: effects of member %s: %w", member.Member.Name, err)
	}
	binding := request.Library.Manifest.Binding().Merge(policy.Binding{Roles: member.Member.Roles})
	return policy.ModelMember{
		Name:    member.Member.Name,
		Graph:   graph,
		Effects: computed,
		Binding: binding,
	}, nil
}

func classifierDirsFor(member ResolvedMember) []string {
	if member.ClassifiersDir != "" {
		return []string{member.ClassifiersDir}
	}
	return nil
}
