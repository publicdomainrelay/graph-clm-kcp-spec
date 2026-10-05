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

commented := code_diff([{
	"path": "scripts/bring-up.sh",
	"status": "modified",
	"added": [{"line": 22, "text": "  # ssh-keygen is replaced by the cloud-init module"}],
}])

markdown := code_diff([{
	"path": "request-vm-ssh/README.md",
	"status": "modified",
	"added": [{"line": 105, "text": "Run ssh-keygen once, then paste the key into the guest's authorized_keys."}],
}])

both_patterns := code_diff([{
	"path": "scripts/bring-up.sh",
	"status": "modified",
	"added": [{"line": 30, "text": "  ssh-keygen -f key && cat key.pub >> authorized_keys"}],
}])

test_no_violation_when_the_line_is_a_comment {
	call := {"parameters": parameters, "review": review(commented)}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_line_is_markdown {
	call := {"parameters": parameters, "review": review(markdown)}
	violations := violation with input as call
	count(violations) == 0
}

test_one_violation_when_both_patterns_match_one_line {
	call := {"parameters": parameters, "review": review(both_patterns)}
	violations := violation with input as call
	count(violations) == 1
}
