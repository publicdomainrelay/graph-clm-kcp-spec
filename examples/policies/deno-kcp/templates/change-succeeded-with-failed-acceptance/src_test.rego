package changesucceededwithfailedacceptance

parameters := {}

succeeded_with_failed_step := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "SpecChange",
	"metadata": {"name": "deploy-examples-atproto-market-s2c-834078074eaa", "namespace": "default"},
	"spec": {"systemContext": "deploy-examples", "direction": "SpecToCode"},
	"status": {
		"phase": "Succeeded",
		"acceptance": [{"name": "market-live-acceptance", "passed": false, "exitCode": 1, "durationSeconds": 284.07}],
	},
}

succeeded_clean := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "SpecChange",
	"metadata": {"name": "clean", "namespace": "default"},
	"spec": {"systemContext": "deploy-examples", "direction": "SpecToCode"},
	"status": {
		"phase": "Succeeded",
		"acceptance": [{"name": "market-live-acceptance", "passed": true, "exitCode": 0, "durationSeconds": 284.07}],
	},
}

succeeded_overridden := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "SpecChange",
	"metadata": {"name": "overridden", "namespace": "default"},
	"spec": {"systemContext": "deploy-examples", "direction": "SpecToCode"},
	"status": {
		"phase": "Succeeded",
		"acceptance": [{
			"name": "market-live-acceptance",
			"passed": false,
			"exitCode": 1,
			"durationSeconds": 284.07,
			"overridden": true,
			"overrideBy": "operator",
			"overrideReason": "the gate itself is what this change fixes",
		}],
	},
}

failed_phase := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "SpecChange",
	"metadata": {"name": "failed", "namespace": "default"},
	"spec": {"systemContext": "deploy-examples", "direction": "SpecToCode"},
	"status": {
		"phase": "Failed",
		"acceptance": [{"name": "market-live-acceptance", "passed": false, "exitCode": 1, "durationSeconds": 284.07}],
	},
}

test_violation_when_succeeded_with_a_failed_step {
	call := {"parameters": parameters, "review": {"kind": {"kind": "SpecChange"}, "object": succeeded_with_failed_step}}
	violations := violation with input as call
	count(violations) == 1
}

test_no_violation_when_every_step_passed {
	call := {"parameters": parameters, "review": {"kind": {"kind": "SpecChange"}, "object": succeeded_clean}}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_failure_was_overridden {
	call := {"parameters": parameters, "review": {"kind": {"kind": "SpecChange"}, "object": succeeded_overridden}}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_phase_is_failed {
	call := {"parameters": parameters, "review": {"kind": {"kind": "SpecChange"}, "object": failed_phase}}
	violations := violation with input as call
	count(violations) == 0
}
