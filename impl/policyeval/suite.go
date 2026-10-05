package policyeval

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/verify"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

type SuiteResult struct {
	Path string

	Name string

	Tests []TestResult
}

type TestResult struct {
	Name string

	Error string

	Cases []CaseResult
}

type CaseResult struct {
	Name string

	Error string

	Skipped bool

	Violations int

	Messages []string
}

func (s SuiteResult) Passed() bool {
	for _, test := range s.Tests {
		if test.Error != "" {
			return false
		}
		for _, item := range test.Cases {
			if item.Error != "" {
				return false
			}
		}
	}
	return true
}

func (s SuiteResult) Cases() int {
	total := 0
	for _, test := range s.Tests {
		total += len(test.Cases)
	}
	return total
}

func RunSuite(ctx context.Context, fsys fs.FS, suitePath string) (SuiteResult, error) {
	data, err := fs.ReadFile(fsys, suitePath)
	if err != nil {
		return SuiteResult{}, err
	}
	var suite verify.Suite
	if err := yaml.Unmarshal(data, &suite); err != nil {
		return SuiteResult{}, fmt.Errorf("policyeval: suite %s: %w", suitePath, err)
	}
	dir := path.Dir(suitePath)
	result := SuiteResult{Path: suitePath, Name: suite.Name}
	for _, test := range suite.Tests {
		result.Tests = append(result.Tests, runTest(ctx, fsys, dir, test))
	}
	return result, nil
}

func runTest(ctx context.Context, fsys fs.FS, dir string, test verify.Test) TestResult {
	result := TestResult{Name: test.Name}
	if test.Skip {
		return result
	}
	library, err := suiteLibrary(fsys, dir, test)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for _, item := range test.Cases {
		if item == nil {
			continue
		}
		result.Cases = append(result.Cases, runCase(ctx, fsys, dir, library, test, *item))
	}
	return result
}

func suiteLibrary(fsys fs.FS, dir string, test verify.Test) (policy.Library, error) {
	library := policy.Library{}
	templateData, err := fs.ReadFile(fsys, path.Join(dir, test.Template))
	if err != nil {
		return policy.Library{}, err
	}
	template, err := policy.ParseTemplate(templateData, nil)
	if err != nil {
		return policy.Library{}, err
	}
	if !hasLib(template.Libs) {
		template.Libs = append(template.Libs, Lib())
	}
	library.Templates = []policy.Template{template}

	if test.Constraint == "" {
		return policy.Library{}, fmt.Errorf("policyeval: test %s has no constraint", test.Name)
	}
	constraintData, err := fs.ReadFile(fsys, path.Join(dir, test.Constraint))
	if err != nil {
		return policy.Library{}, err
	}
	constraint, err := policy.ParseConstraint(constraintData, template.Name)
	if err != nil {
		return policy.Library{}, err
	}
	library.Constraints = []policy.Constraint{constraint}
	return library, nil
}

func hasLib(libs []string) bool {
	for _, lib := range libs {
		if strings.Contains(lib, "package lib.specd") {
			return true
		}
	}
	return false
}

func runCase(ctx context.Context, fsys fs.FS, dir string, library policy.Library, test verify.Test, item verify.Case) CaseResult {
	result := CaseResult{Name: item.Name}
	if item.Skip {
		result.Skipped = true
		return result
	}
	engine, err := NewEngine(ctx, library, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for _, inventoryPath := range item.Inventory {
		objects, err := readObjects(fsys, path.Join(dir, inventoryPath))
		if err != nil {
			result.Error = err.Error()
			return result
		}
		if err := engine.AddInventory(ctx, objects); err != nil {
			result.Error = err.Error()
			return result
		}
	}
	objects, err := readObjects(fsys, path.Join(dir, item.Object))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(objects) != 1 {
		result.Error = fmt.Sprintf("policyeval: %s defines %d objects, want 1", item.Object, len(objects))
		return result
	}
	results, err := engine.ReviewRaw(ctx, objects[0])
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Violations = len(results)
	for _, one := range results {
		result.Messages = append(result.Messages, one.Msg)
	}
	assertions := item.Assertions
	if len(assertions) == 0 {
		result.Error = "assertions must be non-empty"
		return result
	}
	for index := range assertions {
		if err := assertions[index].Run(results); err != nil {
			result.Error = err.Error()
			return result
		}
	}
	return result
}

func readObjects(fsys fs.FS, name string) ([]*unstructured.Unstructured, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	out := []*unstructured.Unstructured{}
	for _, document := range SplitDocuments(data) {
		object, err := policy.Unstructured(document)
		if err != nil {
			return nil, fmt.Errorf("policyeval: %s: %w", name, err)
		}
		out = append(out, object)
	}
	return out, nil
}
