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

schema_validation := code_diff([{
	"path": "deploy/install-provider.sh",
	"status": "modified",
	"added": [{"line": 12, "text": "  \"$KUBECTL\" --kubeconfig=\"$KUBECONFIG\" apply --validate=false \"$@\";"}],
}])

node_env := code_diff([{
	"path": "test/client.ts",
	"status": "modified",
	"added": [{"line": 9, "text": "  process.env.NODE_TLS_REJECT_UNAUTHORIZED = \"0\";"}],
}])

skopeo_tls_verify := code_diff([{
	"path": "scripts/pull.sh",
	"status": "modified",
	"added": [{"line": 4, "text": "  skopeo copy --tls-verify=false docker://example/image oci:image"}],
}])

insecure_skip := code_diff([{
	"path": "deploy/install-provider.sh",
	"status": "modified",
	"added": [{"line": 21, "text": "  \"$KUBECTL\" --server=\"$PROVIDER_SERVER\" --insecure-skip-tls-verify apply -f -"}],
}])

two_patterns_one_line := code_diff([{
	"path": "scripts/probe.sh",
	"status": "modified",
	"added": [{"line": 30, "text": "  curl -k --insecure \"https://${HOST}/health\""}],
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

test_no_violation_when_only_schema_validation_is_off {
	call := {"parameters": parameters, "review": review(schema_validation)}
	violations := violation with input as call
	count(violations) == 0
}

test_violation_when_the_node_tls_escape_hatch_is_set {
	call := {"parameters": parameters, "review": review(node_env)}
	violations := violation with input as call
	count(violations) == 1
}

test_violation_when_skopeo_disables_tls_verification {
	call := {"parameters": parameters, "review": review(skopeo_tls_verify)}
	violations := violation with input as call
	count(violations) == 1
}

test_violation_when_kubectl_skips_tls_verification {
	call := {"parameters": parameters, "review": review(insecure_skip)}
	violations := violation with input as call
	count(violations) == 1
}

test_one_violation_when_several_patterns_match_one_line {
	call := {"parameters": parameters, "review": review(two_patterns_one_line)}
	violations := violation with input as call
	count(violations) == 1
}
