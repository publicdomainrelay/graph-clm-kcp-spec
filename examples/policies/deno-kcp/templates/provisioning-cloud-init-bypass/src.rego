package provisioningcloudinitbypass

violation[{"msg": msg, "details": details}] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	user_data_marker(row.text)
	bypass_marker(row.text)
	not allowed(row.text)
	msg := sprintf("added line %s:%d skips the cloud-init user_data path: %q; every guest must come up through the user_data the RFP flow produces", [file.path, row.line, trim_space(row.text)])
	details := {"file": file.path, "line": row.line}
}

user_data_marker(text) {
	regex.match(input.parameters.userDataPatterns[_], text)
}

bypass_marker(text) {
	regex.match(input.parameters.bypassPatterns[_], text)
}

allowed(text) {
	regex.match(input.parameters.allowPatterns[_], text)
}
