package nodirectguestconnect

inventory := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"CodeGraph": {"x": {
	"metadata": {"name": "x", "namespace": "default"},
	"spec": {
		"repository": "x",
		"files": [{"path": "test/denied.ts"}],
		"nodes": [],
		"edges": [],
		"texts": {"test/denied.ts": "const x = Deno.connect\n"},
	},
}}}}}}

review := {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "x", "namespace": "default"}, "spec": {"repository": "x"}}}

test_violation_when_the_pattern_matches {
	call := {"parameters": {"globs": ["test/**"], "pattern": "Deno\\.connect"}, "review": review}
	violations := violation with input as call with data.inventory as inventory
	count(violations) == 1
}

test_no_violation_when_the_pattern_is_absent {
	call := {"parameters": {"globs": ["test/**"], "pattern": "a pattern that is not there"}, "review": review}
	violations := violation with input as call with data.inventory as inventory
	count(violations) == 0
}
