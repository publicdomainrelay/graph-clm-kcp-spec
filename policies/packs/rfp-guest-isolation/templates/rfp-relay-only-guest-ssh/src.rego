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
	not proxied(effect)
	msg := sprintf("ssh from %s goes through no tunnel and no %s-channel flow: %s:%d; a guest is reached only through the relay",
		[effect.component, relay_class, effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "relayClass": relay_class, "file": effect.file, "line": object.get(effect, "line", 0)}
}

# proxied keeps an ssh whose proxy command names a transport the binding calls
# a relay out of the report. A proxy command that names no relay term -- a bare
# nc, an -W jump host, a tunnel the vocabulary does not know -- is not a relay,
# so the ssh is denied.
proxied(effect) {
	proxy := object.get(object.get(effect, "attrs", {}), "proxyCommand", "")
	proxy != ""
	term := specd.vocabulary_terms("channels", relay_class)[_]
	contains(lower(proxy), lower(term))
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
	flow := specd.model_flows[_]
	flow.evidence[_] == effect.id
	specd.acted_on_in_role(flow, guest_role)
	msg := sprintf("the test dials the guest directly: %s:%d", [effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "flow": flow, "file": effect.file, "line": object.get(effect, "line", 0)}
}
