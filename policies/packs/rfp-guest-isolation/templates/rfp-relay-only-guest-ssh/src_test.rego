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

# A relay is anything that is not a direct connection, so a named transport the
# binding's vocabulary does not list is still a relay.
test_no_violation_when_the_proxy_is_a_transport_the_vocabulary_does_not_name {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "chisel client https://relay 22"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_no_violation_when_the_proxy_names_iroh {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "iroh connect --node <ticket>"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_no_violation_when_the_proxy_names_dumbpipe_connect_tcp {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "dumbpipe connect-tcp <ticket>"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

# An unnamed transport -- an expression the model could not resolve, or a
# transport object it could not read -- is not a direct connection, so it passes.
test_no_violation_when_the_proxy_command_is_an_unresolved_expression {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "${transport.proxyCommand()}"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_no_violation_when_a_transport_object_carries_the_ssh {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "transport": {"proxyCommand": ["websocat", "--binary"]}}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_violation_when_the_proxy_is_nc_to_the_guest {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "nc guest 2222"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_the_proxy_is_ncat_by_absolute_path {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "/usr/bin/ncat %h %p"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_the_proxy_is_socat_over_tcp {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "socat - TCP:guest:22"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_the_proxy_uses_dev_tcp {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "sh -c 'exec 3<>/dev/tcp/guest/22'"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

test_violation_when_the_proxy_dials_the_ssh_own_target {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "host": "10.0.0.7", "proxyCommand": "nc 10.0.0.7 2222"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
}

# A jump host is not the guest: the ssh's connection goes to the hop, and the
# hop is what dials the guest's address.
test_no_violation_when_the_proxy_jumps_through_a_host {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "ssh -W %h:%p jumphost.example.org"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_no_violation_when_a_socks_hop_carries_the_ssh {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "nc -X 5 -x 127.0.0.1:1080 %h %p"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_no_violation_when_a_jump_option_names_a_host {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyJump": "relay.example.org"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

test_no_violation_when_an_ssh_config_carries_the_proxy {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "sshConfig": "./test/ssh_config"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

# An ssh to a host the binding knows is not the guest is left alone.
test_no_violation_for_an_ssh_to_a_host_that_is_not_the_guest {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "host": "github.com"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "unknown", "initiator": "requester", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

# A proxy command the walk could not resolve is still a relay, whatever channel
# the flow the model built for it carries.
test_no_violation_when_the_relay_channel_carries_an_unresolved_proxy {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh", "proxyCommand": "${transport.proxyCommand()}"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "channel": "relay", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 0
}

# The channel never exempts an ssh whose own arguments are direct: the clause
# that read the flow channel is gone (review 0006 N1).
test_violation_when_the_relay_channel_carries_an_ssh_with_no_proxy {
	ssh := {"id": "ssh", "kind": "ssh.connect", "component": "requester", "attrs": {"argv0": "ssh"}, "file": "lib/requester/mod.ts", "line": 29}
	flow := {"from": "requester", "to": "guest", "initiator": "requester", "channel": "relay", "source": "observed", "evidence": ["ssh"]}
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model(roles, [ssh], [flow])
	count(violations) == 1
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
