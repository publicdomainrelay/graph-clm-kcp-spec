package spec

func AcceptanceBlocked(steps []AcceptanceStep, results []AcceptanceResult) (AcceptanceResult, bool) {
	for index, step := range steps {
		if index >= len(results) {
			break
		}
		if step.Gate && !results[index].Passed && !results[index].Overridden {
			return results[index], true
		}
	}
	return AcceptanceResult{}, false
}

// ApplyOverrides marks every failed step an operator's override names, so a
// red gate lands one commit instead of freezing the repository. It returns the
// overrides it consumed.
func ApplyOverrides(results []AcceptanceResult, overrides []AcceptanceOverride) []AcceptanceOverride {
	if len(overrides) == 0 {
		return nil
	}
	consumed := []AcceptanceOverride{}
	for index := range results {
		if results[index].Passed {
			continue
		}
		for _, override := range overrides {
			if override.Step != results[index].Name {
				continue
			}
			results[index].Overridden = true
			results[index].OverrideBy = override.By
			results[index].OverrideReason = override.Reason
			consumed = append(consumed, override)
			break
		}
	}
	return consumed
}

func AcceptanceTrailerLines(steps []AcceptanceStep, results []AcceptanceResult) []string {
	lines := make([]string, 0, len(results))
	for index, result := range results {
		if result.Overridden {
			lines = append(lines, overrideTrailerLine(result))
			continue
		}
		lines = append(lines, result.Name+" "+acceptanceState(result)+" ("+acceptanceKind(steps, index)+")")
	}
	return lines
}

func overrideTrailerLine(result AcceptanceResult) string {
	by := result.OverrideBy
	if by == "" {
		by = "an operator"
	}
	reason := result.OverrideReason
	if reason == "" {
		reason = "no reason given"
	}
	return result.Name + " overridden by " + by + " (" + reason + ")"
}

// OverriddenSteps names the steps a set of results landed past their gate.
func OverriddenSteps(results []AcceptanceResult) []string {
	out := []string{}
	for _, result := range results {
		if result.Overridden {
			out = append(out, result.Name)
		}
	}
	return out
}

func acceptanceState(result AcceptanceResult) string {
	if result.Passed {
		return "passed"
	}
	return "failed"
}

func acceptanceKind(steps []AcceptanceStep, index int) string {
	if index < len(steps) && steps[index].Gate {
		return "gate"
	}
	return "report"
}
