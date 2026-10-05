package guestreportreachin

parameters := {
	"hostGlobs": ["lib/market-bidder/**"],
	"emitterPatterns": "ON_NETWORK_EVENT|registerIdentity|computeIdentity|nodeId",
	"reachInPatterns": ["\\.getNodeId\\s*\\(", "container\\s+exec"],
	"edgeKinds": ["calls"],
}

review := {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"repository": "market"}}}

graph(nodes, edges) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"CodeGraph": {"market": {
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {"repository": "market", "files": [], "nodes": nodes, "edges": edges, "texts": {}},
}}}}}}

emitter := {"id": "fn:emit", "kind": "function", "name": "emitOnNetwork", "qualifiedName": "emitOnNetwork", "file": "lib/market-bidder/mod.ts", "startLine": 5, "text": "return { type: ON_NETWORK_EVENT, address: report.address }\n"}

edge := {"source": "fn:emit", "target": "fn:nodeid", "kind": "calls", "line": 6}

test_violation_when_the_emitter_reaches_get_node_id {
	node := {"id": "fn:nodeid", "kind": "method", "name": "getNodeId", "qualifiedName": "provider.getNodeId", "file": "lib/market-bidder/provider.ts", "startLine": 20, "text": "return provider.getNodeId(providerId)\n"}
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph([emitter, node], [edge])
	count(violations) == 1
}

test_no_violation_when_the_emitter_only_reads_the_report {
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph([emitter], [])
	count(violations) == 0
}
