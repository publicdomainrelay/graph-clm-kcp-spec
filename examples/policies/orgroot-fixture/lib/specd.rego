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

# A diff a realize reviews may carry no repository: it is then named after the
# repository it belongs to, so a rule that reads the diff still resolves the
# code graph. The name is only the fallback; a diff that names its repository
# is resolved by the clause above, and two clauses producing two names would be
# a conflict rather than a lookup.
repository_name = name {
	input.review.kind.kind == "CodeDiff"
	not review_names_a_repository
	name := input.review.object.metadata.name
}

review_names_a_repository {
	input.review.object.spec.repository != ""
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

default architecture_model = {"spec": {"components": [], "effects": [], "flows": [], "triggers": []}}

architecture_model = obj {
	obj := data.inventory.namespace[namespace][api_version]["ArchitectureModel"][repository_name]
}

default model_components = []

model_components = out {
	out := architecture_model.spec.components
}

default model_flows = []

model_flows = out {
	out := architecture_model.spec.flows
}

default model_triggers = []

model_triggers = out {
	out := architecture_model.spec.triggers
}

components_with_role(role) = out {
	out := [component | component := model_components[_]; component.roles[_] == role]
}

roles_of(component) = out {
	out := [role | entry := model_components[_]; entry.name == component; role := entry.roles[_]]
}

flows_where(filter) = out {
	out := [flow | flow := model_flows[_]; flow_filter_matches(flow, filter)]
}

flow_filter_matches(flow, filter) {
	not flow_filter_key_fails(flow, filter)
}

flow_filter_key_fails(flow, filter) {
	key := [name | _ = filter[name]][_]
	not flow_filter_key_matches(flow, filter, key)
}

flow_filter_key_matches(flow, filter, "from") {
	flow.from == filter.from
}

flow_filter_key_matches(flow, filter, "to") {
	flow.to == filter.to
}

flow_filter_key_matches(flow, filter, "initiator") {
	flow.initiator == filter.initiator
}

flow_filter_key_matches(flow, filter, "channel") {
	flow.channel == filter.channel
}

flow_filter_key_matches(flow, filter, "purpose") {
	flow.purpose == filter.purpose
}

flow_filter_key_matches(flow, filter, "source") {
	flow.source == filter.source
}

flow_filter_key_matches(flow, filter, "carries") {
	is_string(filter.carries)
	flow.carries[_] == filter.carries
}

flow_filter_key_matches(flow, filter, "carries") {
	is_array(filter.carries)
	wanted := filter.carries[_]
	flow.carries[_] == wanted
}

triggered_by(effect_id) = out {
	out := [trigger | trigger := model_triggers[_]; trigger.to == effect_id]
}

default model_effects = []

model_effects = out {
	out := architecture_model.spec.effects
}

model_effect(id) = out {
	out := model_effects[_]
	out.id == id
}

default model_vocabulary = {}

model_vocabulary = obj {
	obj := object.get(architecture_model.spec, "vocabulary", {})
}

default model_roles = []

model_roles = out {
	out := object.get(architecture_model.spec, "roles", [])
}

# vocabulary_terms names the concrete terms a binding wrote for one class of
# one group: vocabulary_terms("events", "network-report").
vocabulary_terms(group, class) = out {
	out := object.get(object.get(model_vocabulary, group, {}), class, [])
}

# component_in_role is the role of a component, by the roles the model gave it
# or by its name when the model named it after the role alone.
component_in_role(component, role) {
	roles_of(component)[_] == role
}

component_in_role(component, role) {
	component == role
}

initiator_in_role(flow, role) {
	initiator_of(flow) == role
}

initiator_in_role(flow, role) {
	entry := model_components[_]
	entry.name == initiator_of(flow)
	entry.roles[_] == role
}

acted_on_in_role(flow, role) {
	acted_on(flow) == role
}

acted_on_in_role(flow, role) {
	entry := model_components[_]
	entry.name == acted_on(flow)
	entry.roles[_] == role
}

# file_in_role says whose file a path is: a component that carries the role
# owns it when one of the globs the binding gave that role matches the path. It
# is how a rule that reads a CodeDiff tells the guest's files from the rest.
file_in_role(path, role) {
	component := model_components[_]
	component.roles[_] == role
	pattern := object.get(component, "globs", [])[_]
	glob.match(pattern, ["/"], path)
}

# event_class is an event.emit whose emitted type is one of the terms the
# binding wrote for an event class. The comparison is case-insensitive and one
# way: the emitted type carries the vocabulary term, as a constant name carries
# the NSID it stands for.
event_class(effect, class) {
	effect.kind == "event.emit"
	emitted := object.get(object.get(effect, "attrs", {}), "type", "")
	emitted != ""
	term := vocabulary_terms("events", class)[_]
	term_in(emitted, term)
}

# term_in matches a vocabulary term as a whole token. The comparison is
# case-insensitive; a term that is part of a longer word is not the class.
term_in(text, term) {
	term != ""
	lowered := lower(text)
	needle := lower(term)
	start := indexof(lowered, needle)
	start >= 0
	token_before(lowered, start)
	token_after(lowered, start + count(needle))
}

token_before(text, index) {
	index == 0
}

token_before(text, index) {
	index > 0
	not is_token_char(substring(text, index-1, 1))
}

token_after(text, index) {
	index == count(text)
}

token_after(text, index) {
	index < count(text)
	not is_token_char(substring(text, index, 1))
}

is_token_char(char) {
	contains("abcdefghijklmnopqrstuvwxyz0123456789_", char)
}

# route_class is an http.handle on a route the binding wrote for a route class.
route_class(effect, class) {
	effect.kind == "http.handle"
	route := object.get(object.get(effect, "attrs", {}), "path", "")
	route != ""
	term := vocabulary_terms("routes", class)[_]
	term == route
}

route_class(effect, class) {
	effect.kind == "http.handle"
	route := object.get(object.get(effect, "attrs", {}), "path", "")
	route != ""
	term := vocabulary_terms("routes", class)[_]
	glob.match(term, ["/"], route)
}

declared(flow) {
	flow.source == "declared"
}

declared(flow) {
	flow.source == "both"
}

observed(flow) {
	flow.source == "observed"
}

observed(flow) {
	flow.source == "both"
}

forbidden(flow) {
	flow.forbidden == true
}

flow_level(flow, level) {
	flow.level == level
}

# same_flow is the directed shape two flows share when one is a declared
# must-never and the other is a declared or observed flow that violates it: the
# initiating role, the role it acts on, the channel and the purpose. A flow's
# from is the context that declared it and its to is the peer, so the role the
# initiator acts on is the endpoint that is not the initiator. The level, the
# forbidden marker, the carried payloads, the source and the evidence are not
# part of the shape.
same_flow(left, right) {
	initiator_of(left) == initiator_of(right)
	acted_on(left) == acted_on(right)
	object.get(left, "channel", "") == object.get(right, "channel", "")
	object.get(left, "purpose", "") == object.get(right, "purpose", "")
}

initiator_of(flow) = initiator {
	initiator := object.get(flow, "initiator", "")
}

acted_on(flow) = flow.to {
	initiator_of(flow) == flow.from
}

acted_on(flow) = flow.from {
	initiator_of(flow) == flow.to
}

# forbidden_matches names every flow that violates a declared must-never: a flow
# of the same shape that is not itself the marker.
forbidden_matches(flow) {
	not forbidden(flow)
	marker := model_flows[_]
	forbidden(marker)
	same_flow(flow, marker)
}

location(file, line) = {"file": file, "line": line}

violation(msg, details) = result {
	result := {"msg": msg, "details": details}
}

globs_match(globs, path) {
	pattern := globs[_]
	glob.match(pattern, ["/"], path)
}
