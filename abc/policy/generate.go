package policy

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const (
	GenerateModePolicy = "policy"

	GenerateModeBind = "bind"
)

type GenerateContext struct {
	Name string `json:"name"`

	Labels map[string]string `json:"labels,omitempty"`

	Requirements []string `json:"requirements,omitempty"`
}

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

type Check struct {
	Name string `json:"name"`

	Passed bool `json:"passed"`

	Message string `json:"message,omitempty"`
}

type Generator interface {
	Generate(ctx context.Context, request GenerateRequest) (GenerateResult, error)
}

func ForbiddenIdentifiers(binding Binding, classifiers []string, repository string, contexts []string) []string {
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
		targets := binding.Roles[name].Targets
		if targets == nil {
			continue
		}
		for _, attr := range targets.Attrs {
			if attr != "" {
				out = append(out, attr)
			}
		}
	}
	for _, name := range binding.RoleNames() {
		for _, glob := range binding.Roles[name].Globs {
			out = append(out, globLiterals(glob)...)
		}
	}
	for _, classifier := range classifiers {
		out = append(out, classifierLiterals(classifier)...)
	}
	sort.Strings(out)
	return dedupeStrings(out)
}

func classifierLiterals(classifier string) []string {
	name := strings.TrimSpace(classifier)
	if name == "" {
		return nil
	}
	out := []string{name}
	if stem := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)); stem != name && stem != "" {
		out = append(out, stem)
	}
	return out
}

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

type MutationVocabulary struct {
	HostRole string

	GuestRole string

	TestRole string

	Channel string

	Payload string

	Purpose string

	Event string

	Route string

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

type ModelMutation struct {
	Name string

	Reason string

	Model ArchitectureModel
}

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
			From:      vocabulary.HostRole,
			To:        vocabulary.GuestRole,
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
			if flow.Initiator == vocabulary.GuestRole &&
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
			From:      vocabulary.TestRole,
			To:        vocabulary.GuestRole,
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

type BindingReport struct {
	Missing []string

	EmptyRoles []string

	UnmatchedVocabulary []string

	MatchedVocabulary int

	Selectors map[string][]string
}

func (r BindingReport) OK() bool {
	return len(r.Missing) == 0 && len(r.EmptyRoles) == 0 && r.MatchedVocabulary > 0
}

func (r BindingReport) Messages() []string {
	out := []string{}
	if len(r.Missing) > 0 {
		out = append(out, "the binding does not declare "+strings.Join(r.Missing, ", "))
	}
	if len(r.EmptyRoles) > 0 {
		out = append(out, "these roles select no component: "+strings.Join(r.EmptyRoles, ", "))
	}
	if r.MatchedVocabulary == 0 {
		out = append(out, "no vocabulary class matches an effect or a spec term: the binding maps onto nothing")
	}
	return out
}

func (r BindingReport) Notes() []string {
	if len(r.UnmatchedVocabulary) == 0 {
		return nil
	}
	return []string{"no effect and no spec term speaks these vocabulary classes yet: " + strings.Join(r.UnmatchedVocabulary, ", ")}
}

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
		if vocabularyMatches(class, model, terms) {
			report.MatchedVocabulary++
			continue
		}
		report.UnmatchedVocabulary = append(report.UnmatchedVocabulary, class.group+"/"+class.name)
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

func GuardedByContext(library Library) map[string][]string {
	out := map[string][]string{}
	for _, template := range library.Templates {
		for _, requirement := range template.Requirements {
			parsed, err := ParseRequirement(requirement)
			if err != nil {
				continue
			}
			out[parsed.Context] = append(out[parsed.Context], parsed.ID)
		}
	}
	for context := range out {
		slices.Sort(out[context])
		out[context] = dedupeStrings(out[context])
	}
	return out
}

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
