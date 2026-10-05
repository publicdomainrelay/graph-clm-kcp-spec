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

commented := code_diff([{
	"path": "lib/requester-xrpc/mod.ts",
	"status": "modified",
	"added": [{"line": 1185, "text": "  // the websocat ProxyCommand."}],
}])

markdown := code_diff([{
	"path": "request-vm-ssh/README.md",
	"status": "modified",
	"added": [{"line": 107, "text": "legacy websocat ProxyCommand -> xrpc-relay -> tunnel-subscriber -> sshd"}],
}])

two_patterns := code_diff([{
	"path": "lib/market-bidder/mod.ts",
	"status": "modified",
	"added": [{"line": 91, "text": "  spawn(\"socat\", [\"-x\", \"websocat --binary wss://guest\"])"}],
}])

test_no_violation_when_the_line_is_a_comment {
	call := {"parameters": parameters, "review": review(commented)}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_line_is_markdown {
	call := {"parameters": parameters, "review": review(markdown)}
	violations := violation with input as call
	count(violations) == 0
}

test_one_violation_when_several_patterns_match_one_line {
	call := {"parameters": parameters, "review": review(two_patterns)}
	violations := violation with input as call
	count(violations) == 1
}
