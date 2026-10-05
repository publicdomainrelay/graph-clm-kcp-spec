package securitydisabledverification

violation[{"msg": msg, "details": details}] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	not comment(row.text)
	patterns := matched_patterns(row.text)
	count(patterns) > 0
	not allowed(row.text)
	msg := sprintf("added line %s:%d is %q, which turns certificate verification off; a check that trusts any certificate checks nothing", [file.path, row.line, trim_space(row.text)])
	details := {"file": file.path, "line": row.line, "patterns": patterns}
}

matched_patterns(text) = out {
	out := [pattern | pattern := verification_patterns[_]; regex.match(pattern, text)]
}

verification_patterns = out {
	out := input.parameters.disabledVerificationPatterns
} else = [`--insecure\b`, `InsecureSkipVerify`, `rejectUnauthorized\s*:\s*false`, `NODE_TLS_REJECT_UNAUTHORIZED\s*=\s*["']?0`, `--tls-verify\s*=\s*false`, `\bcurl\b[^\n]*\s-[A-Za-z]*k`]

comment(text) {
	regex.match(comment_patterns[_], text)
}

comment_patterns = out {
	out := input.parameters.commentPatterns
} else = [`^\s*#`, `^\s*//`, `^\s*\*`]

allowed(text) {
	regex.match(input.parameters.allowPatterns[_], text)
}
