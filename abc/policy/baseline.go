package policy

import "strconv"

// BaselineKey identifies a violation by constraint, object and site, so a
// change-scoped gate can tell a violation the change introduced from one the
// base already carried. The message is left out: a rule that rewords its
// message about the same site has not added a violation.
func BaselineKey(violation Violation) string {
	file, line := "", 0
	if violation.Location != nil {
		file, line = violation.Location.File, violation.Location.Line
	}
	return ViolationID(violation.Constraint, violation.Object.String(), file, strconv.Itoa(line))
}

// BaselineKeys is every key a report carries.
func BaselineKeys(report Report) map[string]bool {
	out := make(map[string]bool, len(report.Violations))
	for _, violation := range report.Violations {
		out[BaselineKey(violation)] = true
	}
	return out
}

// Inherited marks a violation that was already present at the base. It is
// reported, never blocking.
func inherited(violations []Violation) []Violation {
	out := make([]Violation, 0, len(violations))
	for _, violation := range violations {
		violation.Enforcement = EnforcementWarn
		out = append(out, violation)
	}
	return out
}

// DecideBaseline is Decide scoped to a change: a violation the base report
// already carried is inherited and cannot block; only a new violation is
// decided normally. With baseline disabled it is Decide over the whole head.
func DecideBaseline(head, base Report, repository RepositoryPolicy, overrides []Override) Decision {
	if !repository.BaselineEnabled() {
		return Decide(head, repository, overrides)
	}
	existing := BaselineKeys(base)
	head.Violations = append([]Violation(nil), head.Violations...)
	scoped := head
	scoped.Violations = nil
	inheritedViolations := []Violation{}
	for _, violation := range head.Violations {
		if existing[BaselineKey(violation)] {
			inheritedViolations = append(inheritedViolations, violation)
			continue
		}
		scoped.Violations = append(scoped.Violations, violation)
	}
	decision := Decide(scoped, repository, overrides)
	decision.Inherited = inherited(inheritedViolations)
	return decision
}
