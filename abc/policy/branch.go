package policy

import (
	"path"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/oabranch"
)

const (
	BranchPrefix = "open-policy/"

	PoliciesPath = "policies.yaml"

	LibPath = "lib/specd.rego"

	LibTestPath = "lib/specd_test.rego"

	TemplatesDir = "templates"

	ConstraintsDir = "constraints"

	ClassifiersDir = "classifiers"

	TestsDir = "tests"

	DistDir = "dist"

	ReportsDir = "reports"

	ChangesDir = "changes"

	CataloguePath = "CATALOGUE.md"

	GitAttributesPath = ".gitattributes"

	SourceName = "src.rego"

	TestName = "src_test.rego"

	HeaderName = "template.yaml"

	SuiteName = "suite.yaml"
)

const (
	CommitLabel = Group + "/commit"

	BranchLabel = Group + "/branch"

	RepositoryLabel = Group + "/repository"
)

func Branch(repository string) string {
	return BranchPrefix + repository
}

func Ref(repository string) string {
	return "refs/heads/" + Branch(repository)
}

func Resolve(repository, codeBranch, defaultBranch string) string {
	if codeBranch == "" || codeBranch == defaultBranch {
		return Branch(repository)
	}
	return Branch(repository) + oabranch.BranchSeparator + oabranch.Slug(codeBranch)
}

func RefFor(repository, codeBranch, defaultBranch string) string {
	return "refs/heads/" + Resolve(repository, codeBranch, defaultBranch)
}

func RepositoryOf(branch string) (string, bool) {
	if !strings.HasPrefix(branch, BranchPrefix) {
		return "", false
	}
	return strings.TrimPrefix(branch, BranchPrefix), true
}

func TemplateDir(name string) string {
	return TemplatesDir + "/" + name
}

func TemplateSourcePath(name string) string {
	return TemplateDir(name) + "/" + SourceName
}

func TemplateTestPath(name string) string {
	return TemplateDir(name) + "/" + TestName
}

func TemplateHeaderPath(name string) string {
	return TemplateDir(name) + "/" + HeaderName
}

func ConstraintPath(name string) string {
	return ConstraintsDir + "/" + name + ".yaml"
}

func SuitePath(name string) string {
	return TestsDir + "/" + name + "/" + SuiteName
}

func SuiteCasePath(name, file string) string {
	return TestsDir + "/" + name + "/" + file
}

func DistPath(name string) string {
	return DistDir + "/" + name + ".yaml"
}

func ReportPath(codeBranch string) string {
	return ReportsDir + "/" + oabranch.Slug(codeBranch) + ".yaml"
}

func ChangePath(name string) string {
	return ChangesDir + "/" + name + ".yaml"
}

type PolicyLibrary struct {
	Repository string `json:"repository"`

	Version string `json:"version,omitempty"`

	TestGlobs []string `json:"testGlobs,omitempty"`

	DefaultEnforcement Enforcement `json:"defaultEnforcement,omitempty"`

	Roles map[string]RoleBinding `json:"roles,omitempty"`

	Vocabulary *Vocabulary `json:"vocabulary,omitempty"`

	Imports []PackImport `json:"imports,omitempty"`

	// Enforcement overrides the enforcementAction of the constraints the library
	// holds, matched by constraint name in order, the last match winning. It is
	// applied when the library is loaded, after the imports resolve, so a rule
	// reaches an imported pack's constraints exactly as it reaches the
	// repository's own: a run softens a pack's deny without editing the pack.
	Enforcement []EnforcementRule `json:"enforcement,omitempty"`

	// Members are other repositories the model is built over besides this one,
	// so a rule sees a flow that crosses a repository boundary.
	Members []Member `json:"members,omitempty"`

	// Classifiers names classifier packs under the library's classifiers/
	// directory that apply to this repository's code, so a repository whose
	// effect sites the shared packs do not know can name them without editing
	// its code. Empty reads the checkout's own classifiers/ directory.
	Classifiers []string `json:"classifiers,omitempty"`
}

func (l PolicyLibrary) EnforcementFallback() Enforcement {
	if l.DefaultEnforcement.Known() {
		return l.DefaultEnforcement
	}
	return EnforcementDryRun
}

// EnforcementRule gives the constraints whose name matches Name the action.
// Name is a glob, the way `path.Match` reads one; a constraint name carries no
// slash, so `*` matches every constraint and `provisioning-*` one family.
type EnforcementRule struct {
	Name string `json:"name"`

	Action Enforcement `json:"action"`
}

type Library struct {
	Manifest PolicyLibrary

	// Pack is the pack a directory declares in pack.yaml, when it is one.
	Pack *PackManifest

	// Imported names what an import contributed, keyed by a template's slug or
	// a constraint's name: value is the pack reference. An imported template
	// lives in the pack, not in the repository, so the branch and the kcp sync
	// carry the repository's own templates only.
	Imported map[string]string

	Templates []Template

	Constraints []Constraint

	Lib string

	Files map[string][]byte
}

// ImportedFrom names the pack a template slug or a constraint name came from.
func (l Library) ImportedFrom(key string) string {
	return l.Imported[key]
}

// ApplyEnforcement rewrites the enforcementAction of every constraint the
// library holds -- its own and the ones an imported pack contributed -- with
// the last matching rule of the manifest's enforcement list. A constraint no
// rule matches keeps the action it shipped with. A rule whose action is not
// deny, warn or dryrun is ignored.
func (l *Library) ApplyEnforcement() {
	for index := range l.Constraints {
		for _, rule := range l.Manifest.Enforcement {
			if !rule.Action.Known() {
				continue
			}
			matched, err := path.Match(rule.Name, l.Constraints[index].Name)
			if err == nil && matched {
				l.Constraints[index].Enforcement = rule.Action
			}
		}
	}
}

func (l Library) Constraint(name string) (Constraint, bool) {
	for _, constraint := range l.Constraints {
		if constraint.Name == name {
			return constraint, true
		}
	}
	return Constraint{}, false
}

func (l *Library) Sort() {
	sort.SliceStable(l.Templates, func(left, right int) bool {
		return l.Templates[left].Name < l.Templates[right].Name
	})
	sort.SliceStable(l.Constraints, func(left, right int) bool {
		return l.Constraints[left].Name < l.Constraints[right].Name
	})
}

func (l Library) TemplateOfKind(kind string) (Template, bool) {
	for _, template := range l.Templates {
		if template.Kind == kind {
			return template, true
		}
	}
	return Template{}, false
}

func (l Library) Template(name string) (Template, bool) {
	for _, template := range l.Templates {
		if template.Name == name {
			return template, true
		}
	}
	return Template{}, false
}

func (l Library) ConstraintsFor(template Template) []Constraint {
	out := []Constraint{}
	for _, constraint := range l.Constraints {
		if constraint.Kind == template.Kind {
			out = append(out, constraint)
		}
	}
	return out
}
