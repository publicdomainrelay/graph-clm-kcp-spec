package policyeval

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

type SpecInput struct {
	Repository string

	Contexts []spec.SystemContext

	Applied map[string]spec.SystemContextSpec

	Base map[string]spec.SystemContextSpec

	Binding policy.Binding

	Library policy.Library

	Policy policy.RepositoryPolicy

	Overrides []policy.Override

	MemberCacheDir string

	Tool string
}

type SpecResult struct {
	Model policy.ArchitectureModel

	Report policy.Report

	Decision policy.Decision
}

func (r SpecResult) Messages() []string {
	return r.Decision.Messages()
}

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
		document, err := marshalSpecObject(spec.SystemContext{
			TypeMeta:   metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.SystemContextKind},
			ObjectMeta: context.ObjectMeta,
			Spec:       specification,
		})
		if err != nil {
			return SpecResult{}, err
		}
		object, err := policy.Unstructured(document)
		if err != nil {
			return SpecResult{}, err
		}
		contextObjects = append(contextObjects, object)
	}

	members, err := ResolveMembers(ctx, input.Library.Manifest.Members, input.Library, MemberOptions{
		CacheDir: input.MemberCacheDir,
		Lock:     specGateLock(input.Library),
	})
	if err != nil {
		return SpecResult{}, fmt.Errorf("policyeval: resolve the policy members: %w", err)
	}
	defer func() {
		for _, member := range members {
			member.Cleanup()
		}
	}()
	model, memberPins, err := BuildEvaluationModel(ctx, ModelRequest{
		Repository: input.Repository,
		Contexts:   contexts,
		Library:    input.Library,
		Binding:    input.Binding,
		Members:    members,
		Tool:       input.Tool,
	})
	if err != nil {
		return SpecResult{}, err
	}
	modelDocument, err := marshalSpecObject(model)
	if err != nil {
		return SpecResult{}, err
	}
	modelObject, err := policy.Unstructured(modelDocument)
	if err != nil {
		return SpecResult{}, err
	}
	branch := input.Policy.Branch
	if branch == "" {
		branch = "main"
	}
	repositoryDocument, err := marshalSpecObject(spec.Repository{
		TypeMeta:   metav1.TypeMeta{APIVersion: specapi.APIVersion, Kind: specapi.RepositoryKind},
		ObjectMeta: metav1.ObjectMeta{Name: input.Repository, Namespace: specapi.DefaultNamespace},
		Spec:       spec.RepositorySpec{Branch: branch},
	})
	if err != nil {
		return SpecResult{}, err
	}
	repositoryObject, err := policy.Unstructured(repositoryDocument)
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
	report.Members = memberPins
	overrides, _ := Waivers(input.Library, input.Overrides, time.Now())
	return SpecResult{
		Model:    model,
		Report:   report,
		Decision: policy.Decide(report, input.Policy, overrides),
	}, nil
}

func CheckSpecsBaseline(ctx context.Context, input SpecInput) (SpecResult, error) {
	head, err := CheckSpecs(ctx, input)
	if err != nil {
		return SpecResult{}, err
	}
	if !head.Decision.Blocked || !input.Policy.BaselineEnabled() {
		return head, nil
	}
	base := input
	base.Applied = input.Base
	baseResult, err := CheckSpecs(ctx, base)
	if err != nil {
		return SpecResult{}, err
	}
	overrides, _ := Waivers(input.Library, input.Overrides, time.Now())
	head.Decision = policy.DecideBaseline(head.Report, baseResult.Report, input.Policy, overrides)
	return head, nil
}

func specGateLock(library policy.Library) *policy.PackLock {
	data, ok := library.Files[policy.LockPath]
	if !ok {
		return nil
	}
	lock, err := ParseLock(data)
	if err != nil {
		return nil
	}
	return &lock
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

func marshalSpecObject(value any) ([]byte, error) {
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("policyeval: encode the %T for the spec gate: %w", value, err)
	}
	return encoded, nil
}
