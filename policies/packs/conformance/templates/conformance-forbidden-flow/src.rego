package conformanceforbiddenflow

import data.lib.specd

violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	specd.forbidden_matches(flow)
	marker := [candidate | candidate := specd.model_flows[_]; specd.forbidden(candidate); specd.same_flow(flow, candidate)][0]
	msg := sprintf("the flow %s -> %s (initiator %s, purpose %s) matches the declared must-never %s -> %s",
		[flow.from, flow.to, object.get(flow, "initiator", ""), object.get(flow, "purpose", ""), marker.from, marker.to])
	details := {
		"from": flow.from,
		"to": flow.to,
		"initiator": object.get(flow, "initiator", ""),
		"channel": object.get(flow, "channel", ""),
		"purpose": object.get(flow, "purpose", ""),
		"marker": marker,
		"source": object.get(flow, "source", ""),
		"evidence": object.get(flow, "evidence", []),
	}
}
