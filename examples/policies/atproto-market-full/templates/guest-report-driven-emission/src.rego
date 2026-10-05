package guestreportdrivenemission

import data.lib.specd

subject := object.get(input.parameters, "subject", "the guest network identity")

violation[specd.violation(msg, details)] {
	emitter := specd.nodes_matching_globs(input.parameters.hostGlobs, input.parameters.emitterPatterns)[_]
	specd.definition_node(emitter)
	not driven_by_handler(emitter)
	msg := sprintf("%s emits %s from the provisioning lifecycle, not from an inbound guest report", [emitter.qualifiedName, subject])
	details := specd.location(emitter.file, specd.node_match_line(emitter, input.parameters.emitterPatterns))
}

driven_by_handler(emitter) {
	handler := specd.nodes_identified(input.parameters.handlerPatterns)[_]
	specd.definition_node(handler)
	specd.closure_from({handler.id}, input.parameters.edgeKinds)[emitter.id]
}
