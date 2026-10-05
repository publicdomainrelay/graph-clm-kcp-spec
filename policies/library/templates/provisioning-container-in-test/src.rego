package provisioningcontainerintest

import data.lib.specd

violation[{"msg": msg, "details": details}] {
	file := input.review.object.spec.files[_]
	specd.matches_globs(file_globs, file.path)
	row := file.added[_]
	pattern := container_patterns[_]
	not ignored(file.path)
	regex.match(pattern, row.text)
	msg := sprintf("added test line %s:%d stands up its own container: %q; a test that needs a live guest drives runComputeContract against a real local bidder, it never runs a container itself", [file.path, row.line, trim_space(row.text)])
	details := specd.location(file.path, row.line)
}

file_globs = out {
	out := input.parameters.testGlobs
} else = ["test/**", "**/*_test.go", "**/*_test.ts", "**/*.test.ts"]

container_patterns = out {
	out := input.parameters.containerPatterns
} else = [`\b(?:docker|podman|container|nerdctl)\s+(?:run|exec)\b`]

ignored(path) {
	specd.matches_globs(input.parameters.ignoredPaths, path)
}
