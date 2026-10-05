package rfphostreachin

parameters := {
	"hostRole": "host",
	"guestRole": "guest",
	"networkInfoClass": "network-info",
	"networkDiscoveryClass": "network-discovery",
	"reachInKinds": ["container.exec", "ssh.connect"],
}

review := {"kind": {"kind": "ArchitectureModel"}, "object": {"metadata": {"name": "pack-fixture", "namespace": "default"}, "spec": {"repository": "pack-fixture"}}}

model(components, effects, flows) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"ArchitectureModel": {"pack-fixture": {
	"metadata": {"name": "pack-fixture", "namespace": "default"},
	"spec": {
		"repository": "pack-fixture",
		"roles": ["guest", "host"],
		"vocabulary": {"payloads": {"network-info": ["address"]}, "purposes": {"network-discovery": ["onNetwork"]}},
		"components": components,
		"effects": effects,
		"flows": flows,
		"triggers": [],
	},
}}}}}}

roles := [{"name": "host", "roles": ["host"], "source": "observed"}, {"name": "guest", "roles": ["guest"], "source": "observed"}]

reach_in := {"id": "reachIn", "kind": "container.exec", "component": "host", "attrs": {"verb": "getNodeId"}, "file": "lib/bidder-compute/mod.ts", "line": 282}

test_violation_when_the_host_initiates_a_network_flow_to_the_guest {
	flow := {"from": "host", "to": "guest", "initiator": "host", "channel": "relay", "carries": ["network-info"], "purpose": "network-discovery", "source": "observed", "evidence": ["reachIn"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [reach_in], [flow])
	count(violations) == 1
}

test_violation_when_a_reach_in_effect_has_no_network_purpose {
	flow := {"from": "host", "to": "guest", "initiator": "host", "channel": "relay", "source": "observed", "evidence": ["reachIn"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [reach_in], [flow])
	count(violations) == 1
}

test_no_violation_when_the_guest_initiates {
	flow := {"from": "guest", "to": "host", "initiator": "guest", "channel": "relay", "carries": ["network-info"], "purpose": "network-discovery", "source": "observed", "evidence": ["reachIn"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [reach_in], [flow])
	count(violations) == 0
}

test_no_violation_when_a_host_effect_does_not_act_on_the_guest {
	flow := {"from": "host", "to": "requester", "initiator": "host", "channel": "relay", "source": "observed", "evidence": ["reachIn"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [reach_in], [flow])
	count(violations) == 0
}

test_no_violation_when_the_initiator_is_not_the_host_role {
	flow := {"from": "bidder", "to": "guest", "initiator": "bidder", "channel": "relay", "carries": ["network-info"], "source": "observed", "evidence": ["reachIn"]}
	effect := {"id": "reachIn", "kind": "container.exec", "component": "bidder", "attrs": {"verb": "getNodeId"}, "file": "lib/bidder/mod.ts", "line": 282}
	call := {"parameters": {"hostRole": "host", "guestRole": "guest"}, "review": review}
	violations := violation with input as call with data.inventory as model([{"name": "bidder", "roles": ["requester"], "source": "observed"}, {"name": "guest", "roles": ["guest"], "source": "observed"}], [effect], [flow])
	count(violations) == 0
}
