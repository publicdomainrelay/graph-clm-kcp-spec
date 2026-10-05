package provisioningnewguesttransport

parameters := {}

code_diff(files) := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "CodeDiff",
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {"base": "aaaa", "head": "bbbb", "files": files},
}

review(object) := {"kind": {"kind": "CodeDiff"}, "object": object}

new_transport := code_diff([{
	"path": "lib/market-bidder/mod.ts",
	"status": "modified",
	"added": [{"line": 88, "text": "  spawn(\"websocat\", [\"--binary\", `wss://${guest}/tunnel`])"}],
}])

module_transport := code_diff([{
	"path": "lib/modules/tunnel/mod.ts",
	"status": "modified",
	"added": [{"line": 12, "text": "  return { name: \"tunnel\", run: \"websocat --binary ${sink}\" } // UserDataModule"}],
}])

unrelated := code_diff([{
	"path": "lib/market-bidder/mod.ts",
	"status": "modified",
	"added": [{"line": 90, "text": "  return await fetch(`${relay}/xrpc/provision`)"}],
}])

test_violation_when_a_transport_appears_without_a_module {
	call := {"parameters": parameters, "review": review(new_transport)}
	violations := violation with input as call
	count(violations) == 1
}

test_no_violation_when_the_line_names_a_user_data_module {
	call := {"parameters": parameters, "review": review(module_transport)}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_line_introduces_no_transport {
	call := {"parameters": parameters, "review": review(unrelated)}
	violations := violation with input as call
	count(violations) == 0
}
