package guestreportdrivenemission

parameters := {
	"hostGlobs": ["lib/market-bidder/**"],
	"emitterPatterns": "computeIdentity|REGISTER_IDENTITY|registerIdentity|nodeId",
	"handlerPatterns": "guest\\.onNetwork|OnNetworkReport|/v1/on-network",
	"edgeKinds": ["calls", "contains"],
}

review := {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"repository": "market"}}}

graph(nodes, edges) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"CodeGraph": {"market": {
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {"repository": "market", "files": [], "nodes": nodes, "edges": edges, "texts": {}},
}}}}}}

emitter := {"id": "fn:emit", "kind": "function", "name": "recordIdentity", "qualifiedName": "recordIdentity", "file": "lib/market-bidder/mod.ts", "startLine": 40, "text": "return { computeIdentity: { nodeId: report.nodeId } }\n"}

handler := {"id": "fn:handler", "kind": "function", "name": "guestOnNetwork", "qualifiedName": "guestOnNetwork", "file": "lib/market-bidder/mod.ts", "startLine": 10, "text": "app.post(\"/v1/on-network\", (c) => recordIdentity(c))\n"}

test_violation_when_the_emitter_is_not_reachable_from_the_guest_report_handler {
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph([emitter], [])
	count(violations) == 1
}

test_no_violation_when_the_guest_report_handler_reaches_the_emitter {
	edge := {"source": "fn:handler", "target": "fn:emit", "kind": "calls", "line": 11}
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph([emitter, handler], [edge])
	count(violations) == 0
}
