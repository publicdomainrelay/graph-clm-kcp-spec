// Package plan decides what a Plan does next. It holds no I/O: the controller
// hands it the plan, the contexts and changes kcp holds and the checkout HEAD,
// and carries out the actions it returns.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/authority"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

const (
	PhaseDraft    = "Draft"
	PhaseApproved = "Approved"
	PhaseRunning  = "Running"
	PhaseComplete = "Complete"
	PhaseFailed   = "Failed"

	ItemPending   = "Pending"
	ItemBlocked   = "Blocked"
	ItemRealizing = "Realizing"
	ItemVerifying = "Verifying"
	ItemDone      = "Done"
	ItemFailed    = "Failed"
	ItemElsewhere = "Elsewhere"

	ConditionApproved = "Approved"
	ConditionComplete = "Complete"
)

const (
	ActionApply      = "Apply"
	ActionAcceptance = "Acceptance"
)

// Validate checks what the CRD schema cannot: unique item ids, dependencies
// that name an item, no dependency cycle, and edits that name a context.
func Validate(plan spec.PlanSpec) error {
	seen := map[string]bool{}
	for _, item := range plan.Items {
		if item.ID == "" {
			return fmt.Errorf("plan: an item has no id")
		}
		if seen[item.ID] {
			return fmt.Errorf("plan: item %q appears twice", item.ID)
		}
		seen[item.ID] = true
		for _, edit := range item.Edits {
			if edit.Context == "" {
				return fmt.Errorf("plan: item %q has an edit without a context", item.ID)
			}
		}
		for _, step := range item.Acceptance {
			if step.Name == "" || len(step.Command) == 0 {
				return fmt.Errorf("plan: item %q has an acceptance step without a name or command", item.ID)
			}
		}
	}
	for _, item := range plan.Items {
		for _, dependency := range item.DependsOn {
			if !seen[dependency] {
				return fmt.Errorf("plan: item %q depends on %q, which the plan does not hold", item.ID, dependency)
			}
		}
	}
	if cycle := findCycle(plan.Items); cycle != "" {
		return fmt.Errorf("plan: the items depend on each other in a cycle through %q", cycle)
	}
	return nil
}

func findCycle(items []spec.PlanItem) string {
	edges := map[string][]string{}
	for _, item := range items {
		edges[item.ID] = item.DependsOn
	}
	state := map[string]int{}
	var visit func(string) string
	visit = func(id string) string {
		switch state[id] {
		case 1:
			return id
		case 2:
			return ""
		}
		state[id] = 1
		for _, next := range edges[id] {
			if found := visit(next); found != "" {
				return found
			}
		}
		state[id] = 2
		return ""
	}
	for _, item := range items {
		if found := visit(item.ID); found != "" {
			return found
		}
	}
	return ""
}

// ApplyEdit returns the spec with the edit's upserts and removals applied.
func ApplyEdit(base spec.SystemContextSpec, edit spec.PlanEdit) spec.SystemContextSpec {
	out := base
	if edit.Intent != "" {
		out.Intent = edit.Intent
	}
	out.Requirements = upsert(base.Requirements, edit.Requirements, edit.RemoveRequirements,
		func(entry spec.Requirement) string { return entry.ID })
	out.Interfaces = upsert(base.Interfaces, edit.Interfaces, edit.RemoveInterfaces,
		func(entry spec.Interface) string { return entry.Name })
	out.Interactions = upsert(base.Interactions, edit.Interactions, edit.RemoveInteractions,
		func(entry spec.Interaction) string { return entry.ID })
	return out
}

func upsert[T any](base, set []T, remove []string, keyOf func(T) string) []T {
	if len(set) == 0 && len(remove) == 0 {
		return base
	}
	removed := map[string]bool{}
	for _, key := range remove {
		removed[key] = true
	}
	replacement := map[string]T{}
	for _, entry := range set {
		replacement[keyOf(entry)] = entry
	}
	out := []T{}
	placed := map[string]bool{}
	for _, entry := range base {
		key := keyOf(entry)
		if removed[key] {
			continue
		}
		if next, ok := replacement[key]; ok {
			out = append(out, next)
			placed[key] = true
			continue
		}
		out = append(out, entry)
	}
	for _, entry := range set {
		key := keyOf(entry)
		if !placed[key] && !removed[key] {
			out = append(out, entry)
			placed[key] = true
		}
	}
	return out
}

// EditUnits names the spec units an edit declares.
func EditUnits(edit spec.PlanEdit) authority.Set {
	out := authority.Set{}
	if edit.Intent != "" {
		out[authority.UnitIntent] = true
	}
	for _, entry := range edit.Requirements {
		out[authority.RequirementUnit(entry.ID)] = true
	}
	for _, id := range edit.RemoveRequirements {
		out[authority.RequirementUnit(id)] = true
	}
	for _, entry := range edit.Interfaces {
		out[authority.InterfaceUnit(entry.Name)] = true
	}
	for _, name := range edit.RemoveInterfaces {
		out[authority.InterfaceUnit(name)] = true
	}
	for _, entry := range edit.Interactions {
		out[authority.InteractionUnit(entry.ID)] = true
	}
	for _, id := range edit.RemoveInteractions {
		out[authority.InteractionUnit(id)] = true
	}
	return out
}

// Carries reports whether a spec already says what the edit says. Code refs
// do not count: an edit names intent, the ingest anchors it.
func Carries(contextSpec spec.SystemContextSpec, edit spec.PlanEdit) bool {
	return len(authority.Changed(contextSpec, ApplyEdit(contextSpec, edit)).Intersect(EditUnits(edit))) == 0
}

func Preview(contextSpec spec.SystemContextSpec, edit spec.PlanEdit) string {
	change := delta.Diff(contextSpec, ApplyEdit(contextSpec, edit))
	if change.Empty() {
		return edit.Context + ": already says this"
	}
	return edit.Context + ": " + delta.Summary(change)
}

type Input struct {
	Plan spec.Plan

	// Repository is the repository this instance hosts. An item that names
	// another one is Elsewhere here.
	Repository string

	Contexts map[string]spec.SystemContext

	Changes []spec.SpecChange

	Head string

	MaxAttempts int
}

type Action struct {
	Kind string

	Item string

	Edits []spec.PlanEdit

	Acceptance []spec.AcceptanceStep
}

type Evaluation struct {
	Phase string

	Items []spec.PlanItemStatus

	Actions []Action

	Conditions []metav1.Condition

	// Planned and Resolved name the findings whose state the plan moves:
	// planned once approved, resolved by an item that is done.
	Planned map[string]string

	Resolved map[string]string
}

func Evaluate(input Input) Evaluation {
	plan := input.Plan
	out := Evaluation{Planned: map[string]string{}, Resolved: map[string]string{}}
	phases := map[string]string{}
	busy := map[string]string{}

	for _, item := range plan.Spec.Items {
		previous, _ := plan.ItemStatus(item.ID)
		if !local(item, plan.Spec, input.Repository) {
			continue
		}
		if previous.Phase == ItemRealizing {
			for _, edit := range item.Edits {
				busy[edit.Context] = item.ID
			}
		}
	}

	for _, item := range plan.Spec.Items {
		previous, _ := plan.ItemStatus(item.ID)
		acceptanceHash := AcceptanceHash(item.Acceptance)
		status := spec.PlanItemStatus{
			ID:                item.ID,
			Phase:             previous.Phase,
			AppliedSpecHashes: previous.AppliedSpecHashes,
			Changes:           previous.Changes,
			Commit:            previous.Commit,
			AcceptanceHash:    previous.AcceptanceHash,
			Acceptance:        previous.Acceptance,
			Message:           previous.Message,
		}

		if !local(item, plan.Spec, input.Repository) {
			if status.Phase != ItemDone && status.Phase != ItemFailed {
				status.Phase = ItemElsewhere
				status.Message = "the instance of " + item.Repository + " acts on this item"
			}
			phases[item.ID] = status.Phase
			out.Items = append(out.Items, status)
			continue
		}

		if !plan.Spec.Approved {
			if status.Phase == ItemDone || status.Phase == ItemFailed {
				// A re-applied plan keeps what it already finished: it
				// continues where it stopped once it is approved again.
				status.Preview = previews(item, input.Contexts)
				phases[item.ID] = status.Phase
				out.Items = append(out.Items, status)
				continue
			}
			status = spec.PlanItemStatus{ID: item.ID, Phase: ItemPending, Preview: previews(item, input.Contexts)}
			status.Message = "waits for the plan to be approved"
			phases[item.ID] = status.Phase
			out.Items = append(out.Items, status)
			continue
		}
		for _, finding := range item.Findings {
			out.Planned[finding] = item.ID
		}
		if status.Phase == ItemDone && status.AcceptanceHash != acceptanceHash {
			// The steps changed under a done item: its acceptance runs again.
			status.Phase = ItemVerifying
			status.Message = "the acceptance steps changed"
		}

		switch status.Phase {
		case ItemDone:
			for _, finding := range item.Findings {
				out.Resolved[finding] = status.Commit
			}
			phases[item.ID] = status.Phase
			out.Items = append(out.Items, status)
			continue
		case ItemFailed:
			phases[item.ID] = status.Phase
			out.Items = append(out.Items, status)
			continue
		}

		if waiting := unfinished(item.DependsOn, phases, plan); len(waiting) > 0 {
			status.Phase = ItemBlocked
			status.Message = "waits for " + strings.Join(waiting, ", ")
			phases[item.ID] = status.Phase
			out.Items = append(out.Items, status)
			continue
		}

		if status.Phase == "" || status.Phase == ItemPending || status.Phase == ItemBlocked {
			status, out.Actions = start(item, status, input, busy, out.Actions)
		}
		if status.Phase == ItemRealizing {
			status = realizing(item, status, input)
		}
		if status.Phase == ItemVerifying {
			switch {
			case len(item.Acceptance) == 0:
				status.Phase = ItemDone
				status.Commit = input.Head
				// The recorded hash stays the hash of the steps the item
				// now names, so an item whose steps were dropped is not
				// seen as changed on every later evaluation.
				status.AcceptanceHash = acceptanceHash
				status.Message = "realized; the item names no acceptance"
			case status.Commit != input.Head || status.AcceptanceHash != acceptanceHash:
				out.Actions = append(out.Actions, Action{Kind: ActionAcceptance, Item: item.ID, Acceptance: item.Acceptance})
				status.Message = "acceptance runs at " + short(input.Head)
			}
		}
		if status.Phase == ItemDone {
			for _, finding := range item.Findings {
				out.Resolved[finding] = status.Commit
			}
		}
		phases[item.ID] = status.Phase
		out.Items = append(out.Items, status)
	}

	out.Phase = planPhase(plan.Spec, out.Items)
	out.Conditions = conditions(plan, out.Phase)
	return out
}

func local(item spec.PlanItem, plan spec.PlanSpec, repository string) bool {
	owner := item.Repository
	if owner == "" {
		owner = plan.Repository
	}
	return repository == "" || owner == "" || owner == repository
}

func previews(item spec.PlanItem, contexts map[string]spec.SystemContext) []string {
	out := []string{}
	for _, edit := range item.Edits {
		systemContext, ok := contexts[edit.Context]
		if !ok {
			out = append(out, edit.Context+": no such context yet")
			continue
		}
		out = append(out, Preview(systemContext.Spec, edit))
	}
	return out
}

func unfinished(dependencies []string, phases map[string]string, plan spec.Plan) []string {
	out := []string{}
	for _, dependency := range dependencies {
		phase, ok := phases[dependency]
		if !ok {
			recorded, _ := plan.ItemStatus(dependency)
			phase = recorded.Phase
		}
		if phase != ItemDone {
			out = append(out, dependency)
		}
	}
	sort.Strings(out)
	return out
}

func start(item spec.PlanItem, status spec.PlanItemStatus, input Input, busy map[string]string, actions []Action) (spec.PlanItemStatus, []Action) {
	if len(item.Edits) == 0 {
		status.Phase = ItemVerifying
		return status, actions
	}
	pending := []spec.PlanEdit{}
	for _, edit := range item.Edits {
		systemContext, ok := input.Contexts[edit.Context]
		if !ok {
			status.Phase = ItemBlocked
			status.Message = "waits for context " + edit.Context + " to exist"
			return status, actions
		}
		if holder, taken := busy[edit.Context]; taken && holder != item.ID {
			status.Phase = ItemBlocked
			status.Message = "waits for item " + holder + " to realize its edit of " + edit.Context
			return status, actions
		}
		if !Carries(systemContext.Spec, edit) {
			pending = append(pending, edit)
		}
	}
	for _, edit := range item.Edits {
		busy[edit.Context] = item.ID
	}
	status.Phase = ItemRealizing
	if len(pending) > 0 {
		status.Message = fmt.Sprintf("applying %d staged edit(s)", len(pending))
		actions = append(actions, Action{Kind: ActionApply, Item: item.ID, Edits: pending})
	}
	return status, actions
}

func realizing(item spec.PlanItem, status spec.PlanItemStatus, input Input) spec.PlanItemStatus {
	waiting := []string{}
	changes := map[string]bool{}
	for _, name := range status.Changes {
		changes[name] = true
	}
	for _, edit := range item.Edits {
		systemContext, ok := input.Contexts[edit.Context]
		if !ok {
			status.Phase = ItemFailed
			status.Message = "context " + edit.Context + " disappeared while its edit was realized"
			return status
		}
		applied := status.AppliedSpecHashes[edit.Context]
		attempts, active := 0, false
		for _, change := range input.Changes {
			if change.Spec.SystemContext != edit.Context || change.Spec.Direction != specapi.DirectionSpecToCode {
				continue
			}
			if applied == "" || change.Spec.ToSpecHash != applied {
				continue
			}
			changes[change.Name] = true
			switch change.Status.Phase {
			case specapi.PhaseFailed:
				attempts++
			case specapi.PhaseSucceeded:
			default:
				active = true
			}
		}
		realized := systemContext.Status.RealizedSpec
		if realized != nil && Carries(*realized, edit) {
			continue
		}
		if !active && input.MaxAttempts > 0 && attempts >= input.MaxAttempts {
			status.Phase = ItemFailed
			status.Changes = sortedKeys(changes)
			status.Message = fmt.Sprintf("the edit of %s failed %d attempt(s)", edit.Context, attempts)
			return status
		}
		waiting = append(waiting, edit.Context)
	}
	status.Changes = sortedKeys(changes)
	if len(waiting) > 0 {
		status.Message = "waits for the code of " + strings.Join(waiting, ", ")
		return status
	}
	status.Phase = ItemVerifying
	status.Message = "realized"
	return status
}

func planPhase(plan spec.PlanSpec, items []spec.PlanItemStatus) string {
	if !plan.Approved {
		return PhaseDraft
	}
	done, started := 0, false
	for _, item := range items {
		switch item.Phase {
		case ItemFailed:
			return PhaseFailed
		case ItemDone:
			done++
			started = true
		case ItemRealizing, ItemVerifying:
			started = true
		}
	}
	switch {
	case done == len(items):
		return PhaseComplete
	case started:
		return PhaseRunning
	}
	return PhaseApproved
}

func conditions(plan spec.Plan, phase string) []metav1.Condition {
	now := metav1.Now()
	approved := metav1.Condition{
		Type: ConditionApproved, Status: metav1.ConditionFalse, Reason: "Draft",
		Message: "nothing is applied before the plan is approved", ObservedGeneration: plan.GetGeneration(),
	}
	if plan.Spec.Approved {
		approved.Status, approved.Reason, approved.Message = metav1.ConditionTrue, "Approved", "approved by "+valueOr(plan.Spec.ApprovedBy, "an operator")
	}
	complete := metav1.Condition{
		Type: ConditionComplete, Status: metav1.ConditionFalse, Reason: phase,
		ObservedGeneration: plan.GetGeneration(),
	}
	if phase == PhaseComplete {
		complete.Status = metav1.ConditionTrue
	}
	out := []metav1.Condition{}
	for _, next := range []metav1.Condition{approved, complete} {
		next.LastTransitionTime = now
		for _, previous := range plan.Status.Conditions {
			if previous.Type == next.Type && previous.Status == next.Status {
				next.LastTransitionTime = previous.LastTransitionTime
			}
		}
		out = append(out, next)
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// AcceptanceHash is the stable hash of an item's acceptance steps: the sha256
// of their JSON, hex, first 16 characters. An item status records it with the
// results, so changed steps are seen as a different acceptance.
func AcceptanceHash(steps []spec.AcceptanceStep) string {
	if len(steps) == 0 {
		return ""
	}
	encoded, err := json.Marshal(steps)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])[:16]
}

// AcceptancePassed reports whether every step passed: an item's acceptance is
// its done criteria, so every step gates.
func AcceptancePassed(results []spec.AcceptanceResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if !result.Passed {
			return false
		}
	}
	return true
}

// Finish records an acceptance run on an item status: the head it ran at, the
// results, and the hash of the steps they came from, so a later evaluation
// sees whether the steps have changed since.
func Finish(status spec.PlanItemStatus, head string, results []spec.AcceptanceResult, acceptanceHash string) spec.PlanItemStatus {
	status.Commit = head
	status.AcceptanceHash = acceptanceHash
	status.Acceptance = results
	if AcceptancePassed(results) {
		status.Phase = ItemDone
		status.Message = "acceptance passed at " + short(head)
		return status
	}
	failed := []string{}
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Name)
		}
	}
	status.Message = "acceptance failed at " + short(head) + ": " + strings.Join(failed, ", ") + "; it runs again when HEAD moves or the steps change"
	return status
}
