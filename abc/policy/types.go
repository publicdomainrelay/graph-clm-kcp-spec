package policy

import (
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

const (
	Group   = "specs.publicdomainrelay.dev"
	Version = "v1alpha1"

	APIVersion = Group + "/" + Version

	CodeGraphKind = "CodeGraph"
	CodeDiffKind  = "CodeDiff"

	RepositoryKind    = "Repository"
	SystemContextKind = "SystemContext"
	SpecChangeKind    = "SpecChange"
	ArchitectureKind  = "Architecture"
)

const (
	AnnotationTitle        = Group + "/title"
	AnnotationLevel        = Group + "/level"
	AnnotationSeverity     = Group + "/severity"
	AnnotationRequirements = Group + "/requirements"
	AnnotationGeneratedBy  = Group + "/generated-by"

	AnnotationSlug = Group + "/slug"
)

type Enforcement string

const (
	EnforcementDeny   Enforcement = "deny"
	EnforcementWarn   Enforcement = "warn"
	EnforcementDryRun Enforcement = "dryrun"
)

func EnforcementRank(action Enforcement) int {
	switch action {
	case EnforcementDeny:
		return 3
	case EnforcementWarn:
		return 2
	case EnforcementDryRun:
		return 1
	}
	return 0
}

func (e Enforcement) Known() bool {
	return EnforcementRank(e) > 0
}

type Level string

const (
	LevelMust   Level = "MUST"
	LevelShould Level = "SHOULD"
	LevelMay    Level = "MAY"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

func SeverityForLevel(level Level) Severity {
	switch level {
	case LevelMust:
		return SeverityError
	case LevelShould:
		return SeverityWarning
	case LevelMay:
		return SeverityInfo
	}
	return SeverityInfo
}

func KnownLevel(level Level) bool {
	switch level {
	case LevelMust, LevelShould, LevelMay:
		return true
	}
	return false
}

type Template struct {
	Name string

	Slug string

	Kind string

	Title string

	Level Level

	Severity Severity

	Requirements []string

	GeneratedBy string

	Annotations map[string]string

	Parameters map[string]any

	Rego string

	Libs []string

	Path string
}

type TemplateHeader struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   TemplateMeta   `json:"metadata"`
	Spec       TemplateSpecIn `json:"spec"`
}

type TemplateMeta struct {
	Name        string            `json:"name"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type TemplateSpecIn struct {
	CRD     TemplateCRD      `json:"crd"`
	Targets []TemplateTarget `json:"targets"`
}

type TemplateCRD struct {
	Spec TemplateCRDSpec `json:"spec"`
}

type TemplateCRDSpec struct {
	Names      TemplateNames `json:"names"`
	Validation *ValidationIn `json:"validation,omitempty"`
}

type TemplateNames struct {
	Kind       string   `json:"kind"`
	ShortNames []string `json:"shortNames,omitempty"`
}

type ValidationIn struct {
	OpenAPIV3Schema map[string]any `json:"openAPIV3Schema,omitempty"`
}

type TemplateTarget struct {
	Target string   `json:"target"`
	Rego   string   `json:"rego,omitempty"`
	Libs   []string `json:"libs,omitempty"`
}

func ParseTemplate(header, rego []byte) (Template, error) {
	var parsed TemplateHeader
	if err := yaml.Unmarshal(header, &parsed); err != nil {
		return Template{}, err
	}
	template := Template{
		Name:        parsed.Metadata.Name,
		Kind:        parsed.Spec.CRD.Spec.Names.Kind,
		Annotations: parsed.Metadata.Annotations,
		Rego:        string(rego),
	}
	if parsed.Spec.CRD.Spec.Validation != nil {
		template.Parameters = parsed.Spec.CRD.Spec.Validation.OpenAPIV3Schema
	}
	for _, target := range parsed.Spec.Targets {
		if target.Rego != "" {
			template.Rego = target.Rego
		}
		template.Libs = append(template.Libs, target.Libs...)
	}
	template.Slug = template.Annotations[AnnotationSlug]
	template.Title = template.Annotations[AnnotationTitle]
	template.Level = Level(template.Annotations[AnnotationLevel])
	template.Severity = Severity(template.Annotations[AnnotationSeverity])
	template.Requirements = splitList(template.Annotations[AnnotationRequirements])
	template.GeneratedBy = template.Annotations[AnnotationGeneratedBy]
	if template.Severity == "" {
		template.Severity = SeverityForLevel(template.Level)
	}
	return template, nil
}

func splitList(value string) []string {
	out := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func TemplateSlug(t Template) string {
	if t.Slug != "" {
		return t.Slug
	}
	return t.Name
}

func (t Template) Header() ([]byte, error) {
	header := TemplateHeader{
		APIVersion: "templates.gatekeeper.sh/v1",
		Kind:       "ConstraintTemplate",
		Metadata: TemplateMeta{
			Name:        t.Name,
			Annotations: t.Annotations,
		},
		Spec: TemplateSpecIn{
			CRD: TemplateCRD{Spec: TemplateCRDSpec{Names: TemplateNames{Kind: t.Kind}}},
			Targets: []TemplateTarget{{
				Target: "admission.k8s.gatekeeper.sh",
				Rego:   t.Rego,
				Libs:   t.Libs,
			}},
		},
	}
	if t.Parameters != nil {
		header.Spec.CRD.Spec.Validation = &ValidationIn{OpenAPIV3Schema: t.Parameters}
	}
	return yaml.Marshal(header)
}

type MatchKind struct {
	APIGroups []string `json:"apiGroups,omitempty"`
	Kinds     []string `json:"kinds,omitempty"`
}

type Match struct {
	Kinds []MatchKind `json:"kinds,omitempty"`

	Name string `json:"name,omitempty"`

	Namespaces []string `json:"namespaces,omitempty"`

	ExcludedNamespaces []string `json:"excludedNamespaces,omitempty"`

	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	Scope string `json:"scope,omitempty"`
}

type Constraint struct {
	Name string

	Kind string

	Template string

	Match Match

	Parameters map[string]any

	Enforcement Enforcement

	Path string
}

type ConstraintDoc struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   ConstraintMeta   `json:"metadata"`
	Spec       ConstraintSpecIn `json:"spec"`
}

type ConstraintMeta struct {
	Name string `json:"name"`
}

type ConstraintSpecIn struct {
	Match             Match          `json:"match,omitempty"`
	Parameters        map[string]any `json:"parameters,omitempty"`
	EnforcementAction string         `json:"enforcementAction,omitempty"`
}

func ParseConstraint(doc []byte, template string) (Constraint, error) {
	var parsed ConstraintDoc
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		return Constraint{}, err
	}
	constraint := Constraint{
		Name:        parsed.Metadata.Name,
		Kind:        parsed.Kind,
		Template:    template,
		Match:       parsed.Spec.Match,
		Parameters:  parsed.Spec.Parameters,
		Enforcement: Enforcement(parsed.Spec.EnforcementAction),
	}
	if constraint.Enforcement == "" {
		constraint.Enforcement = EnforcementDeny
	}
	return constraint, nil
}

func (c Constraint) Document() ([]byte, error) {
	doc := ConstraintDoc{
		APIVersion: ConstraintAPIVersion,
		Kind:       c.Kind,
		Metadata:   ConstraintMeta{Name: c.Name},
		Spec: ConstraintSpecIn{
			Match:             c.Match,
			Parameters:        c.Parameters,
			EnforcementAction: string(c.Enforcement),
		},
	}
	return yaml.Marshal(doc)
}

const ConstraintAPIVersion = "constraints.gatekeeper.sh/v1beta1"

type ObjectRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
}

func (r ObjectRef) String() string {
	builder := strings.Builder{}
	if r.Kind != "" {
		builder.WriteString(r.Kind)
		builder.WriteString(" ")
	}
	if r.Namespace != "" {
		builder.WriteString(r.Namespace)
		builder.WriteString("/")
	}
	builder.WriteString(r.Name)
	return builder.String()
}

type Location struct {
	File string `json:"file"`

	Line int `json:"line,omitempty"`

	Node string `json:"node,omitempty"`
}

type Violation struct {
	ID string `json:"id"`

	Policy string `json:"policy"`

	Title string `json:"title,omitempty"`

	Level Level `json:"level,omitempty"`

	Severity Severity `json:"severity,omitempty"`

	Requirements []string `json:"requirements,omitempty"`

	Constraint string `json:"constraint"`

	Enforcement Enforcement `json:"enforcementAction"`

	Msg string `json:"msg"`

	Details map[string]any `json:"details,omitempty"`

	Object ObjectRef `json:"object"`

	Location *Location `json:"location,omitempty"`
}

func (v Violation) GVK() schema.GroupVersionKind {
	return schema.FromAPIVersionAndKind(v.Object.APIVersion, v.Object.Kind)
}

type Report struct {
	Repository string `json:"repository,omitempty"`

	Commit string `json:"commit,omitempty"`

	Templates int `json:"templates"`

	Constraints int `json:"constraints"`

	Reviewed []ObjectRef `json:"reviewed,omitempty"`

	Violations []Violation `json:"violations"`

	Totals map[Enforcement]int `json:"totals"`

	Severity map[Severity]int `json:"severity"`
}

func (r *Report) Sort() {
	sort.SliceStable(r.Violations, func(left, right int) bool {
		a, b := r.Violations[left], r.Violations[right]
		if a.Constraint != b.Constraint {
			return a.Constraint < b.Constraint
		}
		if a.Object.Kind != b.Object.Kind {
			return a.Object.Kind < b.Object.Kind
		}
		if a.Object.Namespace != b.Object.Namespace {
			return a.Object.Namespace < b.Object.Namespace
		}
		if a.Object.Name != b.Object.Name {
			return a.Object.Name < b.Object.Name
		}
		if a.Msg != b.Msg {
			return a.Msg < b.Msg
		}
		return a.ID < b.ID
	})
	sort.SliceStable(r.Reviewed, func(left, right int) bool {
		return r.Reviewed[left].String() < r.Reviewed[right].String()
	})
}

func (r *Report) Tally() {
	r.Totals = map[Enforcement]int{}
	r.Severity = map[Severity]int{}
	for _, violation := range r.Violations {
		r.Totals[violation.Enforcement]++
		r.Severity[violation.Severity]++
	}
}

func (r Report) Empty() bool {
	return len(r.Violations) == 0
}

func (r Report) ForObject(ref ObjectRef) []Violation {
	out := []Violation{}
	for _, violation := range r.Violations {
		if violation.Object == ref {
			out = append(out, violation)
		}
	}
	return out
}

func (r Report) ForFile(path string) []Violation {
	out := []Violation{}
	for _, violation := range r.Violations {
		if violation.Location != nil && violation.Location.File == path {
			out = append(out, violation)
		}
	}
	return out
}

func ViolationID(parts ...string) string {
	joined := strings.Join(parts, "|")
	sum := fnv1a(joined)
	return sum
}

func fnv1a(value string) string {
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	hash := uint64(offset)
	for i := 0; i < len(value); i++ {
		hash ^= uint64(value[i])
		hash *= prime
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hexDigits[hash&0xf]
		hash >>= 4
	}
	return string(out)
}
