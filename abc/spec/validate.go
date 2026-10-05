package spec

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

type Problem struct {
	Path    string
	Message string
}

func (p Problem) Error() string {
	return p.Path + ": " + p.Message
}

type Result struct {
	Problems []Problem

	SpecHash string
}

func (r Result) OK() bool {
	return len(r.Problems) == 0
}

func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	parts := make([]string, 0, len(r.Problems))
	for _, problem := range r.Problems {
		parts = append(parts, problem.Error())
	}
	return errors.New(strings.Join(parts, "; "))
}

type builder struct {
	problems []Problem
}

func (b *builder) add(path, format string, args ...any) {
	b.problems = append(b.problems, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (b *builder) result() Result {
	return Result{Problems: b.problems}
}

func ValidateRepository(repository *Repository) Result {
	b := &builder{}
	if repository == nil {
		b.add("", "repository is nil")
		return b.result()
	}
	b.checkName("metadata.name", repository.Name)

	source := repository.Source()
	switch {
	case source.Path != "" && source.Git != nil:
		b.add("spec.source", "is a path or a git url, not both")
	case source.Git != nil && source.Git.URL == "":
		b.add("spec.source.git.url", "is required")
	}
	if agent := repository.Spec.Agent; agent != nil {
		b.checkAgent("spec.agent", agent)
	}
	b.checkAcceptance(repository.Spec.Acceptance)
	b.checkAcceptanceOverrides(repository.Spec.AcceptanceOverrides, repository.Spec.Acceptance)
	if populate := repository.Spec.Populate; populate != nil {
		switch populate.Partition {
		case "", PartitionDirectory, PartitionPackage:
		default:
			b.add("spec.populate.partition", "%q is not %s or %s", populate.Partition, PartitionDirectory, PartitionPackage)
		}
		if populate.Agent != nil {
			b.checkAgent("spec.populate.agent", populate.Agent)
		}
	}

	return b.result()
}

func (b *builder) checkAcceptance(steps []AcceptanceStep) {
	seen := map[string]bool{}
	for index, step := range steps {
		path := fmt.Sprintf("spec.acceptance[%d]", index)
		switch {
		case step.Name == "":
			b.add(path+".name", "is required")
		case strings.ContainsAny(step.Name, "\n\r"):
			b.add(path+".name", "must not carry a line break")
		case seen[step.Name]:
			b.add(path+".name", "%q is not unique", step.Name)
		default:
			seen[step.Name] = true
		}
		if len(step.Command) == 0 {
			b.add(path+".command", "is required")
		}
		if step.TimeoutSeconds < 0 {
			b.add(path+".timeoutSeconds", "%d is negative", step.TimeoutSeconds)
		}
	}
}

func (b *builder) checkAcceptanceOverrides(overrides []AcceptanceOverride, steps []AcceptanceStep) {
	known := map[string]bool{}
	for _, step := range steps {
		known[step.Name] = true
	}
	seen := map[string]bool{}
	for index, override := range overrides {
		path := fmt.Sprintf("spec.acceptanceOverrides[%d]", index)
		if seen[override.Step] {
			b.add(path+".step", "%q is not unique", override.Step)
		}
		seen[override.Step] = true
		switch {
		case override.Step == "":
			b.add(path+".step", "is required")
		case strings.HasPrefix(override.Step, PolicyOverridePrefix):
			// A policy override waives one constraint for one change; the
			// constraint need not be an acceptance step.
			if override.Step == PolicyOverridePrefix {
				b.add(path+".step", "policy: needs a constraint name")
			}
		case !known[override.Step]:
			b.add(path+".step", "%q is not an acceptance step", override.Step)
		}
		if override.Reason == "" {
			b.add(path+".reason", "is required")
		}
	}
}

func (b *builder) checkAgent(path string, agent *AgentSpec) {
	switch {
	case agent.Kind == "":
	case agent.Kind == "claude", agent.Kind == "claude-mod", agent.Kind == "pi":
	case strings.HasPrefix(agent.Kind, "scripted:"):
		if strings.TrimPrefix(agent.Kind, "scripted:") == "" {
			b.add(path+".kind", "scripted: needs a scenario file")
		}
	default:
		b.add(path+".kind", "%q is not claude, claude-mod, pi or scripted:<file>", agent.Kind)
	}
}

func ValidateSystemContext(context *SystemContext) Result {
	b := &builder{}
	if context == nil {
		b.add("", "system context is nil")
		return b.result()
	}
	b.checkName("metadata.name", context.Name)

	if context.Spec.Repository == "" {
		b.add("spec.repository", "is required")
	}
	if !IsRef(context.Spec.Upstream) {
		b.add("spec.upstream", "%q is not self, sc.<name> or up.<name>", context.Spec.Upstream)
	}
	for index, overlay := range context.Spec.Overlay {
		if !IsRef(overlay) || !hasAnyPrefix(overlay, RefPrefixContext, RefPrefixOverlay) {
			b.add(fmt.Sprintf("spec.overlay[%d]", index), "%q is not sc.<name> or ov.<name>", overlay)
		}
	}
	if context.Spec.Orchestrator != "" && !IsRef(context.Spec.Orchestrator) {
		b.add("spec.orchestrator", "%q is not self, sc.<name>, up.<name>, ov.<name> or orch.<name>", context.Spec.Orchestrator)
	}
	b.checkRefs("spec.dependsOn", context.Spec.DependsOn)
	b.checkRefs("spec.introduces", context.Spec.Introduces)

	b.checkRequirements(context.Spec.Requirements)
	b.checkInterfaces(context.Spec.Interfaces)
	b.checkInteractions(context.Spec.Interactions)
	b.checkCodeRefs("spec.codeRefs", context.Spec.CodeRefs)
	b.checkArch(context.Spec.Arch)

	if context.Status.RealizedSpecHash != "" && !specapi.IsHash(context.Status.RealizedSpecHash) {
		b.add("status.realizedSpecHash", "%q is not a sha256 hex digest", context.Status.RealizedSpecHash)
	}
	if context.Status.RealizedSpec != nil {
		candidate := &SystemContext{
			ObjectMeta: metav1.ObjectMeta{Name: context.Name},
			Spec:       *context.Status.RealizedSpec,
		}
		if result := ValidateSystemContext(candidate); !result.OK() {
			b.add("status.realizedSpec", "%v", result.Err())
		}
	}

	result := b.result()
	if result.OK() {
		hash, err := specapi.HashJSON(context.Spec)
		if err != nil {
			b.add("spec", "cannot hash: %v", err)
			return b.result()
		}
		result.SpecHash = hash
	}
	return result
}

func ValidateSpecChange(change *SpecChange) Result {
	b := &builder{}
	if change == nil {
		b.add("", "spec change is nil")
		return b.result()
	}
	b.checkName("metadata.name", change.Name)

	if change.Spec.SystemContext == "" {
		b.add("spec.systemContext", "is required")
	}
	switch change.Spec.Direction {
	case specapi.DirectionSpecToCode:
		if change.Spec.ToSpecHash == "" {
			b.add("spec.toSpecHash", "is required for %s", specapi.DirectionSpecToCode)
		} else if !specapi.IsHash(change.Spec.ToSpecHash) {
			b.add("spec.toSpecHash", "%q is not a sha256 hex digest", change.Spec.ToSpecHash)
		}
		if change.Spec.FromSpecHash != "" && !specapi.IsHash(change.Spec.FromSpecHash) {
			b.add("spec.fromSpecHash", "%q is not a sha256 hex digest", change.Spec.FromSpecHash)
		}
	case specapi.DirectionCodeToSpec:
		if (change.Spec.ToCommit == "") != (change.Spec.FromCommit == "") {
			b.add("spec.fromCommit", "and spec.toCommit are both empty or both set")
		}
	default:
		b.add("spec.direction", "%q is not %s or %s", change.Spec.Direction, specapi.DirectionSpecToCode, specapi.DirectionCodeToSpec)
	}

	if change.Status.Phase != "" && !slices.Contains([]string{
		specapi.PhasePending, specapi.PhaseRunning, specapi.PhaseSucceeded, specapi.PhaseFailed,
	}, change.Status.Phase) {
		b.add("status.phase", "%q is not Pending, Running, Succeeded or Failed", change.Status.Phase)
	}

	if change.Spec.Delta != nil {
		b.checkDelta("spec.delta", change.Spec.Delta)
	}

	return b.result()
}

func (b *builder) checkDelta(path string, delta *Delta) {
	for index, requirement := range delta.Requirements {
		at := fmt.Sprintf("%s.requirements[%d]", path, index)
		if requirement.ID == "" {
			b.add(at+".id", "is required")
		}
		b.checkOp(at+".op", requirement.Op)
	}
	for index, declared := range delta.Interfaces {
		at := fmt.Sprintf("%s.interfaces[%d]", path, index)
		if declared.Name == "" {
			b.add(at+".name", "is required")
		}
		b.checkOp(at+".op", declared.Op)
	}
	if delta.Observed != nil {
		for index, observedInterface := range delta.Observed.Interfaces {
			at := fmt.Sprintf("%s.observed.interfaces[%d]", path, index)
			if observedInterface.Name == "" {
				b.add(at+".name", "is required")
			}
			b.checkOp(at+".op", observedInterface.Op)
		}
	}
}

func (b *builder) checkOp(path, op string) {
	if !slices.Contains([]string{OpAdded, OpRemoved, OpChanged}, op) {
		b.add(path, "%q is not %s, %s or %s", op, OpAdded, OpRemoved, OpChanged)
	}
}

func ValidateAny(object any) Result {
	switch typed := object.(type) {
	case *Repository:
		return ValidateRepository(typed)
	case *SystemContext:
		return ValidateSystemContext(typed)
	case *SpecChange:
		return ValidateSpecChange(typed)
	}
	b := &builder{}
	b.add("", "kind %T is not Repository, SystemContext or SpecChange", object)
	return b.result()
}

func (b *builder) checkName(path, name string) {
	if name == "" {
		b.add(path, "is required")
		return
	}
	if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
		b.add(path, "%q is not a DNS-1123 subdomain: %s", name, strings.Join(problems, ", "))
	}
}

func (b *builder) checkRequirements(requirements []Requirement) {
	seen := map[string]bool{}
	for index, requirement := range requirements {
		path := fmt.Sprintf("spec.requirements[%d]", index)
		if requirement.ID == "" {
			b.add(path+".id", "is required")
		} else if seen[requirement.ID] {
			b.add(path+".id", "%q is not unique", requirement.ID)
		} else {
			seen[requirement.ID] = true
		}
		if !slices.Contains(Levels(), requirement.Level) {
			b.add(path+".level", "%q is not MUST, SHOULD or MAY", requirement.Level)
		}
		if requirement.Text == "" {
			b.add(path+".text", "is required")
		}
		b.checkCodeRefs(path+".codeRefs", requirement.CodeRefs)
	}
}

func (b *builder) checkInterfaces(interfaces []Interface) {
	seen := map[string]bool{}
	for index, declared := range interfaces {
		path := fmt.Sprintf("spec.interfaces[%d]", index)
		if declared.Name == "" {
			b.add(path+".name", "is required")
		} else if seen[declared.Name] {
			b.add(path+".name", "%q is not unique", declared.Name)
		} else {
			seen[declared.Name] = true
		}
	}
}

func (b *builder) checkInteractions(interactions []Interaction) {
	seen := map[string]bool{}
	for index, interaction := range interactions {
		path := fmt.Sprintf("spec.interactions[%d]", index)
		if interaction.ID == "" {
			b.add(path+".id", "is required")
		} else if seen[interaction.ID] {
			b.add(path+".id", "%q is not unique", interaction.ID)
		} else {
			seen[interaction.ID] = true
		}
		if interaction.Peer == "" {
			b.add(path+".peer", "is required")
		}
		if !slices.Contains(Initiators(), interaction.Initiator) {
			b.add(path+".initiator", "%q is not self or peer", interaction.Initiator)
		}
		if interaction.Level != "" && !slices.Contains(Levels(), interaction.Level) {
			b.add(path+".level", "%q is not MUST, SHOULD or MAY", interaction.Level)
		}
		for refIndex, carried := range interaction.Carries {
			if strings.TrimSpace(carried) == "" {
				b.add(fmt.Sprintf("%s.carries[%d]", path, refIndex), "is empty")
			}
		}
	}
}

func (b *builder) checkRefs(path string, refs []string) {
	seen := map[string]bool{}
	for index, ref := range refs {
		at := fmt.Sprintf("%s[%d]", path, index)
		if !IsRef(ref) || ref == RefSelf {
			b.add(at, "%q is not sc.<name>, up.<name>, ov.<name> or orch.<name>", ref)
			continue
		}
		if seen[ref] {
			b.add(at, "%q is not unique", ref)
			continue
		}
		seen[ref] = true
	}
}

func (b *builder) checkArch(arch *ArchSpec) {
	if arch == nil {
		return
	}
	if arch.ID == "" {
		b.add("spec.arch.id", "is required")
	}
	switch arch.Kind {
	case ArchKindNode, ArchKindDocument:
	default:
		b.add("spec.arch.kind", "%q is not %s or %s", arch.Kind, ArchKindNode, ArchKindDocument)
	}
	if arch.Kind == ArchKindDocument {
		if arch.Document == nil {
			b.add("spec.arch.document", "is required on the document object")
		}
		return
	}
	if arch.Node == nil {
		b.add("spec.arch.node", "is required on a node object")
	}
	if arch.Upstream != "" {
		if !IsRef(arch.Upstream) || arch.Upstream == RefSelf {
			b.add("spec.arch.upstream", "%q is not a ref", arch.Upstream)
		}
	}
	for index, overlay := range arch.Overlay {
		if !IsRef(overlay) || !hasAnyPrefix(overlay, RefPrefixContext, RefPrefixOverlay) {
			b.add(fmt.Sprintf("spec.arch.overlay[%d]", index), "%q is not sc.<name> or ov.<name>", overlay)
		}
	}
	if arch.Orchestrator != "" && !IsRef(arch.Orchestrator) {
		b.add("spec.arch.orchestrator", "%q is not a ref", arch.Orchestrator)
	}
	b.checkRefs("spec.arch.dependsOn", arch.DependsOn)
	b.checkRefs("spec.arch.introduces", arch.Introduces)
	b.checkCodeRefs("spec.arch.code", arch.Code)
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func (b *builder) checkCodeRefs(path string, refs []string) {
	seen := map[string]bool{}
	for index, ref := range refs {
		at := fmt.Sprintf("%s[%d]", path, index)
		if !IsCodeRef(ref) {
			b.add(at, "%q is not file:, function:, method:, type: or package: followed by a value", ref)
			continue
		}
		if seen[ref] {
			b.add(at, "%q is not unique", ref)
			continue
		}
		seen[ref] = true
	}
}
