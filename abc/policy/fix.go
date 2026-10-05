package policy

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const (
	AnnotationFixPrompt = Group + "/fix-prompt"

	AnnotationFixConstraint = Group + "/fix-constraint"

	AnnotationFixSite = Group + "/fix-site"
)

// FixChange is the SpecChange a fix request becomes: a spec-to-code change on
// the violation's SystemContext. The prompt rides on it as an annotation, so
// the spec flow the repository already runs realizes the fix and a reader of
// the change sees the violation it answers.
func FixChange(request FixRequest, name, systemContext, specHash string) *spec.SpecChange {
	annotations := map[string]string{
		AnnotationFixPrompt:     request.Prompt,
		AnnotationFixConstraint: request.Constraint,
	}
	if request.Site != "" {
		annotations[AnnotationFixSite] = request.Site
	}
	change := &spec.SpecChange{
		ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: annotations},
		Spec: spec.SpecChangeSpec{
			SystemContext: systemContext,
			Direction:     specapi.DirectionSpecToCode,
			ToSpecHash:    specHash,
		},
	}
	change.SetDefaults()
	return change
}

// FixRequest is what `specctl policy fix` hands to the normal spec flow: the
// violation, the context it was found in, and the instruction a spec-to-code
// change would carry. It is a request, not a change: the spec flow reviews it
// and decides how the code or the spec answers it.
type FixRequest struct {
	Repository string `json:"repository"`

	Constraint string `json:"constraint"`

	Policy string `json:"policy,omitempty"`

	Level Level `json:"level,omitempty"`

	Object string `json:"object"`

	Site string `json:"site,omitempty"`

	Message string `json:"message"`

	Title string `json:"title,omitempty"`

	Requirements []string `json:"requirements,omitempty"`

	Prompt string `json:"prompt"`
}

func BuildFixRequest(library Library, violation Violation) FixRequest {
	title := violation.Title
	if title == "" {
		if template, ok := library.Template(violation.Policy); ok {
			title = template.Title
		}
	}
	file, line := Site(violation)
	site := ""
	if file != "" {
		site = fmt.Sprintf("%s:%d", file, line)
	}
	requirements := append([]string{}, violation.Requirements...)
	sort.Strings(requirements)
	request := FixRequest{
		Repository:   library.Manifest.Repository,
		Constraint:   violation.Constraint,
		Policy:       violation.Policy,
		Level:        violation.Level,
		Object:       violation.Object.String(),
		Site:         site,
		Message:      violation.Msg,
		Title:        title,
		Requirements: requirements,
	}
	request.Prompt = fixPrompt(request)
	return request
}

func fixPrompt(request FixRequest) string {
	builder := strings.Builder{}
	builder.WriteString("A policy violation must be fixed in ")
	builder.WriteString(request.Repository)
	builder.WriteString(".\n\n")
	if request.Title != "" {
		builder.WriteString("Rule: ")
		builder.WriteString(request.Title)
		builder.WriteString("\n")
	}
	builder.WriteString("Constraint: ")
	builder.WriteString(request.Constraint)
	if request.Level != "" {
		builder.WriteString(" (")
		builder.WriteString(string(request.Level))
		builder.WriteString(")")
	}
	builder.WriteString("\n")
	builder.WriteString("Object: ")
	builder.WriteString(request.Object)
	builder.WriteString("\n")
	if request.Site != "" {
		builder.WriteString("Site: ")
		builder.WriteString(request.Site)
		builder.WriteString("\n")
	}
	builder.WriteString("\nThe violation reads:\n\n")
	builder.WriteString(request.Message)
	builder.WriteString("\n\n")
	if len(request.Requirements) > 0 {
		builder.WriteString("Requirements the rule enforces: ")
		builder.WriteString(strings.Join(request.Requirements, ", "))
		builder.WriteString("\n\n")
	}
	builder.WriteString("Change the code or the spec so the rule no longer fires at this site, without weakening the rule. ")
	builder.WriteString("If the violation is accepted rather than fixed, waive it instead: specctl policy waive ")
	builder.WriteString("with the finding's key and a reason.\n")
	return builder.String()
}
