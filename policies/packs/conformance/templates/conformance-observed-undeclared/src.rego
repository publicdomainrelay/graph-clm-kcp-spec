package conformanceobservedundeclared

import data.lib.specd

violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	specd.observed(flow)
	not specd.forbidden(flow)
	not declared_shape(flow)
	msg := sprintf("the observed flow %s -> %s (initiator %s, purpose %s) is not declared by any interaction",
		[flow.from, flow.to, field(flow, "initiator"), field(flow, "purpose")])
	details := {
		"from": flow.from,
		"to": flow.to,
		"initiator": field(flow, "initiator"),
		"channel": field(flow, "channel"),
		"purpose": field(flow, "purpose"),
		"evidence": object.get(flow, "evidence", []),
	}
}

declared_shape(flow) {
	other := specd.model_flows[_]
	specd.declared(other)
	specd.same_flow(flow, other)
}

field(obj, key) = value {
	value := object.get(obj, key, "")
}
