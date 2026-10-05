package guestreportcloudinit

import data.lib.specd

violation[specd.violation(msg, details)] {
	files := specd.files_matching(input.parameters.guestGlobs)
	count(files) > 0
	not reports_out
	file := files[0]
	msg := sprintf("no cloud-init user_data module publishes the guest's address or routing outbound: %s", [file.path])
	details := specd.location(file.path, 1)
}

reports_out {
	file := specd.files_matching(input.parameters.guestGlobs)[_]
	re_match(input.parameters.reportPattern, specd.code_graph.spec.texts[file.path])
}
