package oabranch

import (
	"sort"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
)

type MergeResult struct {
	Spec spec.SystemContextSpec

	Changed bool

	Conflicts []string

	Delta spec.Delta
}

func Merge(base, ours, theirs spec.SystemContextSpec) MergeResult {
	declaredOurs := declared(ours)
	declaredBase := declared(base)
	declaredTheirs := declared(theirs)
	theirChange := delta.Diff(declaredBase, declaredTheirs)
	if theirChange.Empty() {
		return MergeResult{Spec: ours}
	}
	ourChange := delta.Diff(declaredBase, declaredOurs)
	theirKeys := Keys(theirChange)
	ourKeys := Keys(ourChange)
	conflicts := []string{}
	for key, theirValue := range theirKeys {
		ourValue, touched := ourKeys[key]
		if touched && ourValue != theirValue {
			conflicts = append(conflicts, key)
		}
	}
	sort.Strings(conflicts)
	if len(conflicts) > 0 {
		return MergeResult{Spec: ours, Conflicts: conflicts, Delta: theirChange}
	}
	merged := delta.Apply(ours, theirChange)
	return MergeResult{
		Spec:    merged,
		Changed: !delta.Diff(declaredOurs, declared(merged)).Empty(),
		Delta:   theirChange,
	}
}

func declared(in spec.SystemContextSpec) spec.SystemContextSpec {
	out := spec.Canonicalize(in)
	out.CodeRefs = nil
	return out
}

func Keys(change spec.Delta) map[string]string {
	keys := map[string]string{}
	if change.Intent != nil {
		keys["intent"] = change.Intent.To
	}
	if change.Upstream != nil {
		keys["upstream"] = change.Upstream.To
	}
	if change.Orchestrator != nil {
		keys["orchestrator"] = change.Orchestrator.To
	}
	addSet := func(field string, set *spec.StringSetDelta) {
		if set == nil {
			return
		}
		for _, value := range set.Added {
			keys[field+":"+value] = "added"
		}
		for _, value := range set.Removed {
			keys[field+":"+value] = "removed"
		}
	}
	addSet("overlay", change.Overlay)
	addSet("dependsOn", change.DependsOn)
	addSet("introduces", change.Introduces)
	for _, entry := range change.Requirements {
		keys["requirement:"+entry.ID] = entry.Op + ":" + requirementValue(entry.To)
	}
	for _, entry := range change.Interfaces {
		keys["interface:"+entry.Name] = entry.Op + ":" + interfaceValue(entry.To)
	}
	return keys
}

func requirementValue(requirement *spec.Requirement) string {
	if requirement == nil {
		return ""
	}
	refs := append([]string{}, requirement.CodeRefs...)
	sort.Strings(refs)
	value := string(requirement.Level) + "\x00" + requirement.Text
	for _, ref := range refs {
		value += "\x00" + ref
	}
	return value
}

func interfaceValue(value *spec.Interface) string {
	if value == nil {
		return ""
	}
	return value.Kind + "\x00" + value.Signature + "\x00" + value.File
}
