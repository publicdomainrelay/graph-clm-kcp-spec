package nodirectguestconnect

import data.lib.specd

violation[specd.violation(msg, specd.location(file.path, 1))] {
	file := specd.files_matching(input.parameters.globs)[_]
	re_match(input.parameters.pattern, specd.code_graph.spec.texts[file.path])
	msg := sprintf("integration tests must not dial a guest directly: %s matches %q", [file.path, input.parameters.pattern])
}
