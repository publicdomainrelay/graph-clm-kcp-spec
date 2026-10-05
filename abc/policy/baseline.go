package policy

import "strconv"

func BaselineKey(violation Violation) string {
	file, line := "", 0
	if violation.Location != nil {
		file, line = violation.Location.File, violation.Location.Line
	}
	return ViolationID(violation.Constraint, violation.Object.String(), file, strconv.Itoa(line))
}

func BaselineKeys(report Report) map[string]bool {
	out := make(map[string]bool, len(report.Violations))
	for _, violation := range report.Violations {
		out[BaselineKey(violation)] = true
	}
	return out
}

func inherited(violations []Violation) []Violation {
	out := make([]Violation, 0, len(violations))
	for _, violation := range violations {
		violation.Enforcement = EnforcementWarn
		out = append(out, violation)
	}
	return out
}

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
