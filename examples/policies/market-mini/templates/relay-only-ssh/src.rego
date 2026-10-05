package relayonlyssh

import data.lib.specd

violation[specd.violation(msg, details)] {
	driver := specd.tests_matching_text(input.parameters.testGlobs, input.parameters.driverIdentifiers)[_]
	reachable := specd.closure_from({specd.file_node(driver.path).id}, input.parameters.edgeKinds)
	call := specd.nodes_with_id(reachable)[_]
	specd.definition_node(call)
	re_match(input.parameters.sshPattern, call.text)
	not indirect(call)
	msg := sprintf("ssh invocation %s reachable from %s reaches the guest directly: it carries no ProxyCommand or one that only dials the guest, and a guest is reached only through a relay -- any transport or tunnel passes", [call.qualifiedName, driver.path])
	details := specd.location(call.file, specd.node_match_line(call, input.parameters.sshPattern))
}

# indirect: the ssh carries a ProxyCommand that is not a direct dial. Any
# transport or tunnel counts -- the org relay, fedproxy, websocat, iroh,
# dumbpipe, cloudflared or a transport the classifier could not name.
indirect(call) {
	node := specd.nodes_with_id(specd.closure_from({call.id}, input.parameters.edgeKinds))[_]
	re_match(input.parameters.proxyCommandPattern, node.text)
	not direct_dialer(node.text)
}

direct_dialer(text) {
	pattern := input.parameters.directProxyPatterns[_]
	re_match(pattern, text)
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
