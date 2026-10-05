package guestreportdrivenemission

import data.lib.specd

violation[specd.violation(msg, details)] {
	emitter := specd.nodes_matching_globs(input.parameters.hostGlobs, input.parameters.emitterPatterns)[_]
	specd.definition_node(emitter)
	not driven_by_handler(emitter)
	msg := sprintf("%s emits the guest network identity from the provisioning lifecycle, not from an inbound guest report", [emitter.qualifiedName])
	details := specd.location(emitter.file, specd.node_match_line(emitter, input.parameters.emitterPatterns))
}

driven_by_handler(emitter) {
	handler := specd.nodes_identified(input.parameters.handlerPatterns)[_]
	specd.closure_from({handler.id}, input.parameters.edgeKinds)[emitter.id]
}
