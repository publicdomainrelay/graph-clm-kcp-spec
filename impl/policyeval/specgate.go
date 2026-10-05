package policyeval

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// SpecInput is the spec-time state of one repository: every context as the
// delta leaves it. Applied carries the post-delta spec of a context whose kcp
// object has not moved yet; a context without an entry is taken as it stands.
type SpecInput struct {
	Repository string

	Contexts []spec.SystemContext

	Applied map[string]spec.SystemContextSpec

	Binding policy.Binding

	Library policy.Library
}

// SpecResult is what a policy library said about one repository's declared
// state: the model the rules read, the report and the gate decision.
type SpecResult struct {
	Model policy.ArchitectureModel

	Report policy.Report

	Decision policy.Decision
}

func (r SpecResult) Messages() []string {
	return r.Decision.Messages()
}

// CheckSpecs builds the ArchitectureModel from declared facts only (the specs,
// the binding, no code) and evaluates the policy library against it. It is the
// spec-time half of the gate: nothing is realized here, and the caller decides
// what a blocked result means.
func CheckSpecs(ctx context.Context, input SpecInput) (SpecResult, error) {
	if len(input.Library.Templates) == 0 {
		return SpecResult{}, fmt.Errorf("policyeval: the library of %s has no templates", input.Repository)
	}
	contexts := make([]policy.ModelContext, 0, len(input.Contexts))
	contextObjects := make([]*unstructured.Unstructured, 0, len(input.Contexts))
	for _, context := range input.Contexts {
		specification := context.Spec
		if applied, ok := input.Applied[context.Name]; ok {
			specification = applied
		}
		contexts = append(contexts, policy.ModelContext{
			Name:         context.Name,
			Labels:       context.Labels,
			Interactions: declaredInteractions(specification.Interactions),
		})
		object, err := policy.Unstructured(marshalSpecObject(spec.SystemContext{
			TypeMeta:   metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.SystemContextKind},
			ObjectMeta: context.ObjectMeta,
			Spec:       specification,
		}))
		if err != nil {
			return SpecResult{}, err
		}
		contextObjects = append(contextObjects, object)
	}

	model, err := policy.BuildModel(policy.ModelInput{
		Repository: input.Repository,
		Contexts:   contexts,
		Binding:    input.Binding,
	})
	if err != nil {
		return SpecResult{}, err
	}
	modelObject, err := policy.Unstructured(marshalSpecObject(model))
	if err != nil {
		return SpecResult{}, err
	}
	repositoryObject, err := policy.Unstructured(marshalSpecObject(spec.Repository{
		TypeMeta:   metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.RepositoryKind},
		ObjectMeta: metav1.ObjectMeta{Name: input.Repository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Branch: "main"},
	}))
	if err != nil {
		return SpecResult{}, err
	}

	inventory := append([]*unstructured.Unstructured{modelObject, repositoryObject}, contextObjects...)
	report, err := Evaluate(ctx, Evaluation{
		Library:    input.Library,
		Repository: input.Repository,
		Reviewed:   []*unstructured.Unstructured{modelObject},
		Inventory:  inventory,
	})
	if err != nil {
		return SpecResult{}, err
	}
	return SpecResult{
		Model:    model,
		Report:   report,
		Decision: policy.Decide(report, policy.RepositoryPolicy{}, nil),
	}, nil
}

func declaredInteractions(interactions []spec.Interaction) []policy.DeclaredInteraction {
	out := make([]policy.DeclaredInteraction, 0, len(interactions))
	for _, interaction := range interactions {
		out = append(out, policy.DeclaredInteraction{
			Peer:      interaction.Peer,
			Initiator: interaction.Initiator,
			Channel:   interaction.Channel,
			Carries:   interaction.Carries,
			Purpose:   interaction.Purpose,
			Level:     string(spec.CanonicalInteraction(interaction).Level),
			Forbidden: interaction.Forbidden,
		})
	}
	return out
}

func marshalSpecObject(value any) []byte {
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
