package rfprelayonlyguestssh

import data.lib.specd

guest_role := object.get(input.parameters, "guestRole", "guest")

tester_role := object.get(input.parameters, "testRole", "test")

relay_class := object.get(input.parameters, "relayClass", "relay")

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	effect.kind == "ssh.connect"
	not specd.component_in_role(effect.component, guest_role)
	not relay_carried(effect.id)
	direct_ssh(effect)
	msg := sprintf("ssh from %s reaches the guest directly: %s:%d; a guest is reached only through a relay, so give the ssh a ProxyCommand that goes through any transport -- the org relay, fedproxy, websocat, iroh, dumbpipe, cloudflared or an unnamed tunnel",
		[effect.component, effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "file": effect.file, "line": object.get(effect, "line", 0)}
}

# direct_ssh: an ssh with no ProxyCommand at all, or one whose ProxyCommand
# only dials the guest's address. A relay is anything that is not a direct
# connection, so any other proxy command -- a named transport, an unresolved
# expression, a transport object the model could not name -- is a relay. The
# vocabulary channels/relay is an optional naming hint for messages and for the
# model's flow channel, never a requirement.
direct_ssh(effect) {
	proxy_command(effect) == ""
	not names_a_transport(effect)
}

direct_ssh(effect) {
	direct_dialer(proxy_command(effect))
}

proxy_command(effect) := object.get(object.get(effect, "attrs", {}), "proxyCommand", "")

names_a_transport(effect) {
	key := object.keys(object.get(effect, "attrs", {}))[_]
	contains(lower(key), "transport")
}

direct_dialer(proxy) {
	regex.match("(^|[/[:space:]])(nc|ncat|netcat)([[:space:]]|$)", lower(proxy))
}

direct_dialer(proxy) {
	contains(lower(proxy), "socat")
	contains(lower(proxy), "tcp:")
}

direct_dialer(proxy) {
	contains(proxy, "/dev/tcp/")
}

direct_dialer(proxy) {
	regex.match("(^|[[:space:]])-[WJ][[:space:]]", proxy)
}

relay_carried(id) {
	flow := specd.model_flows[_]
	flow.evidence[_] == id
	flow.channel == relay_class
}

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	effect.kind == "net.dial"
	specd.component_in_role(effect.component, tester_role)
	not relay_carried(effect.id)
	flow := specd.model_flows[_]
	flow.evidence[_] == effect.id
	specd.acted_on_in_role(flow, guest_role)
	msg := sprintf("the test dials the guest directly: %s:%d", [effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "flow": flow, "file": effect.file, "line": object.get(effect, "line", 0)}
}
