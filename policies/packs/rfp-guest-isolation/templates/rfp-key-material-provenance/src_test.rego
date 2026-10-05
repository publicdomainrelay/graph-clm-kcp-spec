package rfpkeymaterialprovenance

parameters := {
	"guestRole": "guest",
	"writtenKinds": ["proc.exec", "file.write"],
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

test_violation_when_a_script_makes_key_material_by_hand {
	files := [added("scripts/bring-up-guest.sh", 21, "ssh-keygen -t ed25519 -N '' -f /root/.ssh/id_ed25519")]
	effect := {"id": "e1", "kind": "proc.exec", "file": "scripts/bring-up-guest.sh", "line": 21, "attrs": {"argv0": "ssh-keygen"}}
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([effect])
	count(violations) == 1
}

test_no_violation_when_the_guest_user_data_writes_the_root_key {
	files := [added("lib/common/cloud-init-common/mod.ts", 487, "path: \"/root/.ssh/authorized_keys\",")]
	effect := {"id": "e2", "kind": "file.write", "file": "lib/common/cloud-init-common/mod.ts", "line": 487, "attrs": {}}
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([effect])
	count(violations) == 0
}

test_no_violation_when_a_test_asserts_on_the_key_file {
	files := [added("test/cloud_init_snapshot_test.ts", 139, "assert(y.includes(\"/root/.ssh/authorized_keys\"), \"root key installed\");")]
	violations := violation with input as {"parameters": parameters, "review": review(files)} with data.inventory as inventory([])
	count(violations) == 0
}
