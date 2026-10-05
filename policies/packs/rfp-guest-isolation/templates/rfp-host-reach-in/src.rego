package rfphostreachin

import data.lib.specd

host_role := object.get(input.parameters, "hostRole", "host")

guest_role := object.get(input.parameters, "guestRole", "guest")

network_info_class := object.get(input.parameters, "networkInfoClass", "network-info")

network_discovery_class := object.get(input.parameters, "networkDiscoveryClass", "network-discovery")

default_reach_in_kinds := ["container.exec", "ssh.connect", "net.dial", "http.request"]

violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	specd.initiator_in_role(flow, host_role)
	reach_in_target(flow)
	network_flow(flow)
	msg := sprintf("the host reaches into the guest: %s -> %s (initiator %s, channel %s, purpose %s, carries %v)",
		[flow.from, flow.to, object.get(flow, "initiator", ""), object.get(flow, "channel", ""), object.get(flow, "purpose", ""), object.get(flow, "carries", [])])
	details := flow_details(flow, site_file(flow), site_line(flow))
}

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	reach_in_kind(effect.kind)
	specd.component_in_role(effect.component, host_role)
	flow := specd.model_flows[_]
	flow.evidence[_] == effect.id
	specd.initiator_in_role(flow, host_role)
	reach_in_effect(flow, effect)
	not reported_as_flow(flow)
	msg := sprintf("the host reaches into the guest with %s: %s:%d",
		[effect.kind, effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "flow": flow_details(flow, effect.file, object.get(effect, "line", 0)), "file": effect.file, "line": object.get(effect, "line", 0)}
}

# reach_in_target is the default: the host acting on the guest, and the host
# acting on a target the model could not resolve. The binding names the
# exceptions -- the targets a host reaches that are known not to be the guest --
# so a new repository does not have to rediscover its own reach-in verbs.
reach_in_target(flow) {
	specd.acted_on_in_role(flow, guest_role)
}

reach_in_target(flow) {
	specd.acted_on(flow) == "unknown"
	not reach_in_exception_flow(flow)
}

reach_in_effect(flow, effect) {
	specd.acted_on_in_role(flow, guest_role)
}

reach_in_effect(flow, effect) {
	specd.acted_on(flow) == "unknown"
	not reach_in_exception(effect)
}

# A flow whose evidence is every effect the host reached with is excepted only
# when each of those effects names an excepted target: one exception must not
# hide the rest of a reach.
reach_in_exception_flow(flow) {
	count(flow.evidence) > 0
	not unexcepted_evidence(flow)
}

unexcepted_evidence(flow) {
	effect := specd.model_effect(flow.evidence[_])
	not reach_in_exception(effect)
}

reach_in_exception(effect) {
	pattern := reach_in_exception_patterns[_]
	glob.match(pattern, ["/"], effect.file)
}

reach_in_exception(effect) {
	pattern := reach_in_exception_patterns[_]
	value := object.get(effect, "attrs", {})[_]
	contains(lower(value), lower(pattern))
}

reach_in_exception_patterns := object.get(specd.model_vocabulary, "reachInExceptions", [])

reach_in_kind(kind) {
	kind == input.parameters.reachInKinds[_]
}

reach_in_kind(kind) {
	not configured_reach_in_kinds
	kind == default_reach_in_kinds[_]
}

configured_reach_in_kinds {
	count(input.parameters.reachInKinds) > 0
}

reported_as_flow(flow) {
	specd.initiator_in_role(flow, host_role)
	reach_in_target(flow)
	network_flow(flow)
}

network_flow(flow) {
	object.get(flow, "purpose", "") == network_discovery_class
}

network_flow(flow) {
	object.get(flow, "carries", [])[_] == network_info_class
}

# reach_in_evidence names where the reach-in happens: the evidence effect of
# the flow that connects to the guest, so the report points at a line.
connecting_kind("container.exec")

connecting_kind("ssh.connect")

reach_in_evidence(flow) = out {
	connecting := [effect | effect := specd.model_effect(flow.evidence[_]); connecting_kind(effect.kind)]
	count(connecting) > 0
	out := connecting[0]
}

reach_in_evidence(flow) = out {
	connecting := [effect | effect := specd.model_effect(flow.evidence[_]); connecting_kind(effect.kind)]
	count(connecting) == 0
	all := [effect | effect := specd.model_effect(flow.evidence[_])]
	count(all) > 0
	out := all[0]
}

site_file(flow) = file {
	effect := reach_in_evidence(flow)
	file := effect.file
}

site_line(flow) = line {
	effect := reach_in_evidence(flow)
	line := object.get(effect, "line", 0)
}

# A declared flow carries no evidence until the first realize. The site is
# empty then, and the rule still has to fire: a spec that plans the reach-in is
# denied before any code exists.
site_file(flow) = "" {
	not reach_in_evidence(flow)
}

site_line(flow) = 0 {
	not reach_in_evidence(flow)
}

flow_details(flow, file, line) = details {
	details := {
		"file": file,
		"line": line,
		"from": flow.from,
		"to": flow.to,
		"initiator": object.get(flow, "initiator", ""),
		"channel": object.get(flow, "channel", ""),
		"purpose": object.get(flow, "purpose", ""),
		"carries": object.get(flow, "carries", []),
		"source": object.get(flow, "source", ""),
		"evidence": object.get(flow, "evidence", []),
	}
}
