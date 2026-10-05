package requirementtexthasmachinepath

violation[{"msg": msg, "details": details}] {
	entry := input.review.object.spec.delta.requirements[_]
	to := object.get(entry, "to", {})
	is_object(to)
	text := object.get(to, "text", "")
	is_string(text)
	pattern := input.parameters.machinePathPatterns[_]
	found := regex.find_n(pattern, text, -1)
	count(found) > 0
	id := object.get(entry, "id", "?")
	msg := sprintf("requirement %s names %v, a path that exists on the machine that wrote it and nowhere else", [id, found])
	details := {"requirement": id, "paths": found, "text": text}
}
