package rfpguestreportsnetwork

import data.lib.specd

host_role := object.get(input.parameters, "hostRole", "host")

guest_role := object.get(input.parameters, "guestRole", "guest")

network_info_class := object.get(input.parameters, "networkInfoClass", "network-info")

network_event_class := object.get(input.parameters, "networkEventClass", "network-report")

report_route_class := object.get(input.parameters, "reportRouteClass", "report")

# The require waits for code. A repository whose specs declare no interaction
# yet is not denied at spec time: the model carries no effects until the first
# realize, and the reach-in rule is what guards the declared shape until then.
violation[specd.violation(msg, details)] {
	has_effects
	not guest_reports
	msg := sprintf("no declared or observed flow has the %s initiating toward another role carrying %s: the guest must report its own network information outbound",
		[guest_role, network_info_class])
	details := {"guestRole": guest_role, "networkInfoClass": network_info_class}
}

has_effects {
	count(specd.model_effects) > 0
}

guest_reports {
	flow := specd.model_flows[_]
	specd.initiator_in_role(flow, guest_role)
	flow.carries[_] == network_info_class
	not specd.acted_on_in_role(flow, guest_role)
	report_peer(flow)
}

report_peer(flow) {
	not configured_report_peers
}

report_peer(flow) {
	specd.acted_on_in_role(flow, input.parameters.reportPeerRoles[_])
}

configured_report_peers {
	count(input.parameters.reportPeerRoles) > 0
}

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	specd.component_in_role(effect.component, host_role)
	specd.event_class(effect, network_event_class)
	not driven_by_report_handler(effect)
	msg := sprintf("the host emits the network report %q from the provisioning lifecycle, not from the http.handle of the guest's report: %s:%d",
		[object.get(object.get(effect, "attrs", {}), "type", ""), effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "routeClass": report_route_class, "file": effect.file, "line": object.get(effect, "line", 0)}
}

driven_by_report_handler(effect) {
	trigger := specd.model_triggers[_]
	trigger.to == effect.id
	handler := specd.model_effect(trigger.from)
	handler.kind == "http.handle"
	specd.route_class(handler, report_route_class)
}

# The emitted payload must come from the guest's report, not from a source the
# host reads itself. Where the model can show a host source on the emitter's
# call chain, the emission is denied whatever drives it; where it cannot, the
# trigger check above is all the rule can read (recorded in docs/policies.md).
violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	specd.component_in_role(effect.component, host_role)
	specd.event_class(effect, network_event_class)
	host_source_feeds(effect)
	msg := sprintf("the host builds the network report %q from a source it reads itself, not from the guest's report: %s:%d",
		[object.get(object.get(effect, "attrs", {}), "type", ""), effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "file": effect.file, "line": object.get(effect, "line", 0)}
}

host_source_feeds(effect) {
	ancestors := specd.closure_reaching({effect.node}, input.parameters.edgeKinds)
	reader := specd.model_effects[_]
	reader.kind == "file.read"
	ancestors[reader.node]
}
