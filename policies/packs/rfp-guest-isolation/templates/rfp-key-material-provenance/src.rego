package rfpkeymaterialprovenance

import data.lib.specd

guest_role := object.get(input.parameters, "guestRole", "guest")

default_key_patterns := ["authorized_keys", "ssh-keygen", "BEGIN [A-Z ]*PRIVATE KEY"]

default_written_kinds := ["proc.exec", "file.write"]

violation[specd.violation(msg, details)] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	not specd.file_in_role(file.path, guest_role)
	not specd.matches_globs(ignored_paths, file.path)
	pattern := key_pattern(row.text)[0]
	written(file.path, row.line)
	msg := sprintf("added line %s:%d makes ssh key material by hand outside the %s role's user_data: %q; a guest's keys are placed by the cloud-init user_data and the guest is reached through the relay",
		[file.path, row.line, guest_role, pattern])
	details := {"file": file.path, "line": row.line, "pattern": pattern}
}

ignored_paths = out {
	out := input.parameters.ignoredPaths
} else = ["**/*.md"]

key_pattern(text) = out {
	out := [pattern | pattern := key_pattern_list[_]; regex.match(pattern, text)]
}

key_pattern_list = out {
	out := input.parameters.keyPatterns
} else = default_key_patterns


written(path, line) {
	effect := specd.effects[_]
	effect.file == path
	effect.line == line
	written_kind(effect.kind)
}

written_kind(kind) {
	kind == input.parameters.writtenKinds[_]
}

written_kind(kind) {
	not configured_written_kinds
	kind == default_written_kinds[_]
}

configured_written_kinds {
	count(input.parameters.writtenKinds) > 0
}
