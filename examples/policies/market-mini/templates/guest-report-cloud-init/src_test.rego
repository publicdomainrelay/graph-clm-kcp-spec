package guestreportcloudinit

parameters := {
	"guestGlobs": ["lib/common/cloud-init-common/**"],
	"reportPattern": "irohReportUrl|report-ticket|on-network|fedproxy-client",
}

review := {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "market", "namespace": "default"}, "spec": {"repository": "market"}}}

graph(texts) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"CodeGraph": {"market": {
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {
		"repository": "market",
		"files": [{"path": "lib/common/cloud-init-common/mod.ts", "test": false}],
		"nodes": [],
		"edges": [],
		"texts": texts,
	},
}}}}}}

test_violation_when_no_module_reports_out {
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph({"lib/common/cloud-init-common/mod.ts": "ExecStart=/usr/local/bin/iroh endpoint\n"})
	count(violations) == 1
}

test_no_violation_when_a_module_reports_out {
	call := {"parameters": parameters, "review": review}
	violations := violation with input as call with data.inventory as graph({"lib/common/cloud-init-common/mod.ts": "curl -X POST \"$irohReportUrl\" -d \"$TICKET\"\n"})
	count(violations) == 0
}
