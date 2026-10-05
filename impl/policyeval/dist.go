package policyeval

import (
	"fmt"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

func Dist(library policy.Library) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, template := range library.Templates {
		built := template
		built.Libs = []string{Lib()}
		encoded, err := built.Header()
		if err != nil {
			return nil, err
		}
		out[policy.DistPath(policy.TemplateSlug(built))] = encoded
	}
	return out, nil
}

func Catalogue(library policy.Library) []byte {
	builder := strings.Builder{}
	builder.WriteString("# Policy catalogue\n\n")
	if library.Manifest.Repository != "" {
		fmt.Fprintf(&builder, "repository: %s\n\n", library.Manifest.Repository)
	}
	builder.WriteString("| policy | title | level | severity | requirements | constraints |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, template := range library.Templates {
		constraints := library.ConstraintsFor(template)
		actions := make([]string, 0, len(constraints))
		for _, constraint := range constraints {
			actions = append(actions, fmt.Sprintf("%s (%s)", constraint.Name, constraint.Enforcement))
		}
		requirements := strings.Join(template.Requirements, ", ")
		if requirements == "" {
			requirements = "-"
		}
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %s | %s |\n",
			policy.TemplateSlug(template),
			template.Title,
			template.Level,
			template.Severity,
			requirements,
			strings.Join(actions, ", "),
		)
	}
	return []byte(builder.String())
}
