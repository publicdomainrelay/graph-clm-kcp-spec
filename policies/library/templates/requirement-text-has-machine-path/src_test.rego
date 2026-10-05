package requirementtexthasmachinepath

parameters := {"machinePathPatterns": ["/(?:home|Users|root|tmp|mnt|opt|srv)/[A-Za-z0-9._@+-]+"]}

delta_with_machine_path := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "SpecChange",
	"metadata": {"name": "c1", "namespace": "default"},
	"spec": {
		"systemContext": "deploy-examples",
		"direction": "SpecToCode",
		"delta": {
			"requirements": [{
				"op": "changed",
				"id": "r.bidder-pod",
				"fields": ["text"],
				"from": {"id": "r.bidder-pod", "level": "MUST", "text": "apply.sh rewrites the prefix"},
				"to": {"id": "r.bidder-pod", "level": "MUST", "text": "apply.sh rewrites the /home/johnandersen777/src/publicdomainrelay-kcp prefix"},
			}],
		},
	},
}

delta_without_machine_path := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "SpecChange",
	"metadata": {"name": "c2", "namespace": "default"},
	"spec": {
		"systemContext": "deploy-examples",
		"direction": "SpecToCode",
		"delta": {
			"requirements": [{
				"op": "changed",
				"id": "r.bidder-pod",
				"fields": ["text"],
				"from": {"id": "r.bidder-pod", "level": "MUST", "text": "apply.sh rewrites the prefix"},
				"to": {"id": "r.bidder-pod", "level": "MUST", "text": "apply.sh rewrites the repository prefix"},
			}],
		},
	},
}

delta_without_requirements := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "SpecChange",
	"metadata": {"name": "c3", "namespace": "default"},
	"spec": {"systemContext": "deploy-examples", "direction": "CodeToSpec", "delta": {}},
}

test_violation_when_a_requirement_text_names_a_machine_path {
	call := {"parameters": parameters, "review": {"kind": {"kind": "SpecChange"}, "object": delta_with_machine_path}}
	violations := violation with input as call
	count(violations) == 1
}

test_no_violation_when_the_text_is_repo_relative {
	call := {"parameters": parameters, "review": {"kind": {"kind": "SpecChange"}, "object": delta_without_machine_path}}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_delta_carries_no_requirements {
	call := {"parameters": parameters, "review": {"kind": {"kind": "SpecChange"}, "object": delta_without_requirements}}
	violations := violation with input as call
	count(violations) == 0
}
