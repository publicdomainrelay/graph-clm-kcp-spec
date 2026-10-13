package specd

import (
	"context"
	"sort"
	"strings"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/plan"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/kcpclient"
)

// HeldRemovalPrefix opens the message of a change the removal guard holds. The
// change stays Pending, so a later declaration lets it run.
const HeldRemovalPrefix = "held: undeclared removal of "

// holdUndeclaredRemoval is the central removal guard: a spec to code change
// that removes a requirement, an interface or an interaction whose unit the
// SystemContext does not declare does not run. Declared means the unit is named
// in the declared annotation, which is where a plan edit's removeRequirements,
// removeInterfaces and removeInteractions land and where specctl arch release
// writes the unit an operator released. The guard reads the delta, so it holds
// a removal whatever field manager wrote the spec, a server side apply
// included. A held change keeps its text and its proposals; only the phase and
// the message move, so the change is never Failed.
func (c *Controller) holdUndeclaredRemoval(ctx context.Context, namespace string, change *spec.SpecChange) (bool, error) {
	if change.Spec.Direction != specapi.DirectionSpecToCode {
		return false, nil
	}
	changeDelta, err := c.deltaForChange(ctx, namespace, change)
	if err != nil {
		return false, err
	}
	removed := removedUnits(changeDelta)
	if len(removed) == 0 {
		return false, nil
	}
	systemContext, err := c.readContext(ctx, namespace, change.Spec.SystemContext)
	if err != nil {
		if kcpclient.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	declared, err := authority.Parse(systemContext.Annotations[specapi.DeclaredAnnotation])
	if err != nil {
		c.log.Warn("the declared annotation of a context is not readable; the removal guard reads it as no declaration",
			"systemcontext", systemContext.Name, "err", err)
		declared = authority.Set{}
	}
	declared = declared.Union(c.planItemUnits(ctx, namespace, systemContext))
	held := []string{}
	for _, unit := range removed {
		if !declared[unit] {
			held = append(held, unit)
		}
	}
	if len(held) == 0 {
		return false, nil
	}
	sort.Strings(held)
	proposals := authority.Proposals(removalSpec(systemContext.Spec, changeDelta), systemContext.Spec, authority.NewSet(held...))
	if heldRemoval(change) && change.Status.Message == HeldRemovalPrefix+held[0] && len(change.Status.Proposals) == len(proposals) {
		return true, nil
	}
	status := map[string]any{
		"phase":     specapi.PhasePending,
		"message":   HeldRemovalPrefix + held[0],
		"proposals": authority.MergeProposals(change.Status.Proposals, proposals),
	}
	if _, err := c.client.PatchStatus(ctx, specapi.SpecChangeGVR, namespace, change.Name, status); err != nil {
		return false, err
	}
	c.log.Warn("a spec to code change is held: it removes a unit the spec does not declare",
		"change", change.Name, "systemcontext", change.Spec.SystemContext, "unit", held[0], "units", held)
	return true, nil
}

// planItemUnits names the units the plan item that staged this context's spec
// declares, so a removal the item names under removeRequirements,
// removeInterfaces or removeInteractions is a declared removal.
func (c *Controller) planItemUnits(ctx context.Context, namespace string, systemContext *spec.SystemContext) authority.Set {
	planName, item, found := strings.Cut(systemContext.Annotations[specapi.PlanItemAnnotation], "/")
	if !found || planName == "" || item == "" {
		return nil
	}
	object, err := c.client.Get(ctx, specapi.PlanGVR, namespace, planName)
	if err != nil {
		return nil
	}
	typed, err := kcpclient.Typed(object)
	if err != nil {
		return nil
	}
	staged, ok := typed.(*spec.Plan)
	if !ok {
		return nil
	}
	units := authority.Set{}
	for _, planItem := range staged.Spec.Items {
		if planItem.ID != item {
			continue
		}
		for _, edit := range planItem.Edits {
			units = units.Union(plan.EditUnits(edit))
		}
	}
	return units
}

// heldRemoval reports whether the status of a change says the guard holds it.
func heldRemoval(change *spec.SpecChange) bool {
	return strings.HasPrefix(change.Status.Message, HeldRemovalPrefix)
}

// removedUnits names the units a delta removes.
func removedUnits(changeDelta spec.Delta) []string {
	out := []string{}
	for _, entry := range changeDelta.Requirements {
		if entry.Op == spec.OpRemoved {
			out = append(out, authority.RequirementUnit(entry.ID))
		}
	}
	for _, entry := range changeDelta.Interfaces {
		if entry.Op == spec.OpRemoved {
			out = append(out, authority.InterfaceUnit(entry.Name))
		}
	}
	for _, entry := range changeDelta.Interactions {
		if entry.Op == spec.OpRemoved {
			out = append(out, authority.InteractionUnit(entry.ID))
		}
	}
	return out
}

// removalSpec is the spec a delta removes from: the drafted spec with every
// removed unit put back at the value the delta carried, so the proposal of a
// held removal shows the value the spec held.
func removalSpec(after spec.SystemContextSpec, changeDelta spec.Delta) spec.SystemContextSpec {
	out := after
	for _, entry := range changeDelta.Requirements {
		if entry.Op == spec.OpRemoved && entry.From != nil {
			out.Requirements = append(out.Requirements, *entry.From)
		}
	}
	for _, entry := range changeDelta.Interfaces {
		if entry.Op == spec.OpRemoved && entry.From != nil {
			out.Interfaces = append(out.Interfaces, *entry.From)
		}
	}
	for _, entry := range changeDelta.Interactions {
		if entry.Op == spec.OpRemoved && entry.From != nil {
			out.Interactions = append(out.Interactions, *entry.From)
		}
	}
	return out
}
