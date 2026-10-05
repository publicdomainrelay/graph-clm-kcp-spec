package provisioningnewguesttransport

import data.lib.specd

violation[{"msg": msg, "details": details}] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	patterns := matched_patterns(row.text)
	count(patterns) > 0
	not comment(row.text)
	not ignored(file.path)
	not module_marker(row.text)
	not allowed(row.text)
	msg := sprintf("added line %s:%d introduces a guest transport the cloud-init does not deploy: %q; a new guest transport is a UserDataModule in cloud-init-common's registry, so the RFP flow stays the only way a guest comes up", [file.path, row.line, trim_space(row.text)])
	details := {"file": file.path, "line": row.line, "patterns": patterns}
}

matched_patterns(text) = out {
	out := [pattern | pattern := transport_patterns[_]; regex.match(pattern, text)]
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

ignored(path) {
	specd.matches_globs(ignored_paths, path)
}

ignored_paths = out {
	out := input.parameters.ignoredPaths
} else = [`**/*.md`]

allowed(text) {
	regex.match(input.parameters.allowPatterns[_], text)
}

comment(text) {
	regex.match(comment_patterns[_], text)
}

comment_patterns = out {
	out := input.parameters.commentPatterns
} else = [`^\s*#`, `^\s*//`, `^\s*\*`]
