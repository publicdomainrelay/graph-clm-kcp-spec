package relayonlyssh

parameters := {
	"testGlobs": ["test/**"],
	"driverIdentifiers": "createMarketBidder|runComputeContract",
	"sshPattern": "new Deno\\.Command\\(\\s*[\"']ssh[\"']",
	"proxyCommandPattern": "ProxyCommand",
	"allowedTransports": ["websocat", "dumbpipe connect", "iroh connect"],
	"directDialPatterns": ["Deno\\.connect\\(\\{[^}]*guest", "[\"']-p[\"']\\s*,\\s*String\\("],
	"edgeKinds": ["calls"],
}

review := {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"repository": "market"}}}

graph(nodes, edges) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"CodeGraph": {"market": {
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {
		"repository": "market",
		"files": [
			{"path": "test/bidder_test.ts", "test": true},
			{"path": "lib/requester.ts", "test": false},
		],
		"nodes": nodes,
		"edges": edges,
		"texts": {"test/bidder_test.ts": "Deno.test(\"contract\", () => runComputeContract())\n"},
	},
}}}}}}

driver_node := {"id": "file:test/bidder_test.ts", "kind": "file", "name": "bidder_test.ts", "qualifiedName": "bidder_test.ts", "file": "test/bidder_test.ts", "text": ""}

driver_edge := {"source": "file:test/bidder_test.ts", "target": "fn:ssh", "kind": "calls", "line": 4}

test_violation_when_ssh_has_no_proxy_command {
	ssh := {"id": "fn:ssh", "kind": "function", "name": "runSession", "qualifiedName": "runSession", "file": "lib/requester.ts", "startLine": 10, "text": "new Deno.Command(\"ssh\", { args: [\"-p\", String(contract.guestPort), \"root@\" + contract.guestHost] })\n"}
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph([driver_node, ssh], [driver_edge])
	count(violations) == 2
}

test_no_violation_when_ssh_carries_the_relay_proxy_command {
	ssh := {"id": "fn:ssh", "kind": "function", "name": "runSession", "qualifiedName": "runSession", "file": "lib/requester.ts", "startLine": 10, "text": "new Deno.Command(\"ssh\", { args: [\"-o\", \"ProxyCommand=websocat --binary wss://guest\"] })\n"}
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph([driver_node, ssh], [driver_edge])
	count(violations) == 0
}

test_no_violation_when_no_test_drives_the_contract {
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph([driver_node], [])
	count(violations) == 0
}
