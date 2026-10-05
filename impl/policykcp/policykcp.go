package policykcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

type Cluster interface {
	ListCluster(ctx context.Context, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error)

	GetCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error)

	CreateCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	ApplyCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	DeleteCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) error
}

const SlugAnnotation = policy.AnnotationSlug

func TemplateObject(template policy.Template) (*unstructured.Unstructured, error) {
	built := template
	if built.Slug != "" && built.Slug != built.Name {
		built.Annotations = withSlug(built.Annotations, built.Slug)
	}
	header, err := built.Header()
	if err != nil {
		return nil, fmt.Errorf("policykcp: template %s: %w", template.Name, err)
	}
	object, err := policyeval.Unstructured(header)
	if err != nil {
		return nil, err
	}
	object.SetKind(policy.ConstraintTemplateKind)
	object.SetAPIVersion(policy.ConstraintTemplateAPIVersion)
	return object, nil
}

func withSlug(annotations map[string]string, slug string) map[string]string {
	out := map[string]string{}
	for key, value := range annotations {
		out[key] = value
	}
	out[SlugAnnotation] = slug
	return out
}

func ParseTemplateObject(object *unstructured.Unstructured) (policy.Template, error) {
	rego := ""

	header := object.DeepCopy()
	targets, found, err := unstructured.NestedSlice(header.Object, "spec", "targets")
	if err != nil {
		return policy.Template{}, fmt.Errorf("policykcp: %s targets: %w", object.GetName(), err)
	}
	if found {
		for _, raw := range targets {
			mapping, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := mapping["rego"].(string); ok && value != "" {
				rego = value
			}
			delete(mapping, "rego")
			delete(mapping, "libs")
		}
		if err := unstructured.SetNestedSlice(header.Object, targets, "spec", "targets"); err != nil {
			return policy.Template{}, err
		}
	}
	encoded, err := yaml.Marshal(header.Object)
	if err != nil {
		return policy.Template{}, err
	}
	template, err := policy.ParseTemplate(encoded, []byte(rego))
	if err != nil {
		return policy.Template{}, err
	}
	if template.Name == "" {
		template.Name = object.GetName()
	}
	if template.Slug == "" {
		template.Slug = template.Name
	}
	return template, nil
}

func ConstraintObject(constraint policy.Constraint) (*unstructured.Unstructured, error) {
	document, err := constraint.Document()
	if err != nil {
		return nil, fmt.Errorf("policykcp: constraint %s: %w", constraint.Name, err)
	}
	return policyeval.Unstructured(document)
}

func ParseConstraintObject(object *unstructured.Unstructured, templateName string) (policy.Constraint, error) {
	encoded, err := yaml.Marshal(object.Object)
	if err != nil {
		return policy.Constraint{}, err
	}
	constraint, err := policy.ParseConstraint(encoded, templateName)
	if err != nil {
		return policy.Constraint{}, err
	}
	if constraint.Kind == "" {
		constraint.Kind = object.GetKind()
	}
	return constraint, nil
}

// Read loads the templates and constraints kcp holds. A constraint list that
// answers "no such resource" means the CRD has not been created yet, so the
// kind simply has no constraints.
func Read(ctx context.Context, cluster Cluster) (policy.Library, error) {
	library := policy.Library{}
	listed, err := cluster.ListCluster(ctx, policy.ConstraintTemplateGVR())
	if err != nil {
		return library, fmt.Errorf("policykcp: list constraint templates: %w", err)
	}
	for index := range listed.Items {
		template, err := ParseTemplateObject(&listed.Items[index])
		if err != nil {
			return library, err
		}
		library.Templates = append(library.Templates, template)
	}
	library.Sort()
	for _, template := range library.Templates {
		listed, err := cluster.ListCluster(ctx, policy.ConstraintGVR(template.Kind))
		if err != nil {
			continue
		}
		for index := range listed.Items {
			constraint, err := ParseConstraintObject(&listed.Items[index], template.Name)
			if err != nil {
				return library, err
			}
			constraint.Kind = template.Kind
			constraint.Path = policy.ConstraintPath(constraint.Name)
			library.Constraints = append(library.Constraints, constraint)
		}
	}
	library.Sort()
	return library, nil
}

type ApplyOptions struct {
	Prune bool
}

// Apply writes the library into kcp: each ConstraintTemplate, the constraint
// CRD its kind needs and every constraint. With Prune, templates and
// constraints kcp holds and the library does not are deleted.
func Apply(ctx context.Context, cluster Cluster, library policy.Library, options ApplyOptions) error {
	engine, err := policyeval.NewEngine(ctx, library, nil)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, template := range library.Templates {
		built := template
		if len(built.Libs) == 0 && library.Lib != "" {
			built.Libs = []string{library.Lib}
		}
		object, err := TemplateObject(built)
		if err != nil {
			return err
		}
		if _, err := cluster.ApplyCluster(ctx, object); err != nil {
			return fmt.Errorf("policykcp: apply template %s: %w", template.Name, err)
		}
		wanted[template.Name] = true
		crd, err := engine.ConstraintCRD(ctx, template)
		if err != nil {
			return err
		}
		if _, err := cluster.ApplyCluster(ctx, crd); err != nil {
			return fmt.Errorf("policykcp: apply constraint CRD for %s: %w", template.Kind, err)
		}
	}
	for _, constraint := range library.Constraints {
		object, err := ConstraintObject(constraint)
		if err != nil {
			return err
		}
		if _, err := cluster.ApplyCluster(ctx, object); err != nil {
			return fmt.Errorf("policykcp: apply constraint %s: %w", constraint.Name, err)
		}
	}
	if options.Prune {
		if err := prune(ctx, cluster, library, wanted); err != nil {
			return err
		}
	}
	return nil
}

func prune(ctx context.Context, cluster Cluster, library policy.Library, wantedTemplates map[string]bool) error {
	listed, err := cluster.ListCluster(ctx, policy.ConstraintTemplateGVR())
	if err != nil {
		return err
	}
	for index := range listed.Items {
		name := listed.Items[index].GetName()
		if wantedTemplates[name] {
			continue
		}
		if err := cluster.DeleteCluster(ctx, policy.ConstraintTemplateGVR(), name); err != nil {
			return err
		}
	}
	wantedConstraints := map[string]map[string]bool{}
	for _, constraint := range library.Constraints {
		if wantedConstraints[constraint.Kind] == nil {
			wantedConstraints[constraint.Kind] = map[string]bool{}
		}
		wantedConstraints[constraint.Kind][constraint.Name] = true
	}
	for _, template := range library.Templates {
		listed, err := cluster.ListCluster(ctx, policy.ConstraintGVR(template.Kind))
		if err != nil {
			continue
		}
		for index := range listed.Items {
			name := listed.Items[index].GetName()
			if wantedConstraints[template.Kind][name] {
				continue
			}
			if err := cluster.DeleteCluster(ctx, policy.ConstraintGVR(template.Kind), name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Files renders the branch files a kcp library owns: the template header, the
// rule source and the constraints. The manifest, the shared lib, the tests and
// the dist stay as the branch holds them.
func Files(library policy.Library) (map[string][]byte, error) {
	add := map[string][]byte{}
	for _, template := range library.Templates {
		slug := policy.TemplateSlug(template)
		stripped := template
		stripped.Rego = ""
		stripped.Libs = nil
		if slug != stripped.Name {
			stripped.Annotations = withSlug(stripped.Annotations, slug)
		}
		header, err := stripped.Header()
		if err != nil {
			return nil, fmt.Errorf("policykcp: template %s: %w", template.Name, err)
		}
		add[policy.TemplateHeaderPath(slug)] = header
		add[policy.TemplateSourcePath(slug)] = []byte(template.Rego)
	}
	for _, constraint := range library.Constraints {
		document, err := constraint.Document()
		if err != nil {
			return nil, fmt.Errorf("policykcp: constraint %s: %w", constraint.Name, err)
		}
		add[policy.ConstraintPath(constraint.Name)] = document
	}
	return add, nil
}

// Stale names the managed branch paths the library no longer covers. Tests,
// the dist, the catalogue, the reports and the manifest are never stale here.
func Stale(existing map[string][]byte, library policy.Library) []string {
	keep := map[string]bool{}
	files, err := Files(library)
	if err != nil {
		return nil
	}
	for name := range files {
		keep[name] = true
	}
	out := []string{}
	for name := range existing {
		if !managed(name) || keep[name] {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func managed(name string) bool {
	return strings.HasPrefix(name, policy.TemplatesDir+"/") ||
		strings.HasPrefix(name, policy.ConstraintsDir+"/")
}

// Distinct reports whether a kcp library differs from a branch library, by
// template and constraint content rather than by commit.
func Distinct(kcp, branch policy.Library) bool {
	return fingerprint(kcp) != fingerprint(branch)
}

func fingerprint(library policy.Library) string {
	builder := strings.Builder{}
	for _, template := range library.Templates {
		fmt.Fprintf(&builder, "T|%s|%s|%s|%v|%v|%s\n",
			template.Name, template.Kind, template.Slug, template.Annotations, template.Parameters, template.Rego)
	}
	for _, constraint := range library.Constraints {
		document, _ := constraint.Document()
		fmt.Fprintf(&builder, "C|%s|%v|%s\n", constraint.Name, constraint.Parameters, document)
	}
	return builder.String()
}
