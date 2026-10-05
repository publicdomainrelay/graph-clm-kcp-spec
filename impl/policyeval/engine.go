package policyeval

import (
	"context"
	"fmt"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	mtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

type Engine struct {
	client *constraintclient.Client

	templates map[string]policy.Template

	parsed map[string]*templates.ConstraintTemplate

	constraints map[string]policy.Constraint

	nameGlobs map[string]policy.Match

	libs []string
}

func NewEngine(ctx context.Context, library policy.Library, libs []string) (*Engine, error) {
	if len(libs) == 0 {
		libs = []string{Lib()}
	}
	scheme := runtime.NewScheme()
	if err := apis.AddToScheme(scheme); err != nil {
		return nil, err
	}
	driver, err := rego.New()
	if err != nil {
		return nil, err
	}
	client, err := constraintclient.NewClient(
		constraintclient.Targets(&target.K8sValidationTarget{}),
		constraintclient.Driver(driver),
		constraintclient.EnforcementPoints(util.GatorEnforcementPoint),
	)
	if err != nil {
		return nil, err
	}

	engine := &Engine{
		client:      client,
		templates:   map[string]policy.Template{},
		parsed:      map[string]*templates.ConstraintTemplate{},
		constraints: map[string]policy.Constraint{},
		nameGlobs:   map[string]policy.Match{},
		libs:        libs,
	}

	for _, template := range library.Templates {
		built := template
		for _, lib := range libs {
			if !hasLib(built.Libs) {
				built.Libs = append(built.Libs, lib)
			}
		}
		header, err := built.Header()
		if err != nil {
			return nil, fmt.Errorf("policyeval: template %s: %w", template.Name, err)
		}
		object, err := Unstructured(header)
		if err != nil {
			return nil, fmt.Errorf("policyeval: template %s: %w", template.Name, err)
		}
		parsed, err := reader.ToTemplate(scheme, object)
		if err != nil {
			return nil, fmt.Errorf("policyeval: template %s: %w", template.Name, err)
		}
		if _, err := client.AddTemplate(ctx, parsed); err != nil {
			return nil, fmt.Errorf("policyeval: template %s: %w", template.Name, err)
		}
		engine.templates[template.Kind] = built
		engine.parsed[template.Kind] = parsed
	}

	for _, constraint := range library.Constraints {
		document, err := constraint.Document()
		if err != nil {
			return nil, fmt.Errorf("policyeval: constraint %s: %w", constraint.Name, err)
		}
		if constraint.Match.HasNameGlob() {
			engine.nameGlobs[constraint.Name] = constraint.Match
			document, err = globConstraintDocument(constraint)
			if err != nil {
				return nil, fmt.Errorf("policyeval: constraint %s: %w", constraint.Name, err)
			}
		}
		object, err := Unstructured(document)
		if err != nil {
			return nil, fmt.Errorf("policyeval: constraint %s: %w", constraint.Name, err)
		}
		if _, err := client.AddConstraint(ctx, object); err != nil {
			return nil, fmt.Errorf("policyeval: constraint %s: %w", constraint.Name, err)
		}
		engine.constraints[constraint.Name] = constraint
	}

	return engine, nil
}

func globConstraintDocument(constraint policy.Constraint) ([]byte, error) {
	stripped := constraint
	stripped.Match.Name = ""
	return stripped.Document()
}

func Unstructured(data []byte) (*unstructured.Unstructured, error) {
	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return nil, fmt.Errorf("policyeval: empty object")
	}
	return &unstructured.Unstructured{Object: decoded}, nil
}

func (e *Engine) AddData(ctx context.Context, object *unstructured.Unstructured) error {
	if _, err := e.client.AddData(ctx, object); err != nil {
		return err
	}
	return nil
}

func (e *Engine) AddInventory(ctx context.Context, objects []*unstructured.Unstructured) error {
	for _, object := range objects {
		if err := e.AddData(ctx, object); err != nil {
			return fmt.Errorf("policyeval: inventory %s/%s: %w", object.GetKind(), object.GetName(), err)
		}
	}
	return nil
}

func (e *Engine) Review(ctx context.Context, object *unstructured.Unstructured) ([]policy.Violation, error) {
	results, err := e.ReviewRaw(ctx, object)
	if err != nil {
		return nil, err
	}
	return e.violations(object, results), nil
}

func (e *Engine) ReviewRaw(ctx context.Context, object *unstructured.Unstructured) ([]*types.Result, error) {
	response, err := e.client.Review(
		ctx,
		target.AugmentedUnstructured{Object: *object, Source: mtypes.SourceTypeOriginal},
		reviews.EnforcementPoint(util.GatorEnforcementPoint),
	)
	if err != nil {
		return nil, err
	}
	return response.Results(), nil
}

func (e *Engine) violations(object *unstructured.Unstructured, results []*types.Result) []policy.Violation {
	ref := ObjectRefOf(object)
	gvk := object.GroupVersionKind()
	out := []policy.Violation{}
	for _, result := range results {
		constraintName := ""
		if result.Constraint != nil {
			constraintName = result.Constraint.GetName()
		}
		if match, ok := e.nameGlobs[constraintName]; ok {
			if !match.Matches(policy.MatchTarget{Object: object, GVK: gvk}) {
				continue
			}
		}
		constraint := e.constraints[constraintName]
		template := e.templates[constraint.Kind]
		if template.Name == "" && result.Constraint != nil {
			template = e.templates[result.Constraint.GetKind()]
		}
		enforcement := policy.Enforcement(result.EnforcementAction)
		if enforcement == "" {
			enforcement = constraint.Enforcement
		}
		if enforcement == "" {
			enforcement = policy.EnforcementDeny
		}
		details := DetailsOf(result)
		location := LocationOf(details)
		violation := policy.Violation{
			Policy:       policy.TemplateSlug(template),
			Title:        template.Title,
			Level:        template.Level,
			Severity:     template.Severity,
			Requirements: template.Requirements,
			Constraint:   constraintName,
			Enforcement:  enforcement,
			Msg:          result.Msg,
			Details:      details,
			Object:       ref,
			Location:     location,
		}
		violation.ID = policy.ViolationID(violation.Policy, violation.Constraint, ref.Kind, ref.Namespace, ref.Name, violation.Msg, locationKey(location))
		out = append(out, violation)
	}
	return out
}

func ObjectRefOf(object *unstructured.Unstructured) policy.ObjectRef {
	return policy.ObjectRef{
		APIVersion: object.GetAPIVersion(),
		Kind:       object.GetKind(),
		Namespace:  object.GetNamespace(),
		Name:       object.GetName(),
	}
}

func DetailsOf(result *types.Result) map[string]any {
	if result.Metadata == nil {
		return map[string]any{}
	}
	details, ok := result.Metadata["details"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return details
}

func LocationOf(details map[string]any) *policy.Location {
	file, _ := details["file"].(string)
	line := intOf(details["line"])
	if nested, ok := details["location"].(map[string]any); ok {
		if file == "" {
			file, _ = nested["file"].(string)
		}
		if line == 0 {
			line = intOf(nested["line"])
		}
	}
	if file == "" {
		return nil
	}
	return &policy.Location{File: file, Line: line}
}

func locationKey(location *policy.Location) string {
	if location == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", location.File, location.Line)
}

// ConstraintCRD builds the constraint CRD Gatekeeper's controller would create
// for a template, with the frameworks' own helper, so a constraint kind specd
// serves is the kind a real Gatekeeper serves. The returned object is the v1
// CustomResourceDefinition, cluster scoped, served at v1beta1.
func (e *Engine) ConstraintCRD(ctx context.Context, template policy.Template) (*unstructured.Unstructured, error) {
	parsed, ok := e.parsed[template.Kind]
	if !ok {
		return nil, fmt.Errorf("policyeval: no template of kind %s", template.Kind)
	}
	internal, err := e.client.CreateCRD(ctx, parsed)
	if err != nil {
		return nil, fmt.Errorf("policyeval: constraint CRD for %s: %w", template.Kind, err)
	}
	return crdToUnstructured(internal)
}

func crdToUnstructured(internal *apiextensions.CustomResourceDefinition) (*unstructured.Unstructured, error) {
	scheme := runtime.NewScheme()
	if err := apiextensions.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	converted := &apiextensionsv1.CustomResourceDefinition{}
	if err := scheme.Convert(internal, converted, nil); err != nil {
		return nil, fmt.Errorf("policyeval: convert the constraint CRD: %w", err)
	}
	converted.TypeMeta = metav1.TypeMeta{
		APIVersion: apiextensionsv1.SchemeGroupVersion.String(),
		Kind:       "CustomResourceDefinition",
	}
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(converted)
	if err != nil {
		return nil, fmt.Errorf("policyeval: encode the constraint CRD: %w", err)
	}
	return &unstructured.Unstructured{Object: content}, nil
}

func intOf(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case uint64:
		return int(typed)
	}
	return 0
}
