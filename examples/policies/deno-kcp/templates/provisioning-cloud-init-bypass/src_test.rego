package provisioningcloudinitbypass

parameters := {
	"userDataPatterns": [`user_data`, `userData`, `cloud-init`, `cloud_config`, `cloud-config`],
	"bypassPatterns": [`skip`, `bypass`, `without`, `disable`, `no-user-data`, `no-userdata`],
}

code_diff(files) := {
	"apiVersion": "specs.publicdomainrelay.dev/v1alpha1",
	"kind": "CodeDiff",
	"metadata": {"name": "market", "namespace": "default"},
	"spec": {"base": "aaaa", "head": "bbbb", "files": files},
}

review(object) := {"kind": {"kind": "CodeDiff"}, "object": object}

bypass := code_diff([{
	"path": "scripts/bring-up.sh",
	"status": "modified",
	"added": [{"line": 14, "text": "  launch_guest --user_data=/dev/null  # bypass the RFP flow"}],
}])

mentions_user_data := code_diff([{
	"path": "lib/modules/tunnel/mod.ts",
	"status": "modified",
	"added": [{"line": 27, "text": "  userData.append(sshModule())"}],
}])

allowed_bypass := code_diff([{
	"path": "lib/modules/tunnel/mod.ts",
	"status": "modified",
	"added": [{"line": 29, "text": "  // user_data is skipped here because the fixture boots from an image"}],
}])

test_violation_when_the_user_data_path_is_skipped {
	call := {"parameters": parameters, "review": review(bypass)}
	violations := violation with input as call
	count(violations) == 1
}

test_no_violation_when_the_line_only_appends_to_user_data {
	call := {"parameters": parameters, "review": review(mentions_user_data)}
	violations := violation with input as call
	count(violations) == 0
}

test_no_violation_when_the_line_is_allowed {
	allow := {"userDataPatterns": parameters.userDataPatterns, "bypassPatterns": parameters.bypassPatterns, "allowPatterns": [`^\s*//`]}
	call := {"parameters": allow, "review": review(allowed_bypass)}
	violations := violation with input as call
	count(violations) == 0
}
