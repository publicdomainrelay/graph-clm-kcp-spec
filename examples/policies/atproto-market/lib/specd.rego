package lib.specd

api_version = "specs.publicdomainrelay.dev/v1alpha1"

default namespace = "default"

namespace = ns {
	ns := input.review.object.metadata.namespace
	ns != ""
}

default repository_name = ""

repository_name = name {
	name := input.review.object.spec.repository
	name != ""
}

repository_name = name {
	input.review.kind.kind == "Repository"
	name := input.review.object.metadata.name
}

repository_name = name {
	input.review.kind.kind == "SpecChange"
	context := data.inventory.namespace[namespace][api_version]["SystemContext"][input.review.object.spec.systemContext]
	name := context.spec.repository
}

default repository = {}

repository = obj {
	obj := data.inventory.namespace[namespace][api_version]["Repository"][repository_name]
}

default code_graph = {"spec": {"files": [], "nodes": [], "edges": [], "texts": {}}}

code_graph = obj {
	obj := data.inventory.namespace[namespace][api_version]["CodeGraph"][repository_name]
}

default arch = {}

arch = obj {
	obj := data.inventory.namespace[namespace][api_version]["Architecture"][repository_name]
}

default contexts = []

contexts = out {
	out := [obj | obj := data.inventory.namespace[namespace][api_version]["SystemContext"][_]]
}

context(name) = obj {
	obj := data.inventory.namespace[namespace][api_version]["SystemContext"][name]
}

requirement(ctx, id) = req {
	req := context(ctx).spec.requirements[_]
	req.id == id
}

files_matching(globs) = out {
	out := [file | file := code_graph.spec.files[_]; globs_match(globs, file.path)]
}

tests_matching(globs) = out {
	out := [file | file := code_graph.spec.files[_]; file.test; globs_match(globs, file.path)]
}

nodes_in_files(paths) = out {
	out := [node | node := code_graph.spec.nodes[_]; paths[_] == node.file]
}

nodes_in_context(name) = out {
	out := [node | node := code_graph.spec.nodes[_]; node.context == name]
}

nodes_named(pattern) = out {
	out := [node | node := code_graph.spec.nodes[_]; re_match(pattern, node.name)]
}

nodes_qualified(pattern) = out {
	out := [node | node := code_graph.spec.nodes[_]; re_match(pattern, node.qualifiedName)]
}

tests_matching_text(globs, pattern) = out {
	out := [file | file := tests_matching(globs)[_]; re_match(pattern, code_graph.spec.texts[file.path])]
}

node(id) = out {
	out := code_graph.spec.nodes[_]
	out.id == id
}

file_node(path) = out {
	out := code_graph.spec.nodes[_]
	out.kind == "file"
	out.file == path
}

nodes_with_id(ids) = out {
	out := [node | node := code_graph.spec.nodes[_]; ids[node.id]]
}

nodes_matching_text(pattern) = out {
	out := [node | node := code_graph.spec.nodes[_]; re_match(pattern, node.text)]
}

identified(node, pattern) {
	re_match(pattern, node.text)
}

identified(node, pattern) {
	re_match(pattern, node.name)
}

identified(node, pattern) {
	re_match(pattern, node.qualifiedName)
}

nodes_identified(pattern) = out {
	out := {node | node := code_graph.spec.nodes[_]; identified(node, pattern)}
}

nodes_matching_globs(globs, pattern) = out {
	out := [node |
		node := code_graph.spec.nodes[_]
		globs_match(globs, node.file)
		re_match(pattern, node.text)
	]
}

nodes_reachable_from(ids, kinds, pattern) = out {
	out := [node |
		reached := reachable_from(ids, kinds)
		node := nodes_with_id(reached)[_]
		re_match(pattern, node.text)
	]
}

node_match_line(node, pattern) = out {
	node.startLine > 0
	lines := split(node.text, "\n")
	found := {line | line := node.startLine + index; re_match(pattern, lines[index])}
	out := sort(found)[0]
}

first_line(path, pattern) = out {
	lines := split(code_graph.spec.texts[path], "\n")
	found := {line | line := index + 1; re_match(pattern, lines[index])}
	out := sort(found)[0]
}

matches_globs(globs, path) {
	globs_match(globs, path)
}

definition_node(node) {
	node.kind != "file"
	node.kind != "import"
}

edge_kinds = ["calls", "imports", "contains", "references", "instantiates", "implements", "extends"]

calls_from(id) = out {
	out := [edge.target | edge := code_graph.spec.edges[_]; edge.source == id; edge.kind == "calls"]
}

callers_of(id) = out {
	out := [edge.source | edge := code_graph.spec.edges[_]; edge.target == id; edge.kind == "calls"]
}

selected_edges(kinds) = out {
	out := [edge | edge := code_graph.spec.edges[_]; kinds[_] == edge.kind]
}

vertices(kinds) = out {
	selected := selected_edges(kinds)
	out := {vertex | vertex := selected[_].source} | {vertex | vertex := selected[_].target}
}

adjacency(kinds) = graph {
	selected := selected_edges(kinds)
	graph := {source: targets |
		source := vertices(kinds)[_]
		targets := [target | other := selected[_]; other.source == source; target := other.target]
	}
}

reverse_adjacency(kinds) = graph {
	selected := selected_edges(kinds)
	graph := {target: sources |
		target := vertices(kinds)[_]
		sources := [source | other := selected[_]; other.target == target; source := other.source]
	}
}

reachable_from(ids, kinds) = out {
	out := graph.reachable(adjacency(kinds), ids)
}

reaching(ids, kinds) = out {
	out := graph.reachable(reverse_adjacency(kinds), ids)
}

closure_from(ids, kinds) = out {
	out := reachable_from(ids, kinds) | {id | id := ids[_]}
}

closure_reaching(ids, kinds) = out {
	out := reaching(ids, kinds) | {id | id := ids[_]}
}

paths_between(ids, kinds) = out {
	out := graph.reachable_paths(adjacency(kinds), ids)
}

lines_matching(path, pattern) = out {
	lines := split(code_graph.spec.texts[path], "\n")
	out := [{"line": index + 1, "text": line} | line := lines[index]; re_match(pattern, line)]
}

files_matching_text(pattern) = out {
	out := [file.path |
		file := code_graph.spec.files[_]
		re_match(pattern, code_graph.spec.texts[file.path])
	]
}

node_text_matches(node, pattern) {
	re_match(pattern, node.text)
}

default effects = []

effects = out {
	out := code_graph.spec.effects
}

effects_of(kind) = out {
	out := [effect | effect := effects[_]; effect.kind == kind]
}

effects_of_component(component, kind) = out {
	out := [effect | effect := effects[_]; effect.kind == kind; effect.component == component]
}

effects_in(globs) = out {
	paths := {path | path := files_matching(globs)[_].path}
	out := [effect | effect := effects[_]; paths[effect.file]]
}

effect_targets(kind) = out {
	out := [target |
		effect := effects_of(kind)[_]
		target := effect.attrs.target
		target != ""
	]
}

location(file, line) = {"file": file, "line": line}

violation(msg, details) = result {
	result := {"msg": msg, "details": details}
}

globs_match(globs, path) {
	pattern := globs[_]
	glob.match(pattern, [], path)
}
