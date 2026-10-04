package oabranch

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/delta"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/mirror"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/abc/spec"
	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/specapi"
)

// declaredForBranch is the spec as specs/<context>.yaml carries it: the derived
// top-level codeRefs live in status/, so a subject must not report them as a
// spec edit on every commit.
func declaredForBranch(in spec.SystemContextSpec) spec.SystemContextSpec {
	out := spec.Canonicalize(in)
	out.CodeRefs = nil
	return out
}

func mirrorParse(name string, data []byte) (spec.SystemContextSpec, error) {
	context, err := mirror.Parse(name, data)
	if err != nil {
		return spec.SystemContextSpec{}, err
	}
	return context.Spec, nil
}

const subjectPartLimit = 8

// Subjects names what a commit changed, one line per kind: a spec edit by
// requirement id, a status rewrite by condition, a change by phase. The first
// line is the commit subject, the rest belong in the body.
func Subjects(plan Plan, snapshot Snapshot, previous map[string][]byte) []string {
	touched := plan.touched()
	subjects := []string{}
	for _, context := range sortedContexts(snapshot.Contexts) {
		path := SpecPath(context.Name)
		if !touched[path] {
			continue
		}
		subjects = append(subjects, specSubject(context, previous[path], plan.removes(path)))
	}
	for _, change := range snapshot.Changes {
		path := ChangePath(change.Name)
		if !touched[path] {
			continue
		}
		subjects = append(subjects, changeSubject(change, previous[path]))
	}
	for _, context := range sortedContexts(snapshot.Contexts) {
		path := StatusPath(context.Name)
		if !touched[path] {
			continue
		}
		subjects = append(subjects, statusSubject(context, previous[path]))
	}
	return subjects
}

func (p Plan) touched() map[string]bool {
	touched := make(map[string]bool, len(p.Added)+len(p.Modified)+len(p.Remove))
	for _, path := range p.Paths() {
		touched[path] = true
	}
	return touched
}

func (p Plan) removes(path string) bool {
	for _, candidate := range p.Remove {
		if candidate == path {
			return true
		}
	}
	return false
}

func specSubject(context spec.SystemContext, previous []byte, removed bool) string {
	old := declaredForBranch(spec.SystemContextSpec{})
	if len(previous) > 0 {
		if parsed, err := mirrorParse(context.Name, previous); err == nil {
			old = declaredForBranch(parsed)
		}
	}
	next := declaredForBranch(context.Spec)
	if removed {
		next = declaredForBranch(spec.SystemContextSpec{Repository: context.Spec.Repository, Upstream: context.Spec.Upstream})
	}
	change := delta.Diff(old, next)
	parts := []string{}
	if change.Intent != nil {
		parts = append(parts, "~intent")
	}
	if change.Upstream != nil {
		parts = append(parts, "~upstream")
	}
	if change.Orchestrator != nil {
		parts = append(parts, "~orchestrator")
	}
	for _, set := range []struct {
		name  string
		delta *spec.StringSetDelta
	}{
		{"overlay", change.Overlay},
		{"dependsOn", change.DependsOn},
		{"introduces", change.Introduces},
		{"codeRefs", change.CodeRefs},
	} {
		if set.delta == nil {
			continue
		}
		for _, added := range set.delta.Added {
			parts = append(parts, "+"+added)
		}
		for _, gone := range set.delta.Removed {
			parts = append(parts, "-"+gone)
		}
	}
	for _, requirement := range change.Requirements {
		parts = append(parts, op(requirement.Op)+requirement.ID)
	}
	for _, declared := range change.Interfaces {
		parts = append(parts, op(declared.Op)+declared.Name)
	}
	return "spec(" + context.Name + "): " + joined(parts, "no declared change")
}

func op(kind string) string {
	switch kind {
	case spec.OpAdded:
		return "+"
	case spec.OpRemoved:
		return "-"
	}
	return "~"
}

func joined(parts []string, fallback string) string {
	if len(parts) == 0 {
		return fallback
	}
	sorted := append([]string{}, parts...)
	sort.Strings(sorted)
	if len(sorted) > subjectPartLimit {
		extra := len(sorted) - subjectPartLimit
		sorted = append(sorted[:subjectPartLimit], fmt.Sprintf("+%d more", extra))
	}
	return strings.Join(sorted, " ")
}

func changeSubject(change spec.SpecChange, previous []byte) string {
	old := changeDoc{}
	if len(previous) > 0 {
		_ = yaml.Unmarshal(previous, &old)
	}
	phase := string(change.Status.Phase)
	if old.Status.Phase == "" {
		return "change(" + change.Name + "): " + phase
	}
	if old.Status.Phase != change.Status.Phase {
		return "change(" + change.Name + "): " + old.Status.Phase + " -> " + phase
	}
	return "change(" + change.Name + "): " + phase + " updated"
}

func statusSubject(context spec.SystemContext, previous []byte) string {
	old := statusDoc{}
	if len(previous) > 0 {
		_ = yaml.Unmarshal(previous, &old)
	}
	parts := []string{}
	before := map[string]string{}
	for _, condition := range old.Conditions {
		before[condition.Type] = condition.Status
	}
	for _, condition := range context.Status.Conditions {
		if before[condition.Type] == string(condition.Status) {
			continue
		}
		parts = append(parts, condition.Type+"="+string(condition.Status))
	}
	if !reflect.DeepEqual(old.Observed, context.Status.Observed) {
		if commit := short(context.Status.ObservedCommit); commit != "" {
			parts = append(parts, "observed "+commit)
		} else {
			parts = append(parts, "observed facts")
		}
	}
	if old.SyncedFingerprint != context.Status.SyncedFingerprint {
		parts = append(parts, "synced")
	}
	if old.RealizedSpecHash != context.Status.RealizedSpecHash {
		parts = append(parts, "realizedSpec")
	}
	return "status(" + context.Name + "): " + joined(parts, "state")
}

func short(commit string) string {
	if len(commit) <= 8 {
		return commit
	}
	return commit[:8]
}

// episodes groups the changes that repeat one another, each group with the
// surviving attempt first: the one that succeeded, else the newest, else the
// name that is not an -a<N> attempt.
func episodes(changes []spec.SpecChange) [][]spec.SpecChange {
	groups := map[string][]spec.SpecChange{}
	order := []string{}
	for _, change := range changes {
		base := spec.EpisodeBase(change)
		if _, seen := groups[base]; !seen {
			order = append(order, base)
		}
		groups[base] = append(groups[base], change)
	}
	sort.Strings(order)
	out := make([][]spec.SpecChange, 0, len(order))
	for _, base := range order {
		group := groups[base]
		sort.Slice(group, func(left, right int) bool {
			if survivorRank(group[left]) != survivorRank(group[right]) {
				return survivorRank(group[left]) < survivorRank(group[right])
			}
			if !group[left].CreationTimestamp.Equal(&group[right].CreationTimestamp) {
				return group[right].CreationTimestamp.Before(&group[left].CreationTimestamp)
			}
			return group[left].Name < group[right].Name
		})
		out = append(out, group)
	}
	return out
}

func survivorRank(change spec.SpecChange) int {
	switch change.Status.Phase {
	case specapi.PhaseSucceeded:
		return 0
	case specapi.PhaseRunning:
		return 1
	case specapi.PhasePending:
		return 2
	}
	return 3
}

func superseded(episode []spec.SpecChange) []supersededDoc {
	if len(episode) < 2 {
		return nil
	}
	out := make([]supersededDoc, 0, len(episode)-1)
	for _, attempt := range episode[1:] {
		out = append(out, supersededDoc{
			Name:    attempt.Name,
			Phase:   string(attempt.Status.Phase),
			Commit:  attempt.Status.Commit,
			Message: attempt.Status.Message,
		})
	}
	return out
}

// CoalesceProgress keeps a change file out of a commit when the only thing that
// moved is its progress list. The file is persisted on a phase transition and
// at the end, so the branch does not carry one commit per recorded turn.
func CoalesceProgress(plan Plan, previous map[string][]byte) (Plan, []string) {
	deferred := []string{}
	for path, data := range plan.Write {
		if !isChangePath(path) {
			continue
		}
		old, ok := previous[path]
		if !ok {
			continue
		}
		if !progressOnly(old, data) {
			continue
		}
		delete(plan.Write, path)
		plan.Modified = without(plan.Modified, path)
		plan.Added = without(plan.Added, path)
		deferred = append(deferred, path)
	}
	sort.Strings(deferred)
	return plan, deferred
}

func without(paths []string, path string) []string {
	out := paths[:0]
	for _, candidate := range paths {
		if candidate != path {
			out = append(out, candidate)
		}
	}
	return out
}

func progressOnly(old, next []byte) bool {
	before, after := changeDoc{}, changeDoc{}
	if err := yaml.Unmarshal(old, &before); err != nil {
		return false
	}
	if err := yaml.Unmarshal(next, &after); err != nil {
		return false
	}
	before.Status.Progress = nil
	after.Status.Progress = nil
	before.Status.Acceptance = nil
	after.Status.Acceptance = nil
	return reflect.DeepEqual(before, after)
}

func isChangePath(path string) bool {
	return IsChangePath(path)
}

// PreserveChanges keeps every change file the branch already carries. changes/
// is the branch's attempt history and is append-only: kcp holds only the
// changes of the branch it is watching, so a re-render from a smaller kcp - a
// restore, a superseded attempt - must not delete what the branch recorded.
func PreserveChanges(files map[string][]byte, previous map[string][]byte) {
	for path, data := range previous {
		if !isChangePath(path) {
			continue
		}
		if _, written := files[path]; written {
			continue
		}
		files[path] = data
	}
}

func changesDocument(snapshot Snapshot) string {
	base := snapshot.Baseline
	if base == nil {
		base = &Baseline{}
	}
	builder := strings.Builder{}
	fmt.Fprintf(&builder, "# Changes on `%s`\n\n", snapshot.branch())
	fmt.Fprintf(&builder, "The requirement-level delta against `%s`, and what this branch realized.\n", Branch(snapshot.Repository.Name))
	requirements := requirementDelta(base.Contexts, snapshot.Contexts)
	builder.WriteString("\n## Requirements\n")
	if len(requirements) == 0 {
		builder.WriteString("\n_None: this branch declares the same requirements as the default branch._\n")
	} else {
		for _, context := range sortedStrings(keys(requirements)) {
			builder.WriteString("\n### " + context + "\n\n")
			for _, line := range requirements[context] {
				builder.WriteString("- " + line + "\n")
			}
		}
	}
	builder.WriteString("\n## Realization\n")
	changes := branchChanges(base.Changes, snapshot.Changes)
	if len(changes) == 0 {
		builder.WriteString("\n_None: no SpecChange landed on this branch yet._\n")
		return builder.String()
	}
	builder.WriteString("\n| change | direction | phase | commit | verify | acceptance |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, change := range changes {
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %d | %s |\n",
			change.Name, change.Spec.Direction, change.Status.Phase, short(change.Status.Commit),
			change.Status.VerifyExitCode, acceptanceSummary(change))
	}
	return builder.String()
}

func acceptanceSummary(change spec.SpecChange) string {
	if len(change.Status.Acceptance) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(change.Status.Acceptance))
	for _, result := range change.Status.Acceptance {
		state := "failed"
		if result.Passed {
			state = "passed"
		}
		parts = append(parts, result.Name+" "+state)
	}
	return strings.Join(parts, ", ")
}

func requirementDelta(baseline, current []spec.SystemContext) map[string][]string {
	before := map[string]spec.SystemContextSpec{}
	for _, context := range baseline {
		before[context.Name] = context.Spec
	}
	out := map[string][]string{}
	for _, context := range current {
		old, had := before[context.Name]
		if !had {
			old = spec.SystemContextSpec{Repository: context.Spec.Repository, Upstream: context.Spec.Upstream}
		}
		change := delta.Diff(declaredForBranch(old), declaredForBranch(context.Spec))
		lines := []string{}
		if change.Intent != nil {
			lines = append(lines, fmt.Sprintf("intent: %s -> %s", quote(change.Intent.From), quote(change.Intent.To)))
		}
		for _, requirement := range change.Requirements {
			lines = append(lines, requirementLine(requirement))
		}
		if len(lines) > 0 {
			out[context.Name] = lines
		}
	}
	return out
}

func requirementLine(change spec.RequirementDelta) string {
	switch change.Op {
	case spec.OpAdded:
		return fmt.Sprintf("added `%s` (%s): %s", change.ID, levelOf(change.To), textOf(change.To))
	case spec.OpRemoved:
		return fmt.Sprintf("removed `%s` (%s)", change.ID, levelOf(change.From))
	}
	fields := strings.Join(change.Fields, ", ")
	if change.From != nil && change.To != nil && change.From.Level != change.To.Level {
		fields = fmt.Sprintf("%s -> %s", change.From.Level, change.To.Level)
	}
	return fmt.Sprintf("changed `%s` (%s): %s", change.ID, fields, textOf(change.To))
}

func levelOf(requirement *spec.Requirement) spec.Level {
	if requirement == nil {
		return ""
	}
	return requirement.Level
}

func textOf(requirement *spec.Requirement) string {
	if requirement == nil {
		return ""
	}
	return quote(requirement.Text)
}

func quote(text string) string {
	single := strings.Join(strings.Fields(text), " ")
	return "\"" + single + "\""
}

// branchChanges are the changes this branch carries that the default branch's
// architecture does not: new names, or a name whose phase moved on.
func branchChanges(baseline, current []spec.SpecChange) []spec.SpecChange {
	before := map[string]string{}
	for _, change := range baseline {
		before[change.Name] = change.Status.Phase
	}
	out := []spec.SpecChange{}
	for _, change := range current {
		if phase, known := before[change.Name]; known && phase == change.Status.Phase {
			continue
		}
		out = append(out, change)
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Name < out[right].Name })
	return out
}

func keys[V any](in map[string]V) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	return out
}

func sortedStrings(values []string) []string {
	sort.Strings(values)
	return values
}
