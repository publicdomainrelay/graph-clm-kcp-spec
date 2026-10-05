package relayonly

parameters := {"guestRole": "guest", "relayClass": "relay"}

review := {"kind": {"kind": "ArchitectureModel"}, "object": {"metadata": {"name": "fixture", "namespace": "default"}, "spec": {"repository": "fixture"}}}

model(effects, flows) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"ArchitectureModel": {"fixture": {
	"metadata": {"name": "fixture", "namespace": "default"},
	"spec": {
		"repository": "fixture",
		"roles": ["guest", "host"],
		"components": [
			{"name": "host", "roles": ["host"], "source": "observed"},
			{"name": "guest", "roles": ["guest"], "source": "observed"},
		],
		"effects": effects,
		"flows": flows,
		"triggers": [],
	},
}}}}}}

untunneled := {"id": "ssh1", "kind": "ssh.connect", "component": "host", "attrs": {}, "file": "host/mod.ts", "line": 4}

tunneled := {"id": "ssh2", "kind": "ssh.connect", "component": "host", "attrs": {"proxyCommand": "websocat"}, "file": "host/mod.ts", "line": 8}

relay_flow := {"from": "host", "to": "guest", "initiator": "host", "channel": "relay", "source": "observed", "evidence": ["ssh1"]}

test_violation_when_no_flow_carries_the_ssh {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model([untunneled], [])
	count(violations) == 1
}

test_no_violation_when_a_relay_flow_carries_the_ssh {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model([untunneled], [relay_flow])
	count(violations) == 0
}

test_no_violation_when_the_proxy_command_names_the_tunnel {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model([tunneled], [])
	count(violations) == 0
}
