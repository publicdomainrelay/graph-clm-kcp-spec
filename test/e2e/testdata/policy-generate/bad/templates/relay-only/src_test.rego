package relayonly

parameters := {"guestRole": "guest", "relayClass": "relay"}

review := {"kind": {"kind": "ArchitectureModel"}, "object": {"metadata": {"name": "fixture", "namespace": "default"}, "spec": {"repository": "fixture"}}}

model(effects) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"ArchitectureModel": {"fixture": {
	"metadata": {"name": "fixture", "namespace": "default"},
	"spec": {
		"repository": "fixture",
		"roles": ["guest", "host"],
		"components": [
			{"name": "host", "roles": ["host"], "source": "observed"},
			{"name": "guest", "roles": ["guest"], "source": "observed"},
		],
		"effects": effects,
		"flows": [],
		"triggers": [],
	},
}}}}}}

untunneled := {"id": "ssh1", "kind": "ssh.connect", "component": "host", "attrs": {}, "file": "host/mod.ts", "line": 4}

test_no_violation_for_an_untunneled_ssh {
	violations := violation with input as {"parameters": parameters, "review": review} with data.inventory as model([untunneled])
	count(violations) == 0
}
