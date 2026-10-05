package lib.specd

inventory := {
	"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {
		"CodeGraph": {"market": {
			"metadata": {"name": "market", "namespace": "default"},
			"spec": {
				"repository": "market",
				"files": [
					{"path": "hono-bidder/mod.ts", "test": false},
					{"path": "test/bidder_test.ts", "test": true},
				],
				"nodes": [
					{"id": "fn:emit", "name": "emitOnNetwork", "qualifiedName": "Bidder.emitOnNetwork", "file": "hono-bidder/mod.ts", "context": "hono-bidder", "text": "emitOnNetwork()\n"},
					{"id": "fn:getNodeId", "name": "getNodeId", "qualifiedName": "Provider.getNodeId", "file": "hono-bidder/mod.ts", "context": "hono-bidder", "text": "getNodeId(providerId)\n"},
					{"id": "fn:report", "name": "onNetworkReport", "qualifiedName": "report.onNetworkReport", "file": "lib/report.ts", "context": "lib", "text": "report the ticket\n"},
				],
				"edges": [
					{"source": "fn:emit", "target": "fn:getNodeId", "kind": "calls", "line": 12},
					{"source": "fn:report", "target": "fn:emit", "kind": "calls", "line": 4},
					{"source": "fn:emit", "target": "fn:report", "kind": "references", "line": 5},
				],
				"texts": {"hono-bidder/mod.ts": "const x = 1\nemitOnNetwork()\nfetch(guestAddress)\n"},
			},
		}},
		"Repository": {"market": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"branch": "main"}}},
		"SystemContext": {"hono-bidder": {
			"metadata": {"name": "hono-bidder", "namespace": "default"},
			"spec": {"repository": "market", "requirements": [{"id": "r.relay-only", "level": "MUST", "text": "ssh over the relay"}]},
		}},
	}}},
}

code_graph_input := {"review": {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"repository": "market"}}}}

context_input := {"review": {"kind": {"kind": "SystemContext"}, "object": {"metadata": {"name": "hono-bidder", "namespace": "default"}, "spec": {"repository": "market"}}}}

test_code_graph_is_the_repository_graph {
	graph := code_graph with input as code_graph_input with data.inventory as inventory
	graph.spec.repository == "market"
}

test_code_graph_resolves_from_a_context_review {
	graph := code_graph with input as context_input with data.inventory as inventory
	graph.spec.repository == "market"
}

test_repository_and_contexts {
	repo := repository with input as context_input with data.inventory as inventory
	repo.spec.branch == "main"
	all := contexts with input as context_input with data.inventory as inventory
	count(all) == 1
	one := context("hono-bidder") with input as context_input with data.inventory as inventory
	one.spec.repository == "market"
}

test_requirement_finds_the_requirement {
	req := requirement("hono-bidder", "r.relay-only") with input as context_input with data.inventory as inventory
	req.level == "MUST"
}

test_files_matching_globs {
	files := files_matching(["hono-*/**"]) with input as code_graph_input with data.inventory as inventory
	count(files) == 1
}

test_tests_matching_selects_test_files {
	files := tests_matching(["**/*_test.ts"]) with input as code_graph_input with data.inventory as inventory
	count(files) == 1
}

test_nodes_in_files_and_contexts {
	nodes := nodes_in_files(["hono-bidder/mod.ts"]) with input as code_graph_input with data.inventory as inventory
	count(nodes) == 2
	by_context := nodes_in_context("lib") with input as code_graph_input with data.inventory as inventory
	count(by_context) == 1
}

test_nodes_named_and_qualified {
	named := nodes_named(`^get.*`) with input as code_graph_input with data.inventory as inventory
	count(named) == 1
	qualified := nodes_qualified(`^Bidder\.`) with input as code_graph_input with data.inventory as inventory
	count(qualified) == 1
}

test_calls_and_callers {
	out := calls_from("fn:emit") with input as code_graph_input with data.inventory as inventory
	out == ["fn:getNodeId"]
	in := callers_of("fn:emit") with input as code_graph_input with data.inventory as inventory
	in == ["fn:report"]
}

test_reachable_follows_only_the_named_edge_kinds {
	reached := reachable_from(["fn:emit"], ["calls"]) with input as code_graph_input with data.inventory as inventory
	reached["fn:emit"]
	reached["fn:getNodeId"]
	not reached["fn:report"]
	referenced := reachable_from(["fn:emit"], ["references"]) with input as code_graph_input with data.inventory as inventory
	referenced["fn:report"]
	not referenced["fn:getNodeId"]
}

test_reaching_walks_backwards {
	reached := reaching(["fn:emit"], ["calls"]) with input as code_graph_input with data.inventory as inventory
	reached["fn:report"]
}

test_lines_matching {
	lines := lines_matching("hono-bidder/mod.ts", "fetch") with input as code_graph_input with data.inventory as inventory
	count(lines) == 1
	lines[0].line == 3
}

test_node_text_matches {
	node := {"text": "dumbpipe connect\n"}
	node_text_matches(node, "dumbpipe") with input as code_graph_input with data.inventory as inventory
}

test_violation_and_location_shape {
	v := violation("bad", location("a.ts", 7)) with input as code_graph_input
	v.msg == "bad"
	v.details.file == "a.ts"
	v.details.line == 7
}

test_missing_inventory_is_empty_not_undefined {
	graph := code_graph with input as code_graph_input with data.inventory as {}
	count(graph.spec.files) == 0
	all := contexts with input as code_graph_input with data.inventory as {}
	count(all) == 0
}
