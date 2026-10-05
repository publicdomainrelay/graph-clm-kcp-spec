package rfprelayonlyguestssh

parameters := {
	"guestRole": "guest",
	"testRole": "test",
	"relayClass": "relay",
}

review := {"kind": {"kind": "ArchitectureModel"}, "object": {"metadata": {"name": "pack-fixture", "namespace": "default"}, "spec": {"repository": "pack-fixture"}}}

model(components, effects, flows) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"ArchitectureModel": {"pack-fixture": {
	"metadata": {"name": "pack-fixture", "namespace": "default"},
	"spec": {
		"repository": "pack-fixture",
		"roles": ["guest", "requester", "test"],
		"vocabulary": {"channels": {"relay": ["websocat", "dumbpipe"]}},
		"components": components,
		"effects": effects,
		"flows": flows,
		"triggers": [],
	},
}}}}}}

roles := [
	{"name": "requester", "roles": ["requester"], "source": "observed"},
	{"name": "test", "roles": ["test"], "source": "observed"},
	{"name": "guest", "roles": ["guest"], "source": "observed"},
]

relayed_ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "websocat --binary"}, "file": "lib/requester-xrpc/mod.ts", "line": 691}

relayed_flow := {"from": "requester", "to": "guest", "initiator": "requester", "channel": "relay", "source": "observed", "evidence": ["ssh"]}

test_no_violation_when_the_ssh_is_carried_by_the_relay {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [relayed_ssh], [relayed_flow])
	count(violations) == 0
}

# A proxy that names no relay term is not a relay: a bare nc, an -W jump host,
# or a tunnel the binding's vocabulary does not name is denied, and the binding
# names an extra term when the tunnel is a relay.
test_violation_when_the_proxy_is_a_transport_the_vocabulary_does_not_name {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "chisel client https://relay 22"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_the_proxy_command_names_no_relay_term {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "${transport.proxyCommand()}"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_the_proxy_is_nc_to_the_guest {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "nc 10.0.0.7 2222"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

# A proxy command the walk could not resolve is still a relay when the flow the
# model built for the ssh is carried by the relay channel.
test_no_violation_when_the_relay_carries_the_unresolved_proxy {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "${transport.proxyCommand()}"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "channel": "relay", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_violation_when_the_ssh_carries_no_proxy_command {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_a_test_body_spawns_ssh_as_a_file_level_effect {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "test", "attrs": {"argv0": "ssh"}, "file": "test/bidder_test.ts", "line": 42, "node": "file:test/bidder_test.ts"}
	flow := {"from": "test", "to": "unknown", "initiator": "test", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_a_test_sshes_directly_to_the_guest {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "test", "attrs": {"argv0": "ssh", "target": "root@10.0.0.7"}, "file": "test/bidder_test.ts", "line": 40}
	flow := {"from": "test", "to": "guest", "initiator": "test", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_no_violation_for_an_ssh_inside_the_guest {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "guest", "attrs": {"argv0": "ssh"}, "file": "lib/cloud-init/mod.ts", "line": 88}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [])
	count(violations) == 0
}

test_violation_when_a_test_dials_the_guest {
	dial := {"id": "dial", "kind": "net.dial", "component": "test", "attrs": {"target": "10.0.0.5:22"}, "file": "test/bidder_test.ts", "line": 20}
	flow := {"from": "test", "to": "guest", "initiator": "test", "source": "observed", "evidence": ["dial"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [dial], [flow])
	count(violations) == 1
}

test_no_violation_when_a_test_dials_another_role {
	dial := {"id": "dial", "kind": "net.dial", "component": "test", "attrs": {"target": "127.0.0.1:8000"}, "file": "test/bidder_test.ts", "line": 20}
	flow := {"from": "test", "to": "requester", "initiator": "test", "source": "observed", "evidence": ["dial"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [dial], [flow])
	count(violations) == 0
}
