package claudecli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/policy"
)

const (
	EnvPolicyMode = "SPECD_POLICY_MODE"

	EnvPolicySlug = "SPECD_POLICY_SLUG"
)

// Generate asks the model to author a policy tree or a binding under the
// request's directory, the same way Realize asks it to edit a working tree.
func (a *Agent) Generate(ctx context.Context, request policy.GenerateRequest) (policy.GenerateResult, error) {
	if request.Dir == "" {
		return policy.GenerateResult{}, fmt.Errorf("claudecli: the generate request names no directory")
	}
	scoped := *a
	scoped.options.Dir = request.Dir
	env := map[string]string{
		EnvRoot:       request.Dir,
		EnvRepository: request.Repository,
		EnvPolicyMode: request.Mode,
		EnvPolicySlug: request.Slug,
	}
	stdout, stderr, err := scoped.run(ctx, GeneratePrompt(request), env)
	result := policy.GenerateResult{Summary: firstLine(stdout), Log: tail(stdout + "\n" + stderr)}
	if err != nil {
		return result, fmt.Errorf("claudecli: generate %s: %w", request.Slug, err)
	}
	return result, nil
}

// GeneratePrompt is the instruction the harness reads: the mode, the invariant
// in the operator's words, the binding in force and the exact files the tree
// must carry.
func GeneratePrompt(request policy.GenerateRequest) string {
	builder := strings.Builder{}
	if request.Mode == policy.GenerateModeBind {
		writeBindingPrompt(&builder, request)
		return builder.String()
	}
	builder.WriteString("You author one Gatekeeper policy for a spec-driven repository.\n\n")
	builder.WriteString("The policy reads the ArchitectureModel: components, roles, effects, flows and triggers. ")
	builder.WriteString("It must never name a repository, a SystemContext, a file path or a symbol of one codebase. ")
	builder.WriteString("Roles and the vocabulary classes of the binding are the only names it may use; the roles and the classes it needs are constraint parameters with the defaults below.\n\n")

	builder.WriteString("## the invariant, in the operator's words\n\n")
	builder.WriteString(strings.TrimSpace(request.Prompt))
	builder.WriteString("\n\n")
	if len(request.Requirements) > 0 {
		builder.WriteString("The policy enforces these requirements: " + strings.Join(request.Requirements, ", ") + ".\n\n")
	}
	fmt.Fprintf(&builder, "The constraint's enforcementAction is %s.\n\n", defaultEnforcement(request.Enforcement))

	builder.WriteString("## the binding in force\n\n")
	builder.WriteString(renderBinding(request))
	builder.WriteString("\n")
	if request.Model != "" {
		builder.WriteString("## the ArchitectureModel of the head commit\n\n")
		builder.WriteString(request.Model)
		builder.WriteString("\n")
	}

	builder.WriteString(policyInstructions(request))
	return builder.String()
}

func writeBindingPrompt(builder *strings.Builder, request policy.GenerateRequest) {
	builder.WriteString("You write the binding of one repository for a portable policy pack.\n\n")
	builder.WriteString("A pack's templates read the ArchitectureModel, not code: they resolve roles to components and code to abstract classes. ")
	builder.WriteString("Your job is the per-repository half: the roles and the vocabulary of policies.yaml. You do not write a rule.\n\n")
	if request.Pack != nil {
		fmt.Fprintf(builder, "## the pack %s\n\n%s\n\n", request.Pack.Reference(), strings.TrimSpace(request.Pack.Description))
		builder.WriteString("It requires these roles: " + strings.Join(request.Pack.Roles, ", ") + ".\n")
		builder.WriteString("It reads these vocabulary classes: " + strings.Join(request.Pack.Vocabulary, ", ") + ".\n\n")
	}
	if roles := request.Binding.RoleNames(); len(roles) > 0 {
		builder.WriteString("Use these role names: " + strings.Join(roles, ", ") + ".\n\n")
	}
	if request.Model != "" {
		builder.WriteString("## the ArchitectureModel of the head commit\n\n")
		builder.WriteString(request.Model)
		builder.WriteString("\n")
	}
	if len(request.Contexts) > 0 {
		builder.WriteString("## the SystemContexts\n\n")
		for _, context := range request.Contexts {
			fmt.Fprintf(builder, "- %s\n", context.Name)
		}
		builder.WriteString("\n")
	}
	builder.WriteString("Write `policies.yaml` in the working directory: a policy library manifest whose `roles` select the components of each pack role (by `contexts`, `globs`, `symbols`, `labels` or `targets`) and whose `vocabulary` maps every class the pack reads to the terms the code and the specs use. ")
	builder.WriteString("Do not change the pack import, the repository name or the enforcement the manifest already carries.\n")
}

func defaultEnforcement(enforcement policy.Enforcement) policy.Enforcement {
	if enforcement.Known() {
		return enforcement
	}
	return policy.EnforcementDryRun
}

// renderBinding spells the binding in force: the roles the model resolves and
// the terms of every vocabulary class, so the rule speaks the project's
// language through the classes only.
func renderBinding(request policy.GenerateRequest) string {
	builder := strings.Builder{}
	roles := request.Binding.RoleNames()
	if len(roles) == 0 {
		builder.WriteString("no role is declared yet; take the role names from the pack and the vocabulary\n\n")
	}
	for _, role := range roles {
		fmt.Fprintf(&builder, "- role %s\n", role)
	}
	groups := []struct {
		group   string
		classes map[string][]string
	}{
		{"channels", request.Binding.Vocabulary.Channels},
		{"events", request.Binding.Vocabulary.Events},
		{"payloads", request.Binding.Vocabulary.Payloads},
		{"purposes", request.Binding.Vocabulary.Purposes},
		{"routes", request.Binding.Vocabulary.Routes},
	}
	for _, group := range groups {
		names := make([]string, 0, len(group.classes))
		for name := range group.classes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&builder, "- %s/%s = %s\n", group.group, name, strings.Join(group.classes[name], ", "))
		}
	}
	return builder.String()
}

func policyInstructions(request policy.GenerateRequest) string {
	slug := request.Slug
	kind := kindOfSlug(slug)
	builder := strings.Builder{}
	builder.WriteString("## write exactly these files\n\n")
	fmt.Fprintf(&builder, "templates/%s/template.yaml\n", slug)
	fmt.Fprintf(&builder, "templates/%s/src.rego\n", slug)
	fmt.Fprintf(&builder, "templates/%s/src_test.rego\n", slug)
	fmt.Fprintf(&builder, "constraints/%s.yaml\n", slug)
	fmt.Fprintf(&builder, "tests/%s/suite.yaml\n", slug)
	fmt.Fprintf(&builder, "tests/%s/inventory/allowed.yaml\n", slug)
	fmt.Fprintf(&builder, "tests/%s/inventory/denied.yaml\n", slug)
	builder.WriteString("\n### template.yaml\n\n")
	builder.WriteString("apiVersion templates.gatekeeper.sh/v1, kind ConstraintTemplate. \n")
	fmt.Fprintf(&builder, "metadata.name is the lowercase kind %q (letters only).\n", kind)
	builder.WriteString("metadata.annotations carry:\n")
	builder.WriteString("  specs.publicdomainrelay.dev/title: one line\n")
	builder.WriteString("  specs.publicdomainrelay.dev/level: MUST or SHOULD\n")
	builder.WriteString("  specs.publicdomainrelay.dev/severity: error or warning\n")
	if len(request.Requirements) > 0 {
		builder.WriteString("  specs.publicdomainrelay.dev/requirements: " + strings.Join(request.Requirements, ",") + "\n")
	}
	fmt.Fprintf(&builder, "  specs.publicdomainrelay.dev/slug: %s\n", slug)
	builder.WriteString("spec.crd.spec.names.kind is the CamelCase kind, and spec.crd.spec.validation.openAPIV3Schema declares every parameter the rego reads, with the same defaults.\n")
	builder.WriteString("spec.targets is one entry: target admission.k8s.gatekeeper.sh, rego empty, libs empty.\n\n")

	builder.WriteString("### src.rego\n\n")
	fmt.Fprintf(&builder, "package %s\n\n", packageOfSlug(slug))
	builder.WriteString("import data.lib.specd\n\n")
	builder.WriteString("Read every role and class from input.parameters with object.get, the default being the name the pack uses. ")
	builder.WriteString("Report with violation[specd.violation(msg, details)] and put file and line in details.\n\n")
	builder.WriteString("lib.specd API: code_graph, contexts, context(name), repository, requirement(ctx, id), files_matching(globs), tests_matching(globs), nodes_in_files(paths), nodes_in_context(name), nodes_named(regex), nodes_qualified(name), calls_from(id), callers_of(id), reachable_from(ids, kinds), reaching(ids, kinds), lines_matching(path, regex), node_text_matches(node, regex), location(file, line), violation(msg, details) and the model helpers model_effects, model_effect(id), model_roles, model_vocabulary, vocabulary_terms, component_in_role(component, role), initiator_in_role(flow, role), acted_on_in_role(flow, role), event_class(effect, class), route_class(effect, class), file_in_role(file, globs), model_flows, model_triggers.\n\n")
	builder.WriteString("A flow is {from, to, initiator, channel, carries, purpose, level, forbidden, source, evidence}; initiator is the role that initiates; evidence names effect ids. An effect is {id, kind, component, context, attrs, file, line, node}; kinds are net.dial, net.listen, http.request, http.handle, proc.exec, ssh.connect, container.exec, event.emit, event.receive, secret.read, file.write.\n\n")
	builder.WriteString("### src_test.rego\n\n")
	builder.WriteString("opa unit tests in the same package, each one asserting `count(violation) == 0` or `count(violation) == 1` with `input` and `data.inventory` set inline. Name at least one test for the allowed case and one for the denied case.\n\n")

	builder.WriteString("### constraints/<slug>.yaml\n\n")
	builder.WriteString("apiVersion constraints.gatekeeper.sh/v1beta1, kind the CamelCase kind, metadata.name the slug. ")
	fmt.Fprintf(&builder, "spec.enforcementAction %s. ", defaultEnforcement(request.Enforcement))
	builder.WriteString("spec.match.kinds is one entry: apiGroups [specs.publicdomainrelay.dev], kinds [ArchitectureModel]. ")
	builder.WriteString("spec.parameters carries every parameter with its value.\n\n")

	builder.WriteString("### tests/<slug>/suite.yaml\n\n")
	builder.WriteString("apiVersion test.gatekeeper.sh/v1alpha1, kind Suite. One test whose template is ../../dist/<slug>.yaml and whose constraint is ../../constraints/<slug>.yaml, with two cases: ")
	builder.WriteString("`object: inventory/allowed.yaml` with `assertions: [{violations: 0}]`, and `object: inventory/denied.yaml` with `assertions: [{violations: 1}]`. ")
	builder.WriteString("Each case also lists its object under `inventory:`.\n\n")

	builder.WriteString("### tests/<slug>/inventory/allowed.yaml and denied.yaml\n\n")
	builder.WriteString("One ArchitectureModel object each: apiVersion specs.publicdomainrelay.dev/v1alpha1, kind ArchitectureModel, metadata name and namespace default. ")
	builder.WriteString("spec carries repository, roles, vocabulary, components, effects, flows and triggers. ")
	builder.WriteString("The allowed model satisfies the invariant; the denied model breaks it in exactly one way. ")
	builder.WriteString("The inventory file is the object itself, so the case reviews the same model it feeds the rule.\n\n")

	builder.WriteString("## rules\n\n")
	builder.WriteString("- Write files only. Do not run git and do not commit.\n")
	builder.WriteString("- The rule must be portable: roles and vocabulary classes only, never a repository, a context, a path or a symbol.\n")
	builder.WriteString("- The rule must be non-vacuous: it must deny a model that breaks the invariant, not only one that already carries the words of the sentence.\n")
	return builder.String()
}

func kindOfSlug(slug string) string {
	parts := strings.FieldsFunc(slug, func(char rune) bool {
		return char == '-' || char == '_' || char == '.' || char == '/'
	})
	builder := strings.Builder{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	if builder.Len() == 0 {
		return "Policy"
	}
	return builder.String()
}

func packageOfSlug(slug string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "", "/", "").Replace(slug))
}
