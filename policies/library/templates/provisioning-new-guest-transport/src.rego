package provisioningnewguesttransport

violation[{"msg": msg, "details": details}] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	pattern := transport_patterns[_]
	regex.match(pattern, row.text)
	not module_marker(row.text)
	not allowed(row.text)
	msg := sprintf("added line %s:%d introduces a guest transport the cloud-init does not deploy: %q; a new guest transport is a UserDataModule in cloud-init-common's registry, so the RFP flow stays the only way a guest comes up", [file.path, row.line, trim_space(row.text)])
	details := {"file": file.path, "line": row.line, "pattern": pattern}
}

transport_patterns = out {
	out := input.parameters.transportPatterns
} else = [`\bwebsocat\b`, `\bwstunnel\b`, `\bchisel\b`, `\bfrp[cs]\b`, `\bautossh\b`, `\brathole\b`, `\bsocat\b`]

module_marker(text) {
	regex.match(module_patterns[_], text)
}

module_patterns = out {
	out := input.parameters.modulePatterns
} else = [`UserDataModule`, `user_data`, `userData`, `cloud-init`, `cloud_config`]

allowed(text) {
	regex.match(input.parameters.allowPatterns[_], text)
}
