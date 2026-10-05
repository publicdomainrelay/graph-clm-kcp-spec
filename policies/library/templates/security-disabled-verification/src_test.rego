package securitydisabledverification

parameters := {}

code_diff(files) := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "CodeDiff",
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {"base": "aaaa", "head": "bbbb", "files": files},
}

review(object) := {"kind": {"kind": "CodeDiff"}, "object": object}

curl_k := code_diff([{
	"path": "deploy/examples/atproto/market/accept.sh",
	"status": "modified",
	"added": [{"line": 141, "text": "  curl -skS \"https://${HOST}:${PORT}/xrpc/_health\""}],
}])

node_reject := code_diff([{
	"path": "lib/requester-xrpc/mod.ts",
	"status": "modified",
	"added": [{"line": 302, "text": "  const client = Deno.createHttpClient({ rejectUnauthorized: false });"}],
}])

verified := code_diff([{
	"path": "deploy/examples/atproto/market/accept.sh",
	"status": "modified",
	"added": [{"line": 141, "text": "  curl -sS --cacert ca.pem \"https://${HOST}:${PORT}/xrpc/_health\""}],
}])

commented := code_diff([{
	"path": "deploy/examples/atproto/market/accept.sh",
	"status": "modified",
	"added": [{"line": 150, "text": "  # curl -skS https://${HOST}/xrpc/_health"}],
}])

test_violation_when_curl_disables_verification {
	call := {"parameters": parameters, "review": review(curl_k)}
	violations := violation with input as call
	count(violations) == 1
}

test_violation_when_the_client_rejects_no_certificate {
	call := {"parameters": parameters, "review": review(node_reject)}
	violations := violation with input as call
	count(violations) == 1
}

test_no_violation_when_the_fetch_verifies_against_a_ca {
	call := {"parameters": parameters, "review": review(verified)}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_line_is_a_comment {
	call := {"parameters": parameters, "review": review(commented)}
	violations := violation with input as call
	count(violations) == 0
}
