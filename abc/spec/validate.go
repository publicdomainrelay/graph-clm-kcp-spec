package spec

import (
	"errors"
	"fmt"
	"slices"
	"strings"

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

	if repository.Spec.Path == "" {
		b.add("spec.path", "is required")
	}

	return b.result()
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
	b.checkCodeRefs("spec.codeRefs", context.Spec.CodeRefs)
	b.checkArch(context.Spec.Arch)

	if context.Status.RealizedSpecHash != "" && !specapi.IsHash(context.Status.RealizedSpecHash) {
		b.add("status.realizedSpecHash", "%q is not a sha256 hex digest", context.Status.RealizedSpecHash)
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
		if change.Spec.ToCommit == "" {
			b.add("spec.toCommit", "is required for %s", specapi.DirectionCodeToSpec)
		}
		if change.Spec.FromCommit == "" {
			b.add("spec.fromCommit", "is required for %s", specapi.DirectionCodeToSpec)
		}
	default:
		b.add("spec.direction", "%q is not %s or %s", change.Spec.Direction, specapi.DirectionSpecToCode, specapi.DirectionCodeToSpec)
	}

	if change.Status.Phase != "" && !slices.Contains([]string{
		specapi.PhasePending, specapi.PhaseRunning, specapi.PhaseSucceeded, specapi.PhaseFailed,
	}, change.Status.Phase) {
		b.add("status.phase", "%q is not Pending, Running, Succeeded or Failed", change.Status.Phase)
	}

	return b.result()
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

// checkArch gates what the arch.yaml importer writes. The node body is opaque
// by design; the fields the importer derives from it are checked, because the
// graph and the validator read those.
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
