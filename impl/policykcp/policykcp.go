package policykcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
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

// normalizeMetadata drops the annotations kcp adds to every object it serves,
// so a template read back from kcp compares equal to the branch it came from
// and the sync does not write cluster metadata into the branch.
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
	// The slug lives in the branch path, not in the header the branch holds, so
	// it is dropped here too and the kcp and branch forms stay comparable.
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

type ReadOptions struct {
	// Repository selects one repository's objects by their label. Empty reads
	// every repository's objects, the way `policy ls` shows the whole cluster.
	Repository string
}

// Read loads the templates and constraints kcp holds. With a repository it
// returns that repository's objects only, so a shared kcp never lends another
// repository's policies to a branch. A constraint list that answers "no such
// resource" means the CRD has not been created yet, so the kind simply has no
// constraints; any other error aborts the read rather than returning a short
// library that a sync would mistake for a deletion.
func Read(ctx context.Context, cluster Cluster, options ReadOptions) (policy.Library, error) {
	library := policy.Library{}
	listed, err := cluster.ListCluster(ctx, policy.ConstraintTemplateGVR())
	if err != nil {
		return library, fmt.Errorf("policykcp: list constraint templates: %w", err)
	}
	for index := range listed.Items {
		if !ownedBy(&listed.Items[index], options.Repository) {
			continue
		}
		template, err := ParseTemplateObject(&listed.Items[index])
		if err != nil {
			return library, err
		}
		library.Templates = append(library.Templates, template)
	}
	library.Sort()
	for _, template := range library.Templates {
		listed, err := listConstraints(ctx, cluster, template.Kind)
		if err != nil {
			return policy.Library{}, fmt.Errorf("policykcp: list the %s constraints: %w", template.Kind, err)
		}
		for index := range listed.Items {
			if !ownedBy(&listed.Items[index], options.Repository) {
				continue
			}
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

// listConstraints retries a transient list error once, then reports it. A
// constraint kind whose CRD is not established yet is not an error: the kind
// has no constraints.
func listConstraints(ctx context.Context, cluster Cluster, kind string) (*unstructured.UnstructuredList, error) {
	gvr := policy.ConstraintGVR(kind)
	listed, err := cluster.ListCluster(ctx, gvr)
	if err == nil || isNotFound(err) {
		if err != nil {
			return &unstructured.UnstructuredList{}, nil
		}
		return listed, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}
	listed, err = cluster.ListCluster(ctx, gvr)
	if err != nil && isNotFound(err) {
		return &unstructured.UnstructuredList{}, nil
	}
	return listed, err
}

// isNotFound reports whether a list failed because the resource is not served,
// which a constraint CRD that has not been established yet answers.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsNotFound(err) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "not found") ||
		strings.Contains(message, "could not find the requested resource") ||
		strings.Contains(message, "no matches for kind")
}

func ownedBy(object *unstructured.Unstructured, repository string) bool {
	if repository == "" {
		return true
	}
	return object.GetLabels()[policy.RepositoryLabel] == repository
}

type ApplyOptions struct {
	Prune bool

	// Repository labels every object written with the repository that owns it,
	// so a read and a prune can tell one repository's policies from another's.
	Repository string
}

// Apply writes the library into kcp: each ConstraintTemplate, the constraint
// CRD its kind needs and every constraint. With Prune, templates and
// constraints kcp holds and the library does not are deleted.
//
// One broken template or constraint never stops the rest: every member is
// attempted, the failing template records the error in its status, and the
// returned error names each failure. A caller that only reports the error
// therefore still leaves kcp holding everything that could be applied.
func Apply(ctx context.Context, cluster Cluster, library policy.Library, options ApplyOptions) error {
	engine, err := policyeval.NewEngine(ctx, library, nil)
	if err != nil {
		return err
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
		labelRepository(object, options.Repository)
		if _, err := cluster.ApplyCluster(ctx, object); err != nil {
			failures = append(failures, fmt.Errorf("policykcp: apply template %s: %w", template.Name, err))
			failures = appendStatus(failures, recordTemplateStatus(ctx, cluster, template.Name, false, err))
			continue
		}
		crd, err := engine.ConstraintCRD(ctx, built)
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
			labelRepository(object, options.Repository)
			_, err = cluster.ApplyCluster(ctx, object)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("policykcp: apply constraint %s: %w", constraint.Name, err))
		}
	}
	if options.Prune {
		if err := prune(ctx, cluster, library, options.Repository, wanted); err != nil {
			failures = append(failures, fmt.Errorf("policykcp: prune: %w", err))
		}
	}
	return errors.Join(failures...)
}

// labelRepository stamps the repository that owns an object, so a read and a
// prune of one repository's policies never touch another's.
func labelRepository(object *unstructured.Unstructured, repository string) {
	if repository == "" {
		return
	}
	labels := object.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[policy.RepositoryLabel] = repository
	object.SetLabels(labels)
}

// recordTemplateStatus writes the simplified byPod status Gatekeeper reports,
// so the reason a template did not establish is readable from kcp.
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

// ConstraintCRDTimeout bounds how long Apply waits for kcp to serve a
// constraint kind after its CRD was applied.
const ConstraintCRDTimeout = 15 * time.Second

// waitForConstraintKind waits until the constraint resource of a kind answers:
// applying a CRD and creating an object of it in one breath races the API
// server's establishment of the new kind.
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

// constraintTemplateStatus is the simplified byPod status Gatekeeper reports:
// whether specd created the constraint CRD, and why not when it could not.
func constraintTemplateStatus(created bool, failure error) map[string]any {
	byPod := map[string]any{"id": "specd"}
	errors := []any{}
	if failure != nil {
		errors = append(errors, map[string]any{"code": "CreateCRDError", "message": failure.Error()})
	}
	byPod["errors"] = errors
	return map[string]any{"created": created, "byPod": []any{byPod}}
}

// prune deletes the templates and constraints of one repository that the
// library no longer covers. An object of another repository, or an unlabelled
// one, is never touched: kcp serves every repository's policies.
func prune(ctx context.Context, cluster Cluster, library policy.Library, repository string, wantedTemplates map[string]bool) error {
	listed, err := cluster.ListCluster(ctx, policy.ConstraintTemplateGVR())
	if err != nil {
		return err
	}
	for index := range listed.Items {
		if !ownedBy(&listed.Items[index], repository) {
			continue
		}
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
		listed, err := listConstraints(ctx, cluster, template.Kind)
		if err != nil {
			return err
		}
		for index := range listed.Items {
			if !ownedBy(&listed.Items[index], repository) {
				continue
			}
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
		if library.ImportedFrom(slug) != "" {
			continue
		}
		stripped := template
		stripped.Rego = ""
		stripped.Libs = nil
		// The branch carries the slug in the directory name, the way
		// specctl policy build writes it, not as an annotation.
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

// Distinct reports whether a kcp library differs from a branch library, by
// template and constraint content rather than by commit.
func Distinct(kcp, branch policy.Library) bool {
	return Fingerprint(kcp) != Fingerprint(branch)
}

// Fingerprint is the content of a library as one comparable string: the same
// library read from a branch and from kcp fingerprints the same, so the sync
// can tell which side moved from a recorded base.
func Fingerprint(library policy.Library) string {
	return fingerprint(library)
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
