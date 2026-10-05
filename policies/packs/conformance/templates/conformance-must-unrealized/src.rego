package conformancemustunrealized

import data.lib.specd

violation[specd.violation(msg, details)] {
	count(specd.architecture_model.spec.effects) > 0
	flow := specd.model_flows[_]
	specd.declared(flow)
	not specd.observed(flow)
	not specd.forbidden(flow)
	specd.flow_level(flow, "MUST")
	msg := sprintf("the declared MUST flow %s -> %s (initiator %s, purpose %s) has no observed evidence",
		[flow.from, flow.to, object.get(flow, "initiator", ""), object.get(flow, "purpose", "")])
	details := {
		"from": flow.from,
		"to": flow.to,
		"initiator": object.get(flow, "initiator", ""),
		"channel": object.get(flow, "channel", ""),
		"purpose": object.get(flow, "purpose", ""),
	}
}
