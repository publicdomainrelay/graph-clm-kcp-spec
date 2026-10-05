package policyeval_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	policyglob "github.com/publicdomainrelay/graph-clm-kcp-spec/common/glob"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/policyeval"
)

var globCases = []struct {
	pattern string
	path    string
	want    bool
}{
	{"hono-bidder/**", "hono-bidder/src/x.ts", true},
	{"hono-bidder/**", "hono-market/src/x.ts", false},
	{"test/*", "test/foo.ts", true},
	{"test/*", "test/nested/foo.ts", false},
	{"*.ts", "test/foo.ts", false},
	{"**/*.ts", "test/foo.ts", true},
	{"lib/*/mod.ts", "lib/a/mod.ts", true},
	{"lib/*/mod.ts", "lib/a/b/mod.ts", false},
	{"lib/**/mod.ts", "lib/a/b/mod.ts", true},
	{"exact/path.ts", "exact/path.ts", true},
}

func TestGlobDialectAgreesBetweenGoAndRego(t *testing.T) {
	var module strings.Builder
	module.WriteString("package globconformance\n\nimport data.lib.specd\n\n")
	for index, test := range globCases {
		if test.want {
			fmt.Fprintf(&module, "test_case_%d {\n\tspecd.globs_match([%q], %q)\n}\n\n", index, test.pattern, test.path)
			continue
		}
		fmt.Fprintf(&module, "test_case_%d {\n\tnot specd.globs_match([%q], %q)\n}\n\n", index, test.pattern, test.path)
	}
	results, err := policyeval.RunOpaTests(context.Background(), map[string]string{
		"conformance.rego": module.String(),
		"lib.rego":         policyeval.Lib(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rego := map[string]bool{}
	for _, result := range results {
		rego[result.Name] = result.Passed()
	}
	for index, test := range globCases {
		name := fmt.Sprintf("test_case_%d", index)
		if passed, ok := rego[name]; !ok || !passed {
			t.Errorf("rego: glob.match(%q) on %q did not agree with want %v (%s)", test.pattern, test.path, test.want, name)
		}
		if got := policyglob.Match(test.pattern, test.path); got != test.want {
			t.Errorf("go: glob.Match(%q, %q) = %v, want %v", test.pattern, test.path, got, test.want)
		}
	}
}
