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

# The payload must come from the guest's report, not from a source the host
# reads itself. An emitter the host's own source reaches -- a DHCP lease file,
# a virsh query, a container inspect -- carries an address the guest never
# reported, so the emission is denied even though the report handler is on the
# path (review 0006 N8).
violation[specd.violation(msg, details)] {
	emitter := specd.nodes_matching_globs(input.parameters.hostGlobs, input.parameters.emitterPatterns)[_]
	specd.definition_node(emitter)
	source := specd.nodes_identified(input.parameters.hostSourcePatterns)[_]
	specd.definition_node(source)
	specd.closure_from({source.id}, input.parameters.edgeKinds)[emitter.id]
	msg := sprintf("%s emits %s from a source the host reads itself, not from the guest's report", [emitter.qualifiedName, subject])
	details := specd.location(emitter.file, specd.node_match_line(emitter, input.parameters.emitterPatterns))
}
