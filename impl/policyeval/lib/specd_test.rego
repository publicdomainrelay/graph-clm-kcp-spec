package lib.specd

inventory := {
	"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {
		"CodeGraph": {"market": {
			"metadata": {"name": "market", "namespace": "default"},
			"spec": {
				"repository": "market",
				"files": [
					{"path": "hono-bidder/mod.ts", "test": false},
					{"path": "lib/report.ts", "test": false},
					{"path": "test/bidder_test.ts", "test": true},
				],
				"nodes": [
					{"id": "fn:emit", "name": "emitOnNetwork", "qualifiedName": "Bidder.emitOnNetwork", "file": "hono-bidder/mod.ts", "context": "hono-bidder", "text": "emitOnNetwork()\n"},
					{"id": "fn:getNodeId", "name": "getNodeId", "qualifiedName": "Provider.getNodeId", "file": "hono-bidder/mod.ts", "context": "hono-bidder", "text": "getNodeId(providerId)\n"},
					{"id": "fn:report", "name": "onNetworkReport", "qualifiedName": "report.onNetworkReport", "file": "lib/report.ts", "context": "lib", "text": "report the ticket\n"},
					{"id": "file:test/other_test.ts", "kind": "file", "name": "other_test.ts", "qualifiedName": "other_test.ts", "file": "test/other_test.ts", "text": ""},
				],
				"edges": [
					{"source": "fn:emit", "target": "fn:getNodeId", "kind": "calls", "line": 12},
					{"source": "fn:report", "target": "fn:emit", "kind": "calls", "line": 4},
					{"source": "fn:emit", "target": "fn:report", "kind": "references", "line": 5},
				],
				"texts": {
					"hono-bidder/mod.ts": "const x = 1\nemitOnNetwork()\nfetch(guestAddress)\n",
					"test/bidder_test.ts": "Deno.test(\"drives the contract\", () => { runComputeContract() })\n",
				},
				"effects": [
					{"id": "e1", "kind": "ssh.connect", "component": "hono-bidder", "context": "hono-bidder", "attrs": {"argv0": "ssh", "proxyCommand": "websocat", "target": "guest.internal"}, "file": "hono-bidder/mod.ts", "line": 40, "node": "fn:ssh"},
					{"id": "e2", "kind": "event.emit", "component": "hono-bidder", "context": "hono-bidder", "attrs": {"nsid": "com.example.onNetwork"}, "file": "hono-bidder/mod.ts", "line": 44, "node": "fn:emit"},
					{"id": "e3", "kind": "net.dial", "component": "lib", "context": "lib", "attrs": {"target": "10.0.0.5:22"}, "file": "lib/report.ts", "line": 9, "node": "fn:report"},
				],
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

test_closure_includes_an_isolated_root {
	reached := closure_from(["file:test/other_test.ts"], ["calls"]) with input as code_graph_input with data.inventory as inventory
	reached["file:test/other_test.ts"]
	back := closure_reaching(["file:test/other_test.ts"], ["calls"]) with input as code_graph_input with data.inventory as inventory
	back["file:test/other_test.ts"]
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

test_tests_matching_text_selects_driving_tests {
	files := tests_matching_text(["**/*_test.ts"], "Deno\\.test") with input as code_graph_input with data.inventory as inventory
	count(files) == 1
}

test_node_lookup_and_id_selection {
	one := node("fn:emit") with input as code_graph_input with data.inventory as inventory
	one.name == "emitOnNetwork"
	selected := nodes_with_id({"fn:emit", "fn:report"}) with input as code_graph_input with data.inventory as inventory
	count(selected) == 2
}

test_file_node_resolves_the_file_node {
	one := file_node("test/other_test.ts") with input as code_graph_input with data.inventory as inventory
	one.kind == "file"
	one.id == "file:test/other_test.ts"
}

test_nodes_identified_matches_name_text_and_qualified {
	by_name := nodes_identified("onNetworkReport") with input as code_graph_input with data.inventory as inventory
	count(by_name) == 1
	by_text := nodes_identified("getNodeId") with input as code_graph_input with data.inventory as inventory
	count(by_text) == 1
	by_qualified := nodes_identified("^report\\.") with input as code_graph_input with data.inventory as inventory
	count(by_qualified) == 1
}

test_nodes_matching_text_and_globs {
	all := nodes_matching_text("getNodeId") with input as code_graph_input with data.inventory as inventory
	count(all) == 1
	scoped := nodes_matching_globs(["lib/**"], "report") with input as code_graph_input with data.inventory as inventory
	count(scoped) == 1
}

test_nodes_reachable_from_matches_text {
	found := nodes_reachable_from(["fn:emit"], ["calls"], "getNodeId") with input as code_graph_input with data.inventory as inventory
	count(found) == 1
}

test_node_match_line_is_the_real_line {
	item := {"id": "fn:x", "file": "a.ts", "startLine": 10, "text": "one\ntwo\nthree\ngetNodeId(p)\n"}
	line := node_match_line(item, "getNodeId\\(") with input as code_graph_input
	line == 13
}

test_first_line_is_one_based {
	line := first_line("hono-bidder/mod.ts", "fetch") with input as code_graph_input with data.inventory as inventory
	line == 3
}

test_definition_node_excludes_file_and_import {
	definition_node({"kind": "function"}) with input as code_graph_input
	not definition_node({"kind": "file"}) with input as code_graph_input
	not definition_node({"kind": "import"}) with input as code_graph_input
}

test_matches_globs {
	matches_globs(["test/**"], "test/a.ts") with input as code_graph_input
	not matches_globs(["test/**"], "lib/a.ts") with input as code_graph_input
}

test_effects_of_kind {
	effects := effects_of("ssh.connect") with input as code_graph_input with data.inventory as inventory
	count(effects) == 1
	effects[0].attrs.proxyCommand == "websocat"
	effects[0].file == "hono-bidder/mod.ts"
	effects[0].line == 40
}

test_effects_of_component_and_kind {
	emitted := effects_of_component("hono-bidder", "event.emit") with input as code_graph_input with data.inventory as inventory
	count(emitted) == 1
	emitted[0].attrs.nsid == "com.example.onNetwork"
	none := effects_of_component("lib", "event.emit") with input as code_graph_input with data.inventory as inventory
	count(none) == 0
}

test_effects_in_globs {
	inside := effects_in(["hono-bidder/**"]) with input as code_graph_input with data.inventory as inventory
	count(inside) == 2
	selected := effects_in(["lib/**"]) with input as code_graph_input with data.inventory as inventory
	count(selected) == 1
	selected[0].kind == "net.dial"
	none := effects_in(["test/**"]) with input as code_graph_input with data.inventory as inventory
	count(none) == 0
}

test_effect_targets_reads_the_target_attribute {
	targets := effect_targets("net.dial") with input as code_graph_input with data.inventory as inventory
	targets == ["10.0.0.5:22"]
}

test_effects_are_empty_without_a_graph {
	none := effects_of("ssh.connect") with input as code_graph_input with data.inventory as {}
	count(none) == 0
	all := effects with input as code_graph_input with data.inventory as {}
	count(all) == 0
}

test_missing_inventory_is_empty_not_undefined {
	graph := code_graph with input as code_graph_input with data.inventory as {}
	count(graph.spec.files) == 0
	all := contexts with input as code_graph_input with data.inventory as {}
	count(all) == 0
}
