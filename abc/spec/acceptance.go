package spec

func AcceptanceBlocked(steps []AcceptanceStep, results []AcceptanceResult) (AcceptanceResult, bool) {
	for index, step := range steps {
		if index >= len(results) {
			break
		}
		if step.Gate && !results[index].Passed {
			return results[index], true
		}
	}
	return AcceptanceResult{}, false
}

func AcceptanceTrailerLines(steps []AcceptanceStep, results []AcceptanceResult) []string {
	lines := make([]string, 0, len(results))
	for index, result := range results {
		lines = append(lines, result.Name+" "+acceptanceState(result)+" ("+acceptanceKind(steps, index)+")")
	}
	return lines
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
