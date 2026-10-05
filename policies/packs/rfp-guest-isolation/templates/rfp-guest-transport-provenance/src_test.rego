package rfpguesstransportprovenance

parameters := {
	"guestRole": "guest",
	"executedKinds": ["proc.exec", "container.exec", "net.listen", "file.write"],
	"installPatterns": ["\\b(?:docker|podman|container|nerdctl)\\s+(?:run|exec|create)\\b", "\\b(?:curl|wget)\\b", "\\btar\\s+-?[xz]", "\\b(?:cp|mv|install|mount)\\s", "\\bchmod\\s", "\\bsystemctl\\s+(?:enable|start)"],
}

review(files) := {"kind": {"kind": "CodeDiff"}, "object": {
	"metadata": {"name": "pack-fixture", "namespace": "default"},
	"spec": {"repository": "pack-fixture", "base": "aaa", "head": "bbb", "files": files},
}}

inventory(effects) := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {
	"ArchitectureModel": {"pack-fixture": {
		"metadata": {"name": "pack-fixture", "namespace": "default"},
		"spec": {
			"repository": "pack-fixture",
			"roles": ["guest", "test"],
			"components": [
				{"name": "guest", "roles": ["guest"], "globs": ["lib/common/cloud-init-common/**"], "source": "observed"},
				{"name": "test", "roles": ["test"], "globs": ["test/**"], "source": "observed"},
			],
			"effects": [],
			"flows": [],
			"triggers": [],
		},
	}},
	"CodeGraph": {"pack-fixture": {
		"metadata": {"name": "pack-fixture", "namespace": "default"},
		"spec": {"repository": "pack-fixture", "effects": effects},
	}},
}}}}

added(path, line, text) := {"path": path, "status": "modified", "added": [{"line": line, "text": text}]}

executing_effect := {"id": "e1", "kind": "container.exec", "file": "test/guest_test.ts", "line": 12, "attrs": {"runtime": "docker", "verb": "run"}}

test_violation_when_a_transport_is_installed_outside_the_guest_role {
	files := [added("test/guest_test.ts", 12, "docker run --mount /bin/websocat guest")]
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([executing_effect])
	count(violations) == 1
}

test_no_violation_when_the_guest_user_data_installs_the_transport {
	files := [added("lib/common/cloud-init-common/mod.ts", 88, "retry sh -c \"curl -sfL websocat.tar.gz | tar -xvz\"")]
	effect := {"id": "e2", "kind": "proc.exec", "file": "lib/common/cloud-init-common/mod.ts", "line": 88, "attrs": {"argv0": "curl"}}
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([effect])
	count(violations) == 0
}

test_no_violation_when_the_line_only_selects_a_transport {
	files := [added("test/guest_test.ts", 12, "return transport === \"iroh\" ? \"dumbpipe\" : \"websocat\";")]
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([])
	count(violations) == 0
}

test_no_violation_for_a_description_that_names_a_transport {
	files := [added("lexicons/com/example/request.json", 72, "\"description\": \"the websocat ProxyCommand. Absent under the default iroh transport.\"")]
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([])
	count(violations) == 0
}

test_no_violation_when_a_probe_only_names_the_transport {
	files := [added("lib/requester-xrpc/mod.ts", 1085, "const which = new Deno.Command(\"which\", { args: [\"dumbpipe\"] });")]
	effect := {"id": "e4", "kind": "proc.exec", "file": "lib/requester-xrpc/mod.ts", "line": 1085, "attrs": {"argv0": "which"}}
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([effect])
	count(violations) == 0
}

test_violation_when_a_script_outside_the_guest_role_runs_a_transport {
	files := [added("scripts/bring-up.sh", 21, "curl -sfL https://example/websocat.tar.gz | tar -xvz -C /usr/local/bin")]
	effect := {"id": "e3", "kind": "proc.exec", "file": "scripts/bring-up.sh", "line": 21, "attrs": {"argv0": "curl"}}
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([effect])
	count(violations) == 1
}
