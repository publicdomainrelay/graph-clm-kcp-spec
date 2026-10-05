package relayonly

import data.lib.specd

guest_role := object.get(input.parameters, "guestRole", "guest")

relay_class := object.get(input.parameters, "relayClass", "relay")

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	effect.kind == "ssh.connect"
	not specd.component_in_role(effect.component, guest_role)
	not relay_carried(effect.id)
	not proxied(effect)
	msg := sprintf("ssh from %s goes through no %s-channel flow: %s:%d",
		[effect.component, relay_class, effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "relayClass": relay_class, "file": effect.file, "line": object.get(effect, "line", 0)}
}

relay_carried(id) {
	flow := specd.model_flows[_]
	flow.evidence[_] == id
	flow.channel == relay_class
}

proxied(effect) {
	proxy := object.get(object.get(effect, "attrs", {}), "proxyCommand", "")
	proxy != ""
}
