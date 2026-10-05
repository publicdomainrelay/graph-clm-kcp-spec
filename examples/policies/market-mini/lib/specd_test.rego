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

model_inventory := {
	"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {
		"Repository": {"market": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"branch": "main"}}},
		"ArchitectureModel": {"market": {
			"metadata": {"name": "market", "namespace": "default"},
			"spec": {
				"components": [
					{"name": "bidder", "roles": ["host"], "context": "bidder", "source": "both"},
					{"name": "guest", "roles": ["guest"], "context": "", "source": "observed"},
					{"name": "requester", "roles": ["requester"], "context": "requester", "source": "declared"},
				],
				"effects": [
					{"id": "m1", "kind": "container.exec", "component": "bidder", "file": "lib/bidder/mod.ts", "line": 20, "node": "fn:emit"},
				],
				"flows": [
					{"from": "guest", "to": "requester", "initiator": "guest", "channel": "relay", "carries": ["network-info"], "purpose": "network-discovery", "source": "both", "evidence": ["e9"]},
					{"from": "bidder", "to": "guest", "initiator": "bidder", "carries": ["network-info"], "purpose": "network-discovery", "source": "observed", "evidence": ["m1"]},
					{"from": "guest", "to": "bidder", "initiator": "peer", "channel": "relay", "carries": ["network-info"], "purpose": "network-report", "source": "declared", "evidence": []},
				],
				"triggers": [
					{"from": "h1", "to": "m1"},
					{"from": "l1", "to": "m1"},
				],
			},
		}},
	}}},
}

model_input := {"review": {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"repository": "market"}}}}

test_architecture_model_resolves_and_is_empty_by_default {
	model := architecture_model with input as model_input with data.inventory as model_inventory
	model.spec.components[0].name == "bidder"
	empty := architecture_model with input as model_input with data.inventory as {}
	count(empty.spec.flows) == 0
}

test_components_with_role {
	guests := components_with_role("guest") with input as model_input with data.inventory as model_inventory
	count(guests) == 1
	guests[0].name == "guest"
	hosts := components_with_role("host") with input as model_input with data.inventory as model_inventory
	count(hosts) == 1
	hosts[0].context == "bidder"
	none := components_with_role("relay") with input as model_input with data.inventory as model_inventory
	count(none) == 0
}

test_roles_of_a_component {
	roles := roles_of("guest") with input as model_input with data.inventory as model_inventory
	roles == ["guest"]
	none := roles_of("absent") with input as model_input with data.inventory as model_inventory
	count(none) == 0
}

test_flows_where_matches_role_pairs {
	flows := flows_where({"from": "guest", "to": "requester"}) with input as model_input with data.inventory as model_inventory
	count(flows) == 1
	flows[0].initiator == "guest"
}

test_flows_where_carries_accepts_a_string_or_an_array {
	byString := flows_where({"carries": "network-info"}) with input as model_input with data.inventory as model_inventory
	count(byString) == 3
	byArray := flows_where({"carries": ["network-info", "other"]}) with input as model_input with data.inventory as model_inventory
	count(byArray) == 3
	miss := flows_where({"carries": "absent"}) with input as model_input with data.inventory as model_inventory
	count(miss) == 0
}

test_flows_where_empty_filter_matches_everything {
	all := flows_where({}) with input as model_input with data.inventory as model_inventory
	count(all) == 3
	reaching := flows_where({"initiator": "bidder"}) with input as model_input with data.inventory as model_inventory
	count(reaching) == 1
}

test_triggered_by_reads_the_target_effect {
	triggers := triggered_by("m1") with input as model_input with data.inventory as model_inventory
	count(triggers) == 2
	triggers[0].from == "h1"
	none := triggered_by("absent") with input as model_input with data.inventory as model_inventory
	count(none) == 0
}

test_declared_and_observed_cover_both {
	both := [flow | flow := model_flows[_]; declared(flow); observed(flow)] with input as model_input with data.inventory as model_inventory
	count(both) == 1
	declaredOnly := [flow | flow := model_flows[_]; declared(flow); not observed(flow)] with input as model_input with data.inventory as model_inventory
	count(declaredOnly) == 1
	observedOnly := [flow | flow := model_flows[_]; observed(flow); not declared(flow)] with input as model_input with data.inventory as model_inventory
	count(observedOnly) == 1
}

marker_inventory := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {
	"ArchitectureModel": {"marker": {
		"metadata": {"name": "marker", "namespace": "default"},
		"spec": {
			"repository": "marker",
			"components": [],
			"effects": [],
			"flows": [
				{"from": "guest", "to": "host", "initiator": "host", "channel": "relay", "purpose": "network-discovery", "level": "MUST", "forbidden": true, "source": "declared"},
				{"from": "guest", "to": "host", "initiator": "host", "channel": "relay", "purpose": "network-discovery", "level": "SHOULD", "source": "observed", "evidence": ["x1"]},
				{"from": "host", "to": "requester", "initiator": "host", "channel": "relay", "purpose": "network-report", "level": "MUST", "source": "declared"},
			],
			"triggers": [],
		},
	}},
}}}}

marker_input := {"review": {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "marker", "namespace": "default"}, "spec": {"repository": "marker"}}}}

test_forbidden_reads_the_marker {
	markers := [flow | flow := model_flows[_]; forbidden(flow)] with input as marker_input with data.inventory as marker_inventory
	count(markers) == 1
	markers[0].initiator == "host"
}

test_same_flow_ignores_the_marker_and_the_source {
	marker := [flow | flow := model_flows[_]; forbidden(flow)][0] with input as marker_input with data.inventory as marker_inventory
	reaching := [flow | flow := model_flows[_]; observed(flow)][0] with input as marker_input with data.inventory as marker_inventory
	other := [flow | flow := model_flows[_]; flow.purpose == "network-report"][0] with input as marker_input with data.inventory as marker_inventory
	same_flow(marker, reaching)
	not same_flow(marker, other)
}

test_forbidden_matches_names_the_real_flow_not_the_marker {
	matched := [flow | flow := model_flows[_]; forbidden_matches(flow)] with input as marker_input with data.inventory as marker_inventory
	count(matched) == 1
	not forbidden(matched[0])
	observed(matched[0])
}

test_flow_level_reads_the_declared_level {
	must := [flow | flow := model_flows[_]; flow_level(flow, "MUST")] with input as marker_input with data.inventory as marker_inventory
	count(must) == 2
	none := [flow | flow := model_flows[_]; flow_level(flow, "MAY")] with input as marker_input with data.inventory as marker_inventory
	count(none) == 0
}

roles_inventory := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {
	"ArchitectureModel": {"roles": {
		"metadata": {"name": "roles", "namespace": "default"},
		"spec": {
			"repository": "roles",
			"roles": ["guest", "host"],
			"vocabulary": {
				"events": {"network-report": ["com.example.vm.onNetwork", "VM_ONNETWORK_NSID"]},
				"routes": {"report": ["/v1/on-network"]},
			},
			"components": [
				{"name": "host", "roles": ["host"], "source": "observed"},
				{"name": "hono-bidder", "roles": ["host"], "source": "observed"},
				{"name": "guest", "roles": ["guest"], "source": "observed"},
			],
			"effects": [
				{"id": "m1", "kind": "event.emit", "component": "host", "attrs": {"type": "VM_ONNETWORK_NSID"}, "file": "a.ts", "line": 1},
				{"id": "m2", "kind": "event.emit", "component": "host", "attrs": {"type": "ACCEPT_NSID"}, "file": "a.ts", "line": 2},
				{"id": "m3", "kind": "http.handle", "component": "host", "attrs": {"method": "POST", "path": "/v1/on-network"}, "file": "a.ts", "line": 3},
				{"id": "m4", "kind": "http.handle", "component": "host", "attrs": {"method": "GET", "path": "/health"}, "file": "a.ts", "line": 4},
			],
			"flows": [
				{"from": "host", "to": "guest", "initiator": "host", "channel": "relay", "purpose": "network-discovery", "source": "observed", "evidence": ["m1"]},
				{"from": "guest", "to": "hono-bidder", "initiator": "guest", "channel": "relay", "source": "observed", "evidence": ["m2"]},
			],
			"triggers": [],
		},
	}},
}}}}

roles_input := {"review": {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "roles", "namespace": "default"}, "spec": {"repository": "roles"}}}}

test_model_roles_and_effects_are_read_from_the_model {
	roles := model_roles with input as roles_input with data.inventory as roles_inventory
	count(roles) == 2
	emits := [effect | effect := model_effects[_]; effect.kind == "event.emit"] with input as roles_input with data.inventory as roles_inventory
	count(emits) == 2
	found := model_effect("m3") with input as roles_input with data.inventory as roles_inventory
	found.attrs.path == "/v1/on-network"
}

test_vocabulary_terms_reads_a_class {
	terms := vocabulary_terms("events", "network-report") with input as roles_input with data.inventory as roles_inventory
	count(terms) == 2
	missing := vocabulary_terms("events", "absent") with input as roles_input with data.inventory as roles_inventory
	count(missing) == 0
}

test_component_in_role_reads_roles_and_names {
	component_in_role("hono-bidder", "host") with input as roles_input with data.inventory as roles_inventory
	component_in_role("host", "host") with input as roles_input with data.inventory as roles_inventory
	not component_in_role("guest", "host") with input as roles_input with data.inventory as roles_inventory
}

test_initiator_and_acted_on_in_role {
	flow := model_flows[0] with input as roles_input with data.inventory as roles_inventory
	initiator_in_role(flow, "host") with input as roles_input with data.inventory as roles_inventory
	acted_on_in_role(flow, "guest") with input as roles_input with data.inventory as roles_inventory
	host_acted := model_flows[1] with input as roles_input with data.inventory as roles_inventory
	acted_on_in_role(host_acted, "host") with input as roles_input with data.inventory as roles_inventory
}

test_event_class_matches_the_emitted_type_case_insensitively {
	effect := model_effect("m1") with input as roles_input with data.inventory as roles_inventory
	event_class(effect, "network-report") with input as roles_input with data.inventory as roles_inventory
	other := model_effect("m2") with input as roles_input with data.inventory as roles_inventory
	not event_class(other, "network-report") with input as roles_input with data.inventory as roles_inventory
}

test_route_class_matches_the_route_path {
	handler := model_effect("m3") with input as roles_input with data.inventory as roles_inventory
	route_class(handler, "report") with input as roles_input with data.inventory as roles_inventory
	other := model_effect("m4") with input as roles_input with data.inventory as roles_inventory
	not route_class(other, "report") with input as roles_input with data.inventory as roles_inventory
}

codediff_inventory := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {
	"CodeGraph": {"market": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"repository": "market", "effects": [
		{"id": "e1", "kind": "proc.exec", "file": "scripts/bring-up.sh", "line": 21},
	]}}},
}}}}

codediff_input := {"review": {"kind": {"kind": "CodeDiff"}, "object": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"base": "a", "head": "b", "files": []}}}}

test_a_codediff_review_resolves_the_repository_graph {
	name := repository_name with input as codediff_input with data.inventory as codediff_inventory
	name == "market"
	graph := code_graph with input as codediff_input with data.inventory as codediff_inventory
	count(graph.spec.effects) == 1
}

named_codediff_input := {"review": {"kind": {"kind": "CodeDiff"}, "object": {"metadata": {"name": "a-change-1", "namespace": "default"}, "spec": {"repository": "market", "base": "a", "head": "b", "files": []}}}}

test_a_codediff_that_names_its_repository_prefers_the_field {
	name := repository_name with input as named_codediff_input with data.inventory as codediff_inventory
	name == "market"
	graph := code_graph with input as named_codediff_input with data.inventory as codediff_inventory
	count(graph.spec.effects) == 1
}
