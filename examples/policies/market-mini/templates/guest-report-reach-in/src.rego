package guestreportreachin

import data.lib.specd

violation[specd.violation(msg, details)] {
	emitter := specd.nodes_matching_globs(input.parameters.hostGlobs, input.parameters.emitterPatterns)[_]
	specd.definition_node(emitter)
	reachable := specd.closure_from({emitter.id}, input.parameters.edgeKinds)
	target := specd.nodes_with_id(reachable)[_]
	specd.definition_node(target)
	specd.matches_globs(input.parameters.hostGlobs, target.file)
	pattern := input.parameters.reachInPatterns[_]
	re_match(pattern, target.text)
	msg := sprintf("%s reaches into the guest from the network emitter %s: %q", [target.qualifiedName, emitter.qualifiedName, pattern])
	details := specd.location(target.file, specd.node_match_line(target, pattern))
}
