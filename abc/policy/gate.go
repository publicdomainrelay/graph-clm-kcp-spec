package policy

import (
	"fmt"
	"sort"
)

type RepositoryPolicy struct {
	Branch string

	Enforcement Enforcement

	Disabled bool
}

func (p RepositoryPolicy) Cap() Enforcement {
	if p.Disabled {
		return EnforcementDryRun
	}
	if !p.Enforcement.Known() {
		return EnforcementDeny
	}
	return p.Enforcement
}

func Cap(action, cap Enforcement) Enforcement {
	if EnforcementRank(action) > EnforcementRank(cap) {
		return cap
	}
	return action
}

type Override struct {
	Constraint string

	Reason string

	By string
}

func (o Override) Step() string {
	return "policy:" + o.Constraint
}

func ParseOverride(step, reason, by string) (Override, bool) {
	const prefix = "policy:"
	if len(step) <= len(prefix) || step[:len(prefix)] != prefix {
		return Override{}, false
	}
	return Override{Constraint: step[len(prefix):], Reason: reason, By: by}, true
}

type Decision struct {
	Denied []Violation

	Warned []Violation

	DryRun []Violation

	Waived []Violation

	Capped []Violation

	Blocked bool
}

func Decide(report Report, repository RepositoryPolicy, overrides []Override) Decision {
	waived := map[string]bool{}
	for _, override := range overrides {
		waived[override.Constraint] = true
	}
	cap := repository.Cap()

	decision := Decision{}
	for _, violation := range report.Violations {
		if waived[violation.Constraint] {
			decision.Waived = append(decision.Waived, violation)
			continue
		}
		effective := Cap(violation.Enforcement, cap)
		if effective != violation.Enforcement {
			decision.Capped = append(decision.Capped, violation)
		}
		switch effective {
		case EnforcementDeny:
			decision.Denied = append(decision.Denied, violation)
		case EnforcementWarn:
			decision.Warned = append(decision.Warned, violation)
		default:
			decision.DryRun = append(decision.DryRun, violation)
		}
	}
	decision.Blocked = len(decision.Denied) > 0
	return decision
}

func (d Decision) Messages() []string {
	out := make([]string, 0, len(d.Denied))
	for _, violation := range d.Denied {
		out = append(out, fmt.Sprintf("%s: %s", violation.Constraint, violation.Msg))
	}
	sort.Strings(out)
	return out
}

func (d Decision) Reasons() []string {
	out := make([]string, 0, len(d.Denied))
	for _, violation := range d.Denied {
		out = append(out, violation.Msg)
	}
	return out
}

func (d Decision) WaivedConstraints() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, violation := range d.Waived {
		if seen[violation.Constraint] {
			continue
		}
		seen[violation.Constraint] = true
		out = append(out, violation.Constraint)
	}
	sort.Strings(out)
	return out
}
