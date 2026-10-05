package policykcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/kcp-libs/common/kcp"
)

type Cluster interface {
	ListCluster(ctx context.Context, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error)

	GetCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error)

	CreateCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	ApplyCluster(ctx context.Context, object *unstructured.Unstructured) (*unstructured.Unstructured, error)

	DeleteCluster(ctx context.Context, gvr schema.GroupVersionResource, name string) error

	PatchStatusCluster(ctx context.Context, gvr schema.GroupVersionResource, name string, status map[string]any) (*unstructured.Unstructured, error)
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
	object, err := policy.Unstructured(header)
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

func normalizeMetadata(object *unstructured.Unstructured) {
	annotations := object.GetAnnotations()
	if len(annotations) == 0 {
		return
	}
	kept := map[string]string{}
	for key, value := range annotations {
		if key == kcp.ClusterAnnotation || key == kcp.PathAnnotation {
			continue
		}
		kept[key] = value
	}
	if len(kept) == 0 {
		object.SetAnnotations(nil)
		return
	}
	object.SetAnnotations(kept)
}

func ParseTemplateObject(object *unstructured.Unstructured) (policy.Template, error) {
	rego := ""
	libs := []string{}

	header := object.DeepCopy()
	normalizeMetadata(header)
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
			if raw, ok := mapping["libs"].([]any); ok {
				for _, entry := range raw {
					if lib, ok := entry.(string); ok {
						libs = append(libs, lib)
					}
				}
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
	delete(template.Annotations, policy.AnnotationSlug)
	if len(libs) > 0 {
		template.Libs = libs
	}
	return template, nil
}

func ConstraintObject(constraint policy.Constraint) (*unstructured.Unstructured, error) {
	document, err := constraint.Document()
	if err != nil {
		return nil, fmt.Errorf("policykcp: constraint %s: %w", constraint.Name, err)
	}
	return policy.Unstructured(document)
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

	CRDs policy.ConstraintCRDBuilder
}

func Apply(ctx context.Context, cluster Cluster, library policy.Library, options ApplyOptions) error {
	if options.CRDs == nil {
		return fmt.Errorf("policykcp: Apply needs a constraint CRD builder")
	}
	wanted := map[string]bool{}
	failures := []error{}
	for _, template := range library.Templates {
		wanted[template.Name] = true
		built := template
		if len(built.Libs) == 0 && library.Lib != "" {
			built.Libs = []string{library.Lib}
		}
		object, err := TemplateObject(built)
		if err != nil {
			failures = append(failures, fmt.Errorf("policykcp: template %s: %w", template.Name, err))
			continue
		}
		if _, err := cluster.ApplyCluster(ctx, object); err != nil {
			failures = append(failures, fmt.Errorf("policykcp: apply template %s: %w", template.Name, err))
			failures = appendStatus(failures, recordTemplateStatus(ctx, cluster, template.Name, false, err))
			continue
		}
		crd, err := options.CRDs.ConstraintCRD(ctx, built)
		if err == nil {
			_, err = cluster.ApplyCluster(ctx, crd)
		}
		if err == nil {
			err = waitForConstraintKind(ctx, cluster, template.Kind)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("policykcp: constraint CRD of %s (%s): %w", template.Name, template.Kind, err))
			failures = appendStatus(failures, recordTemplateStatus(ctx, cluster, template.Name, false, err))
			continue
		}
		failures = appendStatus(failures, recordTemplateStatus(ctx, cluster, template.Name, true, nil))
	}
	for _, constraint := range library.Constraints {
		object, err := ConstraintObject(constraint)
		if err == nil {
			_, err = cluster.ApplyCluster(ctx, object)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("policykcp: apply constraint %s: %w", constraint.Name, err))
		}
	}
	if options.Prune {
		if err := prune(ctx, cluster, library, wanted); err != nil {
			failures = append(failures, fmt.Errorf("policykcp: prune: %w", err))
		}
	}
	return errors.Join(failures...)
}

func recordTemplateStatus(ctx context.Context, cluster Cluster, name string, created bool, failure error) error {
	if _, err := cluster.PatchStatusCluster(ctx, policy.ConstraintTemplateGVR(), name,
		constraintTemplateStatus(created, failure)); err != nil {
		return fmt.Errorf("policykcp: record the constraint CRD status of %s: %w", name, err)
	}
	return nil
}

func appendStatus(failures []error, err error) []error {
	if err == nil {
		return failures
	}
	return append(failures, err)
}

const ConstraintCRDTimeout = 15 * time.Second

func waitForConstraintKind(ctx context.Context, cluster Cluster, kind string) error {
	deadline := time.Now().Add(ConstraintCRDTimeout)
	for {
		if _, err := cluster.ListCluster(ctx, policy.ConstraintGVR(kind)); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			_, err := cluster.ListCluster(ctx, policy.ConstraintGVR(kind))
			return fmt.Errorf("policykcp: the constraint kind %s is not served: %w", kind, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func constraintTemplateStatus(created bool, failure error) map[string]any {
	byPod := map[string]any{"id": "specd"}
	errors := []any{}
	if failure != nil {
		errors = append(errors, map[string]any{"code": "CreateCRDError", "message": failure.Error()})
	}
	byPod["errors"] = errors
	return map[string]any{"created": created, "byPod": []any{byPod}}
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

func Files(library policy.Library) (map[string][]byte, error) {
	add := map[string][]byte{}
	for _, template := range library.Templates {
		slug := policy.TemplateSlug(template)
		if library.ImportedFrom(slug) != "" {
			continue
		}
		stripped := template
		stripped.Rego = ""
		stripped.Libs = nil
		header, err := stripped.Header()
		if err != nil {
			return nil, fmt.Errorf("policykcp: template %s: %w", template.Name, err)
		}
		add[policy.TemplateHeaderPath(slug)] = header
		add[policy.TemplateSourcePath(slug)] = []byte(template.Rego)
	}
	for _, constraint := range library.Constraints {
		if library.ImportedFrom(constraint.Name) != "" {
			continue
		}
		document, err := constraint.Document()
		if err != nil {
			return nil, fmt.Errorf("policykcp: constraint %s: %w", constraint.Name, err)
		}
		add[policy.ConstraintPath(constraint.Name)] = document
	}
	return add, nil
}

func Stale(existing map[string][]byte, library policy.Library) []string {
	keep := map[string]bool{}
	files, err := Files(library)
	if err != nil {
		return nil
	}
	for name := range files {
		keep[name] = true
	}
	slugs := map[string]bool{}
	for _, template := range library.Templates {
		slugs[policy.TemplateSlug(template)] = true
	}
	out := []string{}
	for name := range existing {
		if slug, ok := distSlug(name); ok {
			if !slugs[slug] {
				out = append(out, name)
			}
			continue
		}
		if !managed(name) || keep[name] {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func distSlug(name string) (string, bool) {
	prefix := policy.DistDir + "/"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".yaml") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".yaml"), true
}

func managed(name string) bool {
	return strings.HasPrefix(name, policy.TemplatesDir+"/") ||
		strings.HasPrefix(name, policy.ConstraintsDir+"/")
}

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
