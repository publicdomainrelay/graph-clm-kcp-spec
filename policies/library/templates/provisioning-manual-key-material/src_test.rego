package provisioningmanualkeymaterial

parameters := {"patterns": [`\bauthorized_keys\b`, `\bssh-keygen\b`]}

code_diff(files) := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "CodeDiff",
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {"base": "aaaa", "head": "bbbb", "files": files},
}

review(object) := {"kind": {"kind": "CodeDiff"}, "object": object}

authorized_keys := code_diff([{
	"path": "scripts/bring-up.sh",
	"status": "modified",
	"added": [{"line": 22, "text": "  echo \"$PUBKEY\" >> /root/.ssh/authorized_keys"}],
}])

ssh_keygen := code_diff([{
	"path": "test/guest_test.ts",
	"status": "modified",
	"added": [{"line": 9, "text": "  ssh-keygen -t ed25519 -N '' -f /root/.ssh/id_ed25519"}],
}])

cloud_init_module := code_diff([{
	"path": "lib/modules/fedproxy/mod.ts",
	"status": "modified",
	"added": [{"line": 31, "text": "  authorized_keys: [userData.sshPublicKey],"}],
}])

test_violation_when_authorized_keys_is_written_by_hand {
	call := {"parameters": parameters, "review": review(authorized_keys)}
	violations := violation with input as call
	count(violations) == 1
}

test_violation_when_a_keypair_is_generated_by_hand {
	call := {"parameters": parameters, "review": review(ssh_keygen)}
	violations := violation with input as call
	count(violations) == 1
}

test_no_violation_when_the_line_is_allowed {
	allow := {"patterns": parameters.patterns, "allowPatterns": [`authorized_keys:\s*\[`]}
	call := {"parameters": allow, "review": review(cloud_init_module)}
	violations := violation with input as call
	count(violations) == 0
}
