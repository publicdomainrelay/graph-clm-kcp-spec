package policy

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

const (
	GenerateModePolicy = "policy"

	GenerateModeBind = "bind"
)

// GenerateContext is one SystemContext the generator is told about: its name,
// its labels and the requirements a generated policy can carry.
type GenerateContext struct {
	Name string `json:"name"`

	Labels map[string]string `json:"labels,omitempty"`

	Requirements []string `json:"requirements,omitempty"`
}

// GenerateRequest is what a policy harness is asked for. Policy mode authors a
// template over the ArchitectureModel and the vocabulary; bind mode authors the
// roles and vocabulary of one repository for a pack. Either way the harness
// writes a policy tree under Dir and specd validates it.
type GenerateRequest struct {
	Mode string

	Repository string

	Branch string

	Dir string

	Slug string

	Prompt string

	Requirements []string

	Contexts []GenerateContext

	Enforcement Enforcement

	Pack *PackManifest

	Binding Binding

	Model string

	Existing []string

	Attempt int

	Failures []string
}

type GenerateResult struct {
	Summary string

	Log string

	Files []string
}

// Generator is a policy harness: the same agent kinds a realize uses, asked to
// author a policy or a binding instead of code.
type Generator interface {
	Generate(ctx context.Context, request GenerateRequest) (GenerateResult, error)
}

// ForbiddenIdentifiers names the strings a portable source must not mention:
// the repository, its context names and the literal segments of the role globs.
// Vocabulary terms are not forbidden -- they are the language a portable rule
// is written in -- so a role's symbols and target hints are left out.
func ForbiddenIdentifiers(binding Binding, repository string, contexts []string) []string {
	out := []string{}
	if repository != "" {
		out = append(out, repository)
	}
	for _, context := range contexts {
		if context != "" && context != repository {
			out = append(out, context)
		}
	}
	for _, name := range binding.RoleNames() {
		for _, glob := range binding.Roles[name].Globs {
			out = append(out, globLiterals(glob)...)
		}
	}
	sort.Strings(out)
	return dedupeStrings(out)
}

// globLiterals names the directory and file names of a glob, without its
// wildcards: `lib/common/cloud-init-common/**` yields `lib`, `common` and
// `cloud-init-common`. Single-character and generic segments are dropped, so
// `lib` does not forbid a rule that writes `lib.specd`.
func globLiterals(glob string) []string {
	out := []string{}
	for _, segment := range strings.FieldsFunc(glob, func(char rune) bool {
		return char == '/' || char == '*' || char == '?'
	}) {
		if len(segment) < 3 || segment == "lib" || segment == "test" || segment == "src" || segment == "common" {
			continue
		}
		out = append(out, segment)
	}
	return out
}

// PortabilityFindings reports which forbidden identifiers a generated source
// mentions. A finding is the identifier, once.
func PortabilityFindings(source string, forbidden []string) []string {
	found := map[string]bool{}
	out := []string{}
	lowered := strings.ToLower(source)
	for _, identifier := range forbidden {
		if identifier == "" || len(identifier) < 3 {
			continue
		}
		if !strings.Contains(lowered, strings.ToLower(identifier)) {
			continue
		}
		if found[identifier] {
			continue
		}
		found[identifier] = true
		out = append(out, identifier)
	}
	sort.Strings(out)
	return out
}

// MutationVocabulary names the roles and the vocabulary classes a derived
// mutation speaks. A class left empty makes the mutations that need it
// inapplicable.
type MutationVocabulary struct {
	HostRole string

	GuestRole string

	TestRole string

	Channel string

	Payload string

	Purpose string

	Event string

	Route string

	// EventTerms are the terms of the event class, so a derived mutation drives
	// the emit that really carries the report.
	EventTerms []string
}

var mutationRoleCandidates = map[string][]string{
	"host":  {"host"},
	"guest": {"guest"},
	"test":  {"test"},
}

func pickRole(names []string, wanted string) string {
	for _, candidate := range mutationRoleCandidates[wanted] {
		for _, name := range names {
			if name == candidate {
				return name
			}
		}
	}
	return ""
}

func firstClassName(classes map[string][]string, wanted string) string {
	if names := classNames(classes); len(names) > 0 {
		for _, name := range names {
			if name == wanted {
				return name
			}
		}
		return names[0]
	}
	return ""
}

func classNames(classes map[string][]string) []string {
	out := make([]string, 0, len(classes))
	for name, terms := range classes {
		if name == "" || len(terms) == 0 {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// MutationVocabularyOf reads the roles a pack requires and the binding's
// vocabulary into the names derived mutations use.
func MutationVocabularyOf(binding Binding, roles []string) MutationVocabulary {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, role)
	}
	vocabulary := binding.Vocabulary
	return MutationVocabulary{
		HostRole:   pickRole(names, "host"),
		GuestRole:  pickRole(names, "guest"),
		TestRole:   pickRole(names, "test"),
		Channel:    firstClassName(vocabulary.Channels, "relay"),
		Payload:    firstClassName(vocabulary.Payloads, "network-info"),
		Purpose:    firstClassName(vocabulary.Purposes, "network-discovery"),
		Event:      firstClassName(vocabulary.Events, "network-report"),
		Route:      firstClassName(vocabulary.Routes, "report"),
		EventTerms: vocabulary.Events[firstClassName(vocabulary.Events, "network-report")],
	}
}

// ModelMutation is the input model with one invariant-breaking change, so a
// rule that denies none of them is vacuous.
type ModelMutation struct {
	Name string

	Reason string

	Model ArchitectureModel
}

// Mutations derives the cases a model-level rule must deny: the host reaching
// into the guest, the guest's report dropped, the host's emission driven by the
// provisioning lifecycle, an unrelayed ssh outside the guest and a test dialing
// the guest. A mutation whose roles or components the model does not carry is
// not derived at all.
func Mutations(model ArchitectureModel, vocabulary MutationVocabulary) []ModelMutation {
	out := []ModelMutation{}
	host := firstComponentInRole(model, vocabulary.HostRole)
	guest := firstComponentInRole(model, vocabulary.GuestRole)
	test := firstComponentInRole(model, vocabulary.TestRole)

	if host != "" && guest != "" {
		mutated := cloneModel(model)
		effect := mutationEffect(EffectContainerExec, host, "mutation-host-reach-in", map[string]string{"verb": "getNodeId"})
		mutated.Spec.Effects = append(mutated.Spec.Effects, effect)
		mutated.Spec.Flows = append(mutated.Spec.Flows, ModelFlow{
			From:      host,
			To:        guest,
			Initiator: vocabulary.HostRole,
			Channel:   vocabulary.Channel,
			Carries:   oneIfSet(vocabulary.Payload),
			Purpose:   vocabulary.Purpose,
			Source:    SourceObserved,
			Evidence:  []string{effect.ID},
		})
		out = append(out, ModelMutation{
			Name:   "host-reaches-in",
			Reason: "a host-initiated flow to the guest carrying network information",
			Model:  mutated,
		})
	}

	if vocabulary.GuestRole != "" {
		mutated := cloneModel(model)
		kept := []ModelFlow{}
		dropped := 0
		for _, flow := range mutated.Spec.Flows {
			if roleOfComponent(model, flow.Initiator) == vocabulary.GuestRole &&
				(vocabulary.Payload == "" || containsString(flow.Carries, vocabulary.Payload)) {
				dropped++
				continue
			}
			kept = append(kept, flow)
		}
		if dropped > 0 {
			mutated.Spec.Flows = kept
			out = append(out, ModelMutation{
				Name:   "guest-report-dropped",
				Reason: "the guest no longer reports its network information outbound",
				Model:  mutated,
			})
		}
	}

	if host != "" {
		mutated := cloneModel(model)
		emit := ""
		for _, effect := range mutated.Spec.Effects {
			if effect.Component != host || effect.Kind != EffectEventEmit {
				continue
			}
			if typed := effect.Attr("type"); typed != "" && len(vocabulary.EventTerms) > 0 && !mentionsAny(typed, vocabulary.EventTerms) {
				continue
			}
			emit = effect.ID
			break
		}
		if emit != "" {
			lifecycle := mutationEffect(EffectProcExec, host, "mutation-lifecycle", nil)
			mutated.Spec.Effects = append(mutated.Spec.Effects, lifecycle)
			triggers := []ModelTrigger{}
			for _, trigger := range mutated.Spec.Triggers {
				if trigger.To == emit {
					continue
				}
				triggers = append(triggers, trigger)
			}
			mutated.Spec.Triggers = append(triggers, ModelTrigger{From: lifecycle.ID, To: emit})
			out = append(out, ModelMutation{
				Name:   "emission-from-the-lifecycle",
				Reason: "the host's network report is triggered by the provisioning lifecycle, not by the report handler",
				Model:  mutated,
			})
		}
	}

	if test != "" {
		mutated := cloneModel(model)
		effect := mutationEffect(EffectSSHConnect, test, "mutation-unrelayed-ssh", map[string]string{"target": "guest"})
		mutated.Spec.Effects = append(mutated.Spec.Effects, effect)
		out = append(out, ModelMutation{
			Name:   "unrelayed-ssh",
			Reason: "an ssh outside the guest carried by no relay-channel flow",
			Model:  mutated,
		})
	}

	if test != "" && guest != "" {
		mutated := cloneModel(model)
		effect := mutationEffect(EffectNetDial, test, "mutation-test-dials-guest", map[string]string{"target": guest})
		mutated.Spec.Effects = append(mutated.Spec.Effects, effect)
		mutated.Spec.Flows = append(mutated.Spec.Flows, ModelFlow{
			From:      test,
			To:        guest,
			Initiator: vocabulary.TestRole,
			Source:    SourceObserved,
			Evidence:  []string{effect.ID},
		})
		out = append(out, ModelMutation{
			Name:   "test-dials-the-guest",
			Reason: "a test that dials the guest directly instead of the relay",
			Model:  mutated,
		})
	}

	return out
}

// modelClassNames names the vocabulary classes the model itself resolved: the
// channel, the carried payloads and the purpose of every flow.
func modelClassNames(model ArchitectureModel) []string {
	out := []string{}
	for _, flow := range model.Spec.Flows {
		if flow.Channel != "" {
			out = append(out, flow.Channel)
		}
		if flow.Purpose != "" {
			out = append(out, flow.Purpose)
		}
		out = append(out, flow.Carries...)
	}
	return out
}

func mentionsAny(text string, terms []string) bool {
	lowered := strings.ToLower(text)
	for _, term := range terms {
		if term != "" && strings.Contains(lowered, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func mutationEffect(kind EffectKind, component, node string, attrs map[string]string) Effect {
	return Effect{
		ID:        ViolationID("mutation", string(kind), component, node),
		Kind:      kind,
		Component: component,
		Attrs:     attrs,
		File:      "mutation",
		Line:      1,
		Node:      node,
	}
}

func oneIfSet(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func firstComponentInRole(model ArchitectureModel, role string) string {
	if role == "" {
		return ""
	}
	for _, component := range model.Spec.Components {
		if containsString(component.Roles, role) {
			return component.Name
		}
	}
	return ""
}

// roleOfComponent resolves a component name, or a role name a flow's initiator
// carries, to the role the model knows.
func roleOfComponent(model ArchitectureModel, name string) string {
	for _, component := range model.Spec.Components {
		if component.Name != name {
			continue
		}
		if len(component.Roles) > 0 {
			return component.Roles[0]
		}
		return ""
	}
	for _, role := range model.Spec.Roles {
		if role == name {
			return role
		}
	}
	return ""
}

func cloneModel(model ArchitectureModel) ArchitectureModel {
	out := model
	out.Spec.Components = append([]ModelComponent{}, model.Spec.Components...)
	out.Spec.Roles = append([]string{}, model.Spec.Roles...)
	out.Spec.Vocabulary = cloneVocabulary(model.Spec.Vocabulary)
	out.Spec.Effects = make([]Effect, len(model.Spec.Effects))
	for index, effect := range model.Spec.Effects {
		copied := effect
		if effect.Attrs != nil {
			copied.Attrs = map[string]string{}
			maps.Copy(copied.Attrs, effect.Attrs)
		}
		out.Spec.Effects[index] = copied
	}
	out.Spec.Flows = make([]ModelFlow, len(model.Spec.Flows))
	for index, flow := range model.Spec.Flows {
		copied := flow
		copied.Carries = append([]string{}, flow.Carries...)
		copied.Evidence = append([]string{}, flow.Evidence...)
		out.Spec.Flows[index] = copied
	}
	out.Spec.Triggers = append([]ModelTrigger{}, model.Spec.Triggers...)
	return out
}

func cloneVocabulary(vocabulary Vocabulary) Vocabulary {
	out := Vocabulary{}
	copyClasses := func(in map[string][]string) map[string][]string {
		if in == nil {
			return nil
		}
		cloned := map[string][]string{}
		for name, terms := range in {
			cloned[name] = append([]string{}, terms...)
		}
		return cloned
	}
	out.Events = copyClasses(vocabulary.Events)
	out.Channels = copyClasses(vocabulary.Channels)
	out.Payloads = copyClasses(vocabulary.Payloads)
	out.Purposes = copyClasses(vocabulary.Purposes)
	out.Routes = copyClasses(vocabulary.Routes)
	return out
}

// BindingReport names why a proposed binding does not satisfy a pack: the roles
// that select no component, the vocabulary classes that match nothing, and the
// pack requirements it does not declare.
type BindingReport struct {
	Missing []string

	EmptyRoles []string

	UnmatchedVocabulary []string

	Selectors map[string][]string
}

func (r BindingReport) OK() bool {
	return len(r.Missing) == 0 && len(r.EmptyRoles) == 0 && len(r.UnmatchedVocabulary) == 0
}

func (r BindingReport) Messages() []string {
	out := []string{}
	if len(r.Missing) > 0 {
		out = append(out, "the binding does not declare "+strings.Join(r.Missing, ", "))
	}
	if len(r.EmptyRoles) > 0 {
		out = append(out, "these roles select no component: "+strings.Join(r.EmptyRoles, ", "))
	}
	if len(r.UnmatchedVocabulary) > 0 {
		out = append(out, "these vocabulary classes match no effect and no spec term: "+strings.Join(r.UnmatchedVocabulary, ", "))
	}
	return out
}

// CheckBinding validates a binding against the model it produces and the terms
// the specs and the effects carry: every role of the pack selects at least one
// component, and every vocabulary class matches at least one effect or spec
// term. The pack's own required roles and classes are read from the manifest.
func CheckBinding(model ArchitectureModel, binding Binding, pack *PackManifest, terms []string) BindingReport {
	report := BindingReport{Selectors: map[string][]string{}}
	if pack != nil {
		report.Missing = pack.Missing(binding)
	}
	roles := []string{}
	if pack != nil {
		roles = pack.Roles
	} else {
		roles = binding.RoleNames()
	}
	for _, role := range roles {
		components := model.ComponentsWithRole(role)
		if len(components) == 0 {
			report.EmptyRoles = append(report.EmptyRoles, role)
			continue
		}
		names := make([]string, 0, len(components))
		for _, component := range components {
			names = append(names, component.Name)
		}
		sort.Strings(names)
		report.Selectors[role] = names
	}
	for _, class := range vocabularyClasses(binding.Vocabulary) {
		if !vocabularyMatches(class, model, terms) {
			report.UnmatchedVocabulary = append(report.UnmatchedVocabulary, class.group+"/"+class.name)
		}
	}
	sort.Strings(report.EmptyRoles)
	sort.Strings(report.UnmatchedVocabulary)
	return report
}

type vocabularyClass struct {
	group string

	name string

	terms []string
}

func vocabularyClasses(vocabulary Vocabulary) []vocabularyClass {
	groups := []struct {
		group string
		value map[string][]string
	}{
		{"channels", vocabulary.Channels},
		{"events", vocabulary.Events},
		{"payloads", vocabulary.Payloads},
		{"purposes", vocabulary.Purposes},
		{"routes", vocabulary.Routes},
	}
	out := []vocabularyClass{}
	for _, group := range groups {
		for _, name := range classNames(group.value) {
			out = append(out, vocabularyClass{group: group.group, name: name, terms: group.value[name]})
		}
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].group != out[right].group {
			return out[left].group < out[right].group
		}
		return out[left].name < out[right].name
	})
	return out
}

// vocabularyMatches reports whether a class is meaningful for the model: a
// term of it appears in an effect of the model or in a spec term, or the class
// name appears in a flow attribute the model resolved (the modelbuild resolves
// an effect's text into a class name, not into the term).
func vocabularyMatches(class vocabularyClass, model ArchitectureModel, terms []string) bool {
	if class.name != "" && slices.Contains(modelClassNames(model), class.name) {
		return true
	}
	haystack := []string{}
	for _, effect := range model.Spec.Effects {
		haystack = append(haystack, effect.File, effect.Node, effect.Component)
		keys := make([]string, 0, len(effect.Attrs))
		for key := range effect.Attrs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			haystack = append(haystack, effect.Attrs[key])
		}
	}
	haystack = append(haystack, terms...)
	for _, term := range class.terms {
		if term == "" {
			continue
		}
		for _, text := range haystack {
			if strings.Contains(strings.ToLower(text), strings.ToLower(term)) {
				return true
			}
		}
	}
	return false
}

// RequirementRef is one `ctx#id` requirement a policy enforces.
type RequirementRef struct {
	Context string

	ID string
}

func ParseRequirement(ref string) (RequirementRef, error) {
	context, id, found := strings.Cut(ref, "#")
	if !found || context == "" || id == "" {
		return RequirementRef{}, fmt.Errorf("policy: %q is not a ctx#id requirement", ref)
	}
	return RequirementRef{Context: context, ID: id}, nil
}

// RequirementRefs parses a requirement list, keeping the order and refusing a
// malformed entry.
func RequirementRefs(refs []string) ([]RequirementRef, error) {
	out := make([]RequirementRef, 0, len(refs))
	for _, ref := range refs {
		parsed, err := ParseRequirement(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

// TemplatesForRequirement names the templates whose requirements annotation
// enforces a requirement of one context. It is what a SystemContext's
// status.enforcedBy carries.
func TemplatesForRequirement(library Library, context string) []string {
	out := []string{}
	for _, template := range library.Templates {
		for _, requirement := range template.Requirements {
			parsed, err := ParseRequirement(requirement)
			if err != nil || parsed.Context != context {
				continue
			}
			out = append(out, template.Name)
			break
		}
	}
	sort.Strings(out)
	return dedupeStrings(out)
}
