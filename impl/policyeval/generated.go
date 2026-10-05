package policyeval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/verify"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

// GeneratedInput is one generated policy tree and everything the checks need:
// the binding it must stay portable over, the repository it joins and the head
// model it is evaluated against.
type GeneratedInput struct {
	Dir string

	Slug string

	Binding policy.Binding

	Repository string

	Contexts []string

	Vocabulary policy.MutationVocabulary

	GeneratedBy string

	Requirements []string

	Commit string

	Reviewed []*unstructured.Unstructured

	Inventory []*unstructured.Unstructured
}

// GeneratedResult is what the checks found: the library the tree holds, one
// record per check and the violations the rule raises against the head model.
type GeneratedResult struct {
	Library policy.Library

	Checks []policy.Check

	Report policy.Report
}

func (r GeneratedResult) Passed() bool {
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return len(r.Checks) > 0
}

func (r GeneratedResult) Failures() []string {
	out := []string{}
	for _, check := range r.Checks {
		if !check.Passed {
			out = append(out, check.Name+": "+check.Message)
		}
	}
	return out
}

func (r GeneratedResult) Template() (policy.Template, bool) {
	if len(r.Library.Templates) != 1 {
		return policy.Template{}, false
	}
	return r.Library.Templates[0], true
}

func (r GeneratedResult) Constraint() (policy.Constraint, bool) {
	if len(r.Library.Constraints) != 1 {
		return policy.Constraint{}, false
	}
	return r.Library.Constraints[0], true
}

// CheckGenerated author-policy checks a generated tree: the annotations the
// request demands, a portable source, a strict compile, the opa unit tests,
// the gator suite, the mutation check and the evaluation against the head
// model. A failed check is a result, not an error; the error is reserved for a
// tree that cannot be read at all.
func CheckGenerated(ctx context.Context, input GeneratedInput) (GeneratedResult, error) {
	result := GeneratedResult{}
	if err := AnnotateGenerated(input.Dir, input.Slug, input.Requirements, input.GeneratedBy); err != nil {
		result.Checks = append(result.Checks, failCheck("annotations", err.Error()))
		return result, nil
	}
	result.Checks = append(result.Checks, policy.Check{Name: "annotations", Passed: true})
	library, err := LoadRaw(os.DirFS(input.Dir))
	if err != nil {
		return result, fmt.Errorf("policyeval: read the generated tree: %w", err)
	}
	result.Library = library

	if check := checkShape(library, input.Slug); !check.Passed {
		result.Checks = append(result.Checks, check)
		return result, nil
	}
	result.Checks = append(result.Checks, policy.Check{Name: "shape", Passed: true, Message: input.Slug})

	template := library.Templates[0]
	if findings := policy.PortabilityFindings(template.Rego, policy.ForbiddenIdentifiers(input.Binding, input.Repository, input.Contexts)); len(findings) > 0 {
		result.Checks = append(result.Checks, failCheck("portable",
			"the rule names "+strings.Join(findings, ", ")+"; a portable rule reads roles and vocabulary classes only"))
	} else {
		result.Checks = append(result.Checks, policy.Check{Name: "portable", Passed: true})
	}

	if _, err := NewEngine(ctx, library, nil); err != nil {
		result.Checks = append(result.Checks, failCheck("compile", err.Error()))
		return result, nil
	}
	result.Checks = append(result.Checks, policy.Check{Name: "compile", Passed: true})

	units, err := runGeneratedUnits(ctx, input.Dir, library, input.Slug)
	if err != nil {
		result.Checks = append(result.Checks, failCheck("units", err.Error()))
	} else if failed := failingUnits(units); len(failed) > 0 {
		result.Checks = append(result.Checks, failCheck("units", strings.Join(failed, "; ")))
	} else {
		result.Checks = append(result.Checks, policy.Check{Name: "units", Passed: true,
			Message: fmt.Sprintf("%d opa test(s)", len(units))})
	}

	dist, err := Dist(library)
	if err != nil {
		return result, err
	}
	for name, data := range dist {
		target := filepath.Join(input.Dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return result, err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return result, err
		}
	}
	suites, err := runGeneratedSuites(ctx, input.Dir)
	if err != nil {
		result.Checks = append(result.Checks, failCheck("suite", err.Error()))
	} else {
		result.Checks = append(result.Checks, suites)
	}

	result.Checks = append(result.Checks, mutationCheck(ctx, input, library))

	report, err := Evaluate(ctx, Evaluation{
		Library:    library,
		Repository: input.Repository,
		Commit:     input.Commit,
		Reviewed:   input.Reviewed,
		Inventory:  input.Inventory,
	})
	if err != nil {
		result.Checks = append(result.Checks, failCheck("head evaluation", err.Error()))
		return result, nil
	}
	result.Report = report
	result.Checks = append(result.Checks, policy.Check{Name: "head evaluation", Passed: true,
		Message: fmt.Sprintf("%d violation(s) against %s", len(report.Violations), shortCommit(input.Commit))})
	return result, nil
}

// AnnotateGenerated writes the annotations the request demands into the
// generated template, so the requirements the operator named and the
// PolicyChange that generated it are recorded even when the harness left them
// out, and the severity follows the level.
func AnnotateGenerated(dir, slug string, requirements []string, generatedBy string) error {
	name := filepath.Join(dir, filepath.FromSlash(policy.TemplateHeaderPath(slug)))
	data, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("the generated tree has no %s: %w", policy.TemplateHeaderPath(slug), err)
	}
	document := map[string]any{}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("%s: %w", policy.TemplateHeaderPath(slug), err)
	}
	metadata, _ := document["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
		document["metadata"] = metadata
	}
	annotations, _ := metadata["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
		metadata["annotations"] = annotations
	}
	annotations[policy.AnnotationSlug] = slug
	if generatedBy != "" {
		annotations[policy.AnnotationGeneratedBy] = generatedBy
	}
	if len(requirements) > 0 {
		annotations[policy.AnnotationRequirements] = strings.Join(requirements, ",")
	}
	if level, _ := annotations[policy.AnnotationLevel].(string); level != "" {
		if _, ok := annotations[policy.AnnotationSeverity]; !ok {
			annotations[policy.AnnotationSeverity] = string(policy.SeverityForLevel(policy.Level(level)))
		}
	}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return err
	}
	return os.WriteFile(name, encoded, 0o644)
}

func checkShape(library policy.Library, slug string) policy.Check {
	if len(library.Templates) != 1 || len(library.Constraints) != 1 {
		return failCheck("shape", fmt.Sprintf("the tree carries %d template(s) and %d constraint(s), want one of each",
			len(library.Templates), len(library.Constraints)))
	}
	if actual := policy.TemplateSlug(library.Templates[0]); actual != slug {
		return failCheck("shape", fmt.Sprintf("the template is %q, want %q", actual, slug))
	}
	constraint := library.Constraints[0]
	if constraint.Enforcement == "" || !constraint.Enforcement.Known() {
		return failCheck("shape", fmt.Sprintf("the constraint %s names no enforcement action", constraint.Name))
	}
	if !matchesModel(constraint) {
		return failCheck("shape", fmt.Sprintf("the constraint %s does not review the ArchitectureModel", constraint.Name))
	}
	return policy.Check{Name: "shape", Passed: true}
}

func matchesModel(constraint policy.Constraint) bool {
	for _, kind := range constraint.Match.Kinds {
		if slices.Contains(kind.Kinds, policy.ArchitectureModelKind) {
			return true
		}
	}
	return false
}

func runGeneratedUnits(ctx context.Context, dir string, library policy.Library, slug string) ([]OpaTestResult, error) {
	modules := map[string]string{policy.LibPath: Lib()}
	modules[policy.TemplateSourcePath(slug)] = library.Templates[0].Rego
	if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(policy.TemplateTestPath(slug)))); err == nil {
		modules[policy.TemplateTestPath(slug)] = string(data)
	}
	return RunOpaTests(ctx, modules)
}

func failingUnits(results []OpaTestResult) []string {
	out := []string{}
	for _, result := range results {
		if !result.Passed() {
			out = append(out, result.Package+"."+result.Name+": "+result.Error)
		}
	}
	sort.Strings(out)
	return out
}

func runGeneratedSuites(ctx context.Context, dir string) (policy.Check, error) {
	suites, err := filepath.Glob(filepath.Join(dir, policy.TestsDir, "*", policy.SuiteName))
	if err != nil {
		return policy.Check{}, err
	}
	if len(suites) == 0 {
		return failCheck("suite", "the tree carries no gator suite"), nil
	}
	sort.Strings(suites)
	cases := 0
	for _, suite := range suites {
		relative, err := filepath.Rel(dir, suite)
		if err != nil {
			return policy.Check{}, err
		}
		result, err := RunSuite(ctx, os.DirFS(dir), filepath.ToSlash(relative))
		if err != nil {
			return policy.Check{}, fmt.Errorf("%s: %w", relative, err)
		}
		cases += result.Cases()
		if !result.Passed() {
			return failCheck("suite", suiteFailure(result)), nil
		}
	}
	return policy.Check{Name: "suite", Passed: true, Message: fmt.Sprintf("%d case(s)", cases)}, nil
}

func suiteFailure(result SuiteResult) string {
	problems := []string{}
	for _, test := range result.Tests {
		if test.Error != "" {
			problems = append(problems, test.Name+": "+test.Error)
		}
		for _, item := range test.Cases {
			if item.Error != "" {
				problems = append(problems, test.Name+"/"+item.Name+": "+item.Error)
			}
		}
	}
	if len(problems) == 0 {
		return result.Name + " failed"
	}
	return strings.Join(problems, "; ")
}

// mutationCheck derives a denying case from the allowed fixture of the suite
// and refuses a rule that denies none of them: a rule that only recognizes the
// words of the sentence is vacuous.
func mutationCheck(ctx context.Context, input GeneratedInput, library policy.Library) policy.Check {
	model, found, err := allowedFixture(input.Dir)
	if err != nil {
		return failCheck("mutation", err.Error())
	}
	if !found {
		return failCheck("mutation", "the suite carries no allowed ArchitectureModel case to mutate")
	}
	mutations := policy.Mutations(model, input.Vocabulary)
	if len(mutations) == 0 {
		return failCheck("mutation", "the allowed fixture carries no role the vocabulary names, so no mutation applies")
	}
	engine, err := NewEngine(ctx, library, nil)
	if err != nil {
		return failCheck("mutation", err.Error())
	}
	denied := []string{}
	tried := []string{}
	for _, mutation := range mutations {
		tried = append(tried, mutation.Name)
		object, err := modelObject(mutation.Model)
		if err != nil {
			return failCheck("mutation", err.Error())
		}
		// The rule reads the model from the inventory, so the mutated case is
		// both the reviewed object and the inventory entry, the way a suite
		// case lists its own object.
		if err := engine.AddInventory(ctx, []*unstructured.Unstructured{object}); err != nil {
			return failCheck("mutation", err.Error())
		}
		violations, err := engine.Review(ctx, object)
		if err != nil {
			return failCheck("mutation", err.Error())
		}
		if len(violations) > 0 {
			denied = append(denied, mutation.Name)
		}
	}
	if len(denied) == 0 {
		return failCheck("mutation", "the rule denies none of the derived cases: "+strings.Join(tried, ", "))
	}
	return policy.Check{Name: "mutation", Passed: true, Message: "denies " + strings.Join(denied, ", ")}
}

func modelObject(model policy.ArchitectureModel) (*unstructured.Unstructured, error) {
	model.APIVersion = policy.APIVersion
	model.Kind = policy.ArchitectureModelKind
	if model.Metadata.Name == "" {
		model.Metadata.Name = "mutation"
		model.Metadata.Namespace = "default"
	}
	encoded, err := yaml.Marshal(model)
	if err != nil {
		return nil, err
	}
	return Unstructured(encoded)
}

// allowedFixture finds the ArchitectureModel of a suite case that asserts zero
// violations, and its inventory.
func allowedFixture(dir string) (policy.ArchitectureModel, bool, error) {
	suites, err := filepath.Glob(filepath.Join(dir, policy.TestsDir, "*", policy.SuiteName))
	if err != nil {
		return policy.ArchitectureModel{}, false, err
	}
	sort.Strings(suites)
	for _, suitePath := range suites {
		data, err := os.ReadFile(suitePath)
		if err != nil {
			return policy.ArchitectureModel{}, false, err
		}
		var suite verify.Suite
		if err := yaml.Unmarshal(data, &suite); err != nil {
			return policy.ArchitectureModel{}, false, fmt.Errorf("%s: %w", suitePath, err)
		}
		base := filepath.Dir(suitePath)
		for _, test := range suite.Tests {
			for _, item := range test.Cases {
				if item == nil || !expectsNoViolations(*item) {
					continue
				}
				objects, err := readObjects(os.DirFS(dir), filepath.ToSlash(filepath.Join(relativeTo(dir, base), item.Object)))
				if err != nil {
					return policy.ArchitectureModel{}, false, err
				}
				for _, object := range objects {
					if object.GetKind() != policy.ArchitectureModelKind {
						continue
					}
					model, err := decodeModel(object)
					if err != nil {
						return policy.ArchitectureModel{}, false, err
					}
					return model, true, nil
				}
			}
		}
	}
	return policy.ArchitectureModel{}, false, nil
}

func expectsNoViolations(item verify.Case) bool {
	for index := range item.Assertions {
		assertion := &item.Assertions[index]
		if assertion.Violations == nil {
			continue
		}
		if assertion.Violations.IntValue() == 0 {
			return true
		}
		if value := assertion.Violations.StrVal; value == "no" {
			return true
		}
	}
	return false
}

func relativeTo(dir, sub string) string {
	relative, err := filepath.Rel(dir, sub)
	if err != nil {
		return sub
	}
	return relative
}

func decodeModel(object *unstructured.Unstructured) (policy.ArchitectureModel, error) {
	encoded, err := yaml.Marshal(object.Object)
	if err != nil {
		return policy.ArchitectureModel{}, err
	}
	model := policy.ArchitectureModel{}
	if err := yaml.Unmarshal(encoded, &model); err != nil {
		return policy.ArchitectureModel{}, err
	}
	return model, nil
}

func failCheck(name, message string) policy.Check {
	return policy.Check{Name: name, Passed: false, Message: message}
}

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	if commit == "" {
		return "the working tree"
	}
	return commit
}
