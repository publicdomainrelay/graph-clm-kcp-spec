package rfpguesstransportprovenance

import data.lib.specd

guest_role := object.get(input.parameters, "guestRole", "guest")

default_transport_patterns := ["websocat", "wstunnel", "chisel", "frp[cs]", "autossh", "rathole", "socat", "dumbpipe", "fedproxy"]

default_executed_kinds := ["proc.exec", "container.exec", "net.listen", "file.write"]

violation[specd.violation(msg, details)] {
	file := input.review.object.spec.files[_]
	row := file.added[_]
	not specd.file_in_role(file.path, guest_role)
	not specd.matches_globs(ignored_paths, file.path)
	pattern := transport_pattern(row.text)[0]
	executed(file.path, row.line)
	installs(row.text)
	msg := sprintf("added line %s:%d installs or runs the guest transport %q outside the %s role's user_data: a guest transport is a UserDataModule in the guest's cloud-init, so the RFP flow stays the only way a guest comes up",
		[file.path, row.line, pattern, guest_role])
	details := {"file": file.path, "line": row.line, "pattern": pattern}
}

ignored_paths = out {
	out := input.parameters.ignoredPaths
} else = ["**/*.md"]

transport_pattern(text) = out {
	out := [pattern | pattern := transport_pattern_list[_]; regex.match(pattern, text)]
}

transport_pattern_list = out {
	out := input.parameters.transportPatterns
} else = default_transport_patterns


default_install_patterns := ["\\b(?:docker|podman|container|nerdctl)\\s+(?:run|exec|create)\\b", "\\b(?:curl|wget)\\b", "\\btar\\s+-?[xz]", "\\b(?:cp|mv|install|mount)\\s", "\\bchmod\\s", "\\bsystemctl\\s+(?:enable|start)", "\\b(?:apt-get|apt|dnf|yum|apk)\\s+install"]

# installs keeps a probe out: `which dumbpipe` names the transport and runs a
# command, and it installs nothing. The line must read like an installation.
installs(text) {
	pattern := install_patterns[_]
	regex.match(pattern, text)
}

install_patterns = out {
	out := input.parameters.installPatterns
} else = default_install_patterns

# executed says the added line is a real site: an effect of an executing or a
# writing kind starts there. A description, a selector or a test name that
# merely mentions a transport is not an installation.
executed(path, line) {
	effect := specd.effects[_]
	effect.file == path
	effect.line == line
	executed_kind(effect.kind)
}

executed_kind(kind) {
	kind == input.parameters.executedKinds[_]
}

executed_kind(kind) {
	not configured_executed_kinds
	kind == default_executed_kinds[_]
}

configured_executed_kinds {
	count(input.parameters.executedKinds) > 0
}
