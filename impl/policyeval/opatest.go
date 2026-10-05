package policyeval

import (
	"context"
	"sort"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/tester"
)

type OpaTestResult struct {
	Name string

	Package string

	File string

	Fail bool

	Skip bool

	Error string
}

func (r OpaTestResult) Passed() bool {
	return !r.Fail && r.Error == ""
}

func RunOpaTests(ctx context.Context, modules map[string]string) ([]OpaTestResult, error) {
	parsed := map[string]*ast.Module{}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		module, err := ast.ParseModuleWithOpts(name, modules[name], ast.ParserOptions{RegoVersion: ast.RegoV0})
		if err != nil {
			return []OpaTestResult{{File: name, Fail: true, Error: err.Error()}}, nil
		}
		parsed[name] = module
	}

	runner := tester.NewRunner().
		SetStore(inmem.NewFromObject(map[string]any{})).
		SetModules(parsed).
		SetDefaultRegoVersion(ast.RegoV0)
	results, err := runner.RunTests(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := []OpaTestResult{}
	for result := range results {
		item := OpaTestResult{
			Name:    result.Name,
			Package: result.Package,
			Fail:    result.Fail,
			Skip:    result.Skip,
		}
		if result.Location != nil {
			item.File = result.Location.File
		}
		if result.Error != nil {
			item.Error = result.Error.Error()
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Package != out[right].Package {
			return out[left].Package < out[right].Package
		}
		return out[left].Name < out[right].Name
	})
	return out, nil
}
