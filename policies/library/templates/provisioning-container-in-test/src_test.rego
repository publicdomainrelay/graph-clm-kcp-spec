package provisioningcontainerintest

parameters := {
	"testGlobs": ["test/**"],
	"containerPatterns": [`\b(?:docker|podman|container|nerdctl)\s+(?:run|exec)\b`],
}

code_diff(files) := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "CodeDiff",
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {"base": "aaaa", "head": "bbbb", "files": files},
}

review(object) := {"kind": {"kind": "CodeDiff"}, "object": object}

container_in_test := code_diff([{
	"path": "test/bidder_test.ts",
	"status": "modified",
	"added": [{"line": 12, "text": "  await container run --rm alpine true"}],
}])

container_in_source := code_diff([{
	"path": "lib/market-bidder/mod.ts",
	"status": "modified",
	"added": [{"line": 40, "text": "  await container exec guest apt-get install -y openssh-server"}],
}])

run_compute_contract := code_diff([{
	"path": "test/bidder_test.ts",
	"status": "modified",
	"added": [{"line": 18, "text": "  await runComputeContract(request, bidder)"}],
}])

test_violation_when_a_test_runs_a_container {
	call := {"parameters": parameters, "review": review(container_in_test)}
	violations := violation with input as call
	count(violations) == 1
}

test_no_violation_when_the_container_command_is_not_in_a_test {
	call := {"parameters": parameters, "review": review(container_in_source)}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_test_drives_the_contract {
	call := {"parameters": parameters, "review": review(run_compute_contract)}
	violations := violation with input as call
	count(violations) == 0
}

comment_in_test := code_diff([{
	"path": "test/bidder_test.ts",
	"status": "modified",
	"added": [{"line": 12, "text": "  // the old path was container run --rm alpine true"}],
}])

test_no_violation_when_the_line_is_a_comment {
	call := {"parameters": parameters, "review": review(comment_in_test)}
	violations := violation with input as call
	count(violations) == 0
}
