package rfprelayonlyguestssh

import data.lib.specd

guest_role := object.get(input.parameters, "guestRole", "guest")

tester_role := object.get(input.parameters, "testRole", "test")

relay_class := object.get(input.parameters, "relayClass", "relay")

attrs(effect) = out {
	out := object.get(effect, "attrs", {})
}

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	effect.kind == "ssh.connect"
	not specd.component_in_role(effect.component, guest_role)
	reaches_the_guest(effect)
	not relayed(effect)
	msg := sprintf("ssh from %s reaches the guest directly: %s:%d; a guest is reached only through a relay, so give the ssh a ProxyCommand that goes through any transport -- the org relay, fedproxy, websocat, iroh, dumbpipe, cloudflared or an unnamed tunnel",
		[effect.component, effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "file": effect.file, "line": object.get(effect, "line", 0)}
}

# reaches_the_guest reads the ssh's own destination, then the flow the model
# built for it. An ssh whose destination the model cannot read is treated as
# the guest: a repository that does not bind its guest's addresses is guarded
# rather than trusted. An ssh to a host the binding knows is something else --
# a git server, a jumphost -- is not this repository's guest and is left alone.
reaches_the_guest(effect) {
	not has_ssh_host(effect)
}

reaches_the_guest(effect) {
	ssh_host(effect) == guest_role
}

reaches_the_guest(effect) {
	specd.component_in_role(ssh_host(effect), guest_role)
}

reaches_the_guest(effect) {
	flow := specd.model_flows[_]
	flow.evidence[_] == effect.id
	specd.acted_on_in_role(flow, guest_role)
}

has_ssh_host(effect) {
	ssh_host(effect) != ""
}

ssh_host(effect) = host {
	host := object.get(attrs(effect), "host", "")
	host != ""
} else = host {
	host := object.get(attrs(effect), "destination", "")
	host != ""
} else = host {
	raw := object.get(attrs(effect), "target", "")
	raw != ""
	host := without_user(raw)
}

without_user(raw) = host {
	parts := split(raw, "@")
	count(parts) == 2
	host := parts[1]
}

without_user(raw) = host {
	parts := split(raw, "@")
	count(parts) == 1
	host := parts[0]
}

# relayed is a property of the ssh's own arguments and of nothing else. A relay
# word in the enclosing function, in a function the ssh calls, or on the line
# above it is not this ssh's relay: the exemption a relay earns is earned by
# the call itself.
relayed(effect) {
	object.get(attrs(effect), "proxyJump", "") != ""
}

relayed(effect) {
	object.get(attrs(effect), "sshConfig", "") != ""
}

relayed(effect) {
	key := object.keys(attrs(effect))[_]
	contains(lower(key), "transport")
}

relayed(effect) {
	proxy := object.get(attrs(effect), "proxyCommand", "")
	proxy != ""
	not proxy_dials_the_guest(effect, proxy)
}

# proxy_dials_the_guest reads the destination of the proxy command itself. nc,
# socat, openssl s_client, a python socket, /dev/tcp and an inner ssh -W all
# dial an address; when that address is the guest's, is the ssh's own target, is
# %h, or is something the model cannot read, the ssh is a direct connection
# wearing a proxy. Any other proxy -- a named transport, a jumphost, a SOCKS
# hop, a helper the model could not name -- is a relay.
proxy_dials_the_guest(effect, proxy) {
	target := proxy_targets(proxy)[_]
	guest_destination(effect, target)
}

proxy_targets(proxy) = out {
	out := {target |
		pattern := proxy_target_patterns[_]
		match := regex.find_all_string_submatch_n(pattern, proxy, 1)[_]
		target := match[1]
		target != ""
	}
}

proxy_target_patterns := [
	"(?:^|[ /\"'`(])(?:nc\\.openbsd|nc\\.traditional|ncat|netcat|nc)[ \t]+(?:-[^ \t]+[ \t]+)*([^ \t\"'`]+)[ \t]+(?:%p|[0-9]+)",
	"(?:TCP4|TCP6|TCP-CONNECT|TCP):([^ \t:\"'`]+)",
	"s_client[^\n]*?-connect[ \t]+([^ \t:\"'`]+)",
	"/dev/tcp/([^/ \t\"'`]+)",
	"(?:create_connection|\\bconnect)\\([ \t]*\\(?[ \t]*[\"'`]([^\"'`]+)",
	"\\bssh\\b[^\n]*?-W[ \t]+[^ \t\"'`]+[ \t]+([^ \t\"'`]+)",
]

guest_destination(effect, target) {
	target == "%h"
}

guest_destination(effect, target) {
	startswith(target, "%")
}

guest_destination(effect, target) {
	startswith(target, "$")
}

guest_destination(effect, target) {
	target == guest_role
}

guest_destination(effect, target) {
	specd.component_in_role(target, guest_role)
}

guest_destination(effect, target) {
	has_ssh_host(effect)
	target == ssh_host(effect)
}

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	effect.kind == "net.dial"
	specd.component_in_role(effect.component, tester_role)
	dial_reaches_the_guest(effect)
	msg := sprintf("the test dials the guest directly: %s:%d", [effect.file, object.get(effect, "line", 0)])
	details := {"effect": effect, "file": effect.file, "line": object.get(effect, "line", 0)}
}

# dial_reaches_the_guest is the test-side half of the host rule: a dial to the
# guest, and a dial whose target the model could not resolve, are both
# reach-ins. The binding names the exceptions -- the targets a test may dial.
dial_reaches_the_guest(effect) {
	flow := dial_flow(effect)
	specd.acted_on_in_role(flow, guest_role)
}

dial_reaches_the_guest(effect) {
	flow := dial_flow(effect)
	specd.acted_on(flow) == "unknown"
	not dial_exception(effect)
}

dial_reaches_the_guest(effect) {
	not dial_flow(effect)
	not dial_exception(effect)
}

dial_flow(effect) = flow {
	flow := specd.model_flows[_]
	flow.evidence[_] == effect.id
}

dial_exception(effect) {
	pattern := dial_exception_patterns[_]
	glob.match(pattern, ["/"], effect.file)
}

dial_exception(effect) {
	pattern := dial_exception_patterns[_]
	value := attrs(effect)[_]
	contains(lower(value), lower(pattern))
}

dial_exception_patterns := object.get(specd.model_vocabulary, "reachInExceptions", [])

# Rule 1 holds at spec time too (review 0006 B9): the effects a declaration
# will produce do not exist yet, so the clause reads the declared flow itself.
# A declaration that sends a non-guest initiator at the guest over a channel
# that names no relay term is the reach-in the rule forbids, declared. The
# check needs the binding to name its relay terms under channels/<relayClass>;
# a binding that does not is recorded as a limit in docs/policies.md.
violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	specd.declared(flow)
	not specd.initiator_in_role(flow, guest_role)
	specd.acted_on_in_role(flow, guest_role)
	relay_class_declared
	not declared_relay(flow)
	msg := sprintf("the declared flow %s -> %s reaches the guest over channel %q, which names no relay; a guest is reached only through a relay, so declare the flow over a channel the binding lists under channels/%s",
		[flow.from, flow.to, object.get(flow, "channel", ""), relay_class])
	details := {"flow": flow, "from": flow.from, "to": flow.to, "channel": object.get(flow, "channel", ""), "source": object.get(flow, "source", "")}
}

relay_class_declared {
	count(specd.vocabulary_terms("channels", relay_class)) > 0
}

declared_relay(flow) {
	term := specd.vocabulary_terms("channels", relay_class)[_]
	specd.term_in(object.get(flow, "channel", ""), term)
}
