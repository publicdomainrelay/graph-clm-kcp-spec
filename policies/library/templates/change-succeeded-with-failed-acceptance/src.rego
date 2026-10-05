package changesucceededwithfailedacceptance

violation[{"msg": msg, "details": details}] {
	change := input.review.object
	change.status.phase == "Succeeded"
	some index
	step := change.status.acceptance[index]
	step.passed == false
	not overridden(step)
	code := object.get(step, "exitCode", null)
	seconds := object.get(step, "durationSeconds", null)
	msg := sprintf("change %s is Succeeded while acceptance step %s reports passed false (exit code %v, %v seconds), so the gate that exists to stop it was recorded and then ignored", [change.metadata.name, object.get(step, "name", "?"), code, seconds])
	details := {
		"phase": change.status.phase,
		"acceptance": object.get(step, "name", ""),
		"exitCode": code,
		"outputTail": object.get(step, "outputTail", ""),
		"index": index,
	}
}

overridden(step) {
	object.get(step, "overridden", false) == true
}
