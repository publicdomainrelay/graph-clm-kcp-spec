package relayonlyssh

import data.lib.specd

violation[specd.violation(msg, details)] {
	driver := specd.tests_matching_text(input.parameters.testGlobs, input.parameters.driverIdentifiers)[_]
	reachable := specd.closure_from({specd.file_node(driver.path).id}, input.parameters.edgeKinds)
	call := specd.nodes_with_id(reachable)[_]
	specd.definition_node(call)
	re_match(input.parameters.sshPattern, call.text)
	not proxied(call)
	msg := sprintf("ssh invocation %s reachable from %s carries no allowed ProxyCommand transport", [call.qualifiedName, driver.path])
	details := specd.location(call.file, specd.node_match_line(call, input.parameters.sshPattern))
}

proxied(call) {
	with_proxy_command(call)
	with_allowed_transport(call)
}

with_proxy_command(call) {
	node := specd.nodes_with_id(specd.closure_from({call.id}, input.parameters.edgeKinds))[_]
	re_match(input.parameters.proxyCommandPattern, node.text)
}

with_allowed_transport(call) {
	node := specd.nodes_with_id(specd.closure_from({call.id}, input.parameters.edgeKinds))[_]
	transport := input.parameters.allowedTransports[_]
	re_match(transport, node.text)
}

violation[specd.violation(msg, details)] {
	driver := specd.tests_matching_text(input.parameters.testGlobs, input.parameters.driverIdentifiers)[_]
	reachable := specd.closure_from({specd.file_node(driver.path).id}, input.parameters.edgeKinds)
	call := specd.nodes_with_id(reachable)[_]
	specd.definition_node(call)
	pattern := input.parameters.directDialPatterns[_]
	re_match(pattern, call.text)
	msg := sprintf("test %s dials a guest address directly: %s matches %q", [driver.path, call.qualifiedName, pattern])
	details := specd.location(call.file, specd.node_match_line(call, pattern))
}
