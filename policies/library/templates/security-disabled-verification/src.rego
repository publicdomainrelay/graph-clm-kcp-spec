package securitydisabledverification

violation[{"msg": msg, "details": details}] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	not comment(row.text)
	pattern := verification_patterns[_]
	regex.match(pattern, row.text)
	not allowed(row.text)
	msg := sprintf("added line %s:%d is %q, which turns certificate verification off; a check that trusts any certificate checks nothing", [file.path, row.line, trim_space(row.text)])
	details := {"file": file.path, "line": row.line, "pattern": pattern}
}

verification_patterns = out {
	out := input.parameters.disabledVerificationPatterns
} else = [`--validate=false`, `--insecure\b`, `InsecureSkipVerify`, `rejectUnauthorized\s*:\s*false`, `\bcurl\b[^\n]*\s-[A-Za-z]*k`]

comment(text) {
	regex.match(comment_patterns[_], text)
}

comment_patterns = out {
	out := input.parameters.commentPatterns
} else = [`^\s*#`, `^\s*//`, `^\s*\*`]

allowed(text) {
	regex.match(input.parameters.allowPatterns[_], text)
}
