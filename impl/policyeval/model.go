package policyeval

import (
	"context"
	"fmt"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/codegraphfacts"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/effects"
)

type ModelRequest struct {
	Repository string

	Graph policy.CodeGraph

	Effects []policy.Effect

	Contexts []policy.ModelContext

	Library policy.Library

	Binding policy.Binding

	Members []ResolvedMember

	Tool string
}

func (r ModelRequest) binding() policy.Binding {
	if len(r.Binding.Roles) > 0 || len(r.Binding.Vocabulary.Classes()) > 0 || len(r.Binding.Imports) > 0 {
		return r.Binding
	}
	return r.Library.Manifest.Binding()
}

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
		Binding:    request.binding(),
		Members:    models,
	})
	if err != nil {
		return policy.ArchitectureModel{}, nil, err
	}
	policy.SortMembers(pins)
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
	binding := request.binding().Merge(policy.Binding{Roles: member.Member.Roles})
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
