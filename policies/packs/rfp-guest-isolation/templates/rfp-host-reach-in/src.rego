package rfphostreachin

import data.lib.specd

host_role := object.get(input.parameters, "hostRole", "host")

guest_role := object.get(input.parameters, "guestRole", "guest")

network_info_class := object.get(input.parameters, "networkInfoClass", "network-info")

network_discovery_class := object.get(input.parameters, "networkDiscoveryClass", "network-discovery")

default_reach_in_kinds := ["container.exec", "ssh.connect"]

violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	specd.initiator_in_role(flow, host_role)
	specd.acted_on_in_role(flow, guest_role)
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
	specd.acted_on_in_role(flow, guest_role)
	not reported_as_flow(flow)
	msg := sprintf("the host reaches into the guest with %s: %s:%d",
		[effect.kind, effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "flow": flow_details(flow, effect.file, object.get(effect, "line", 0)), "file": effect.file, "line": object.get(effect, "line", 0)}
}

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
	specd.acted_on_in_role(flow, guest_role)
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
