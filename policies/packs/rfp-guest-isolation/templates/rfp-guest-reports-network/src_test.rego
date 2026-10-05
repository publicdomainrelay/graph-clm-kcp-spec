package rfpguestreportsnetwork

parameters := {
	"hostRole": "host",
	"guestRole": "guest",
	"networkInfoClass": "network-info",
	"networkEventClass": "network-report",
	"reportRouteClass": "report",
}

review := {"kind": {"kind": "ArchitectureModel"}, "object": {"metadata": {"name": "pack-fixture", "namespace": "default"}, "spec": {"repository": "pack-fixture"}}}

model(components, effects, flows, triggers) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"ArchitectureModel": {"pack-fixture": {
	"metadata": {"name": "pack-fixture", "namespace": "default"},
	"spec": {
		"repository": "pack-fixture",
		"roles": ["guest", "host"],
		"vocabulary": {
			"events": {"network-report": ["COMPUTE_EVENTS_VM_ONNETWORK_NSID"]},
			"payloads": {"network-info": ["address"]},
			"routes": {"report": ["/v1/on-network"]},
		},
		"components": components,
		"effects": effects,
		"flows": flows,
		"triggers": triggers,
	},
}}}}}}

roles := [{"name": "host", "roles": ["host"], "source": "observed"}, {"name": "guest", "roles": ["guest"], "source": "observed"}]

report := {"id": "report", "kind": "http.request", "component": "guest", "attrs": {"argv0": "curl"}, "file": "lib/cloud-init/mod.ts", "line": 449}

handler := {"id": "handler", "kind": "http.handle", "component": "host", "attrs": {"method": "POST", "path": "/v1/on-network"}, "file": "lib/market-bidder/mod.ts", "line": 475}

lifecycle := {"id": "lifecycle", "kind": "proc.exec", "component": "host", "attrs": {"argv0": "sh"}, "file": "hono-bidder/mod.ts", "line": 165}

emit := {"id": "emit", "kind": "event.emit", "component": "host", "attrs": {"type": "COMPUTE_EVENTS_VM_ONNETWORK_NSID"}, "file": "lib/market-bidder/mod.ts", "line": 490}

report_flow := {"from": "guest", "to": "host", "initiator": "guest", "channel": "relay", "carries": ["network-info"], "purpose": "network-discovery", "source": "observed", "evidence": ["report"]}

test_violation_when_no_guest_report_flow_exists {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [handler, emit], [], [{"from": "handler", "to": "emit"}])
	count(violations) == 1
}

test_no_violation_at_spec_time_when_the_model_carries_no_effects {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [], [], [])
	count(violations) == 0
}

test_violation_when_the_emit_is_driven_by_the_lifecycle {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [report, handler, lifecycle, emit], [report_flow], [{"from": "lifecycle", "to": "emit"}])
	count(violations) == 1
}

test_violation_when_the_emit_is_driven_by_another_route {
	other := {"id": "other", "kind": "http.handle", "component": "host", "attrs": {"method": "GET", "path": "/health"}, "file": "hono-bidder/mod.ts", "line": 567}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [report, other, emit], [report_flow], [{"from": "other", "to": "emit"}])
	count(violations) == 1
}

test_no_violation_when_the_report_handler_drives_the_emit {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [report, handler, emit], [report_flow], [{"from": "handler", "to": "emit"}])
	count(violations) == 0
}

test_no_violation_when_the_emit_is_not_of_the_network_report_class {
	other_emit := {"id": "emit", "kind": "event.emit", "component": "host", "attrs": {"type": "MARKET_BID_NSID"}, "file": "hono-bidder/mod.ts", "line": 364}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [report, handler, other_emit], [report_flow], [])
	count(violations) == 0
}

test_no_violation_when_the_emitted_type_only_contains_the_term {
	# A term is a whole token: `PREFIX_COMPUTE_EVENTS_VM_ONNETWORK_NSID_SUFFIX`
	# is not the event class the vocabulary named.
	near := {"id": "emit", "kind": "event.emit", "component": "host", "attrs": {"type": "PREFIX_COMPUTE_EVENTS_VM_ONNETWORK_NSID_SUFFIX"}, "file": "hono-bidder/mod.ts", "line": 364}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [report, handler, near], [report_flow], [])
	count(violations) == 0
}

test_report_peer_roles_narrow_the_flow {
	call := {"parameters": {"guestRole": "guest", "networkInfoClass": "network-info", "networkEventClass": "network-report", "reportRouteClass": "report", "reportPeerRoles": ["host"]}, "review": review}
	violations := violation with input as call with data.inventory as model(roles, [report, handler], [report_flow], [])
	count(violations) == 0
}

test_report_peer_roles_reject_a_report_to_another_role {
	elsewhere := {"from": "guest", "to": "relay", "initiator": "guest", "channel": "relay", "carries": ["network-info"], "purpose": "network-discovery", "source": "observed", "evidence": ["report"]}
	call := {"parameters": {"hostRole": "host", "guestRole": "guest", "networkInfoClass": "network-info", "networkEventClass": "network-report", "reportRouteClass": "report", "reportPeerRoles": ["host"]}, "review": review}
	violations := violation with input as call with data.inventory as model(roles, [report, handler], [elsewhere], [])
	count(violations) == 1
}
