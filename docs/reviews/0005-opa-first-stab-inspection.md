# Review 0005: inspection of deno-kcp `opa-first-stab/opa`

- Source: https://github.com/publicdomainrelay/deno-kcp/tree/opa-first-stab/opa
- Inspected at commit `5d662f4` ("opa: rounds 3 to 6, ten policy sets, the
  catalogue, the integration gate") on 2026-10-04.
- Method: a fresh shallow clone; read `README.md`, `plans/DOMAIN.md`,
  `plans/RESULTS.md`, `plans/ROUND-*.md`, `run.sh`, `lib/violation`, and
  `policies/code_safety/violations/provisioning.rego`.

This inspection is the "What exists and what we keep" input to
`docs/plans/0008-policies.md`.

## What it is

| part | content |
| --- | --- |
| size | 12,701 lines; 10 policy sets; 238 violation ids |
| engine | plain OPA (`package deno_kcp.*`, Rego v1), `opa eval` from `run.sh` |
| input | one JSON document built by a Python loader, `tools/load_spec_tree.py`, from a spec tree checkout and a git diff: `spec_tree`, `base_tree`, `change`, `diff`, `options` |
| violation | `{id, policy, violation, location, message, details}`. Severity, level and title are joined in from each policy's `metadata`, not set by the rule |
| report | `data.deno_kcp.report` (counts by severity, policy and id) and `data.deno_kcp.passed` |
| policy sets | spec_structure, spec_references, arch_consistency, change_integrity, change_impact, acceptance_integrity, traceability, code_safety, change_security, change_quality |
| results | 54 violations on the base tree and 74 on the change plus diff. Real finds include a SpecChange `Succeeded` while its gating acceptance failed, `curl -k` in `accept.sh`, and a machine path in a requirement |
| calibration | thresholds taken from the real corpus (requirement word counts and so on); "unmeasurable is not wrong" |

## What it lacks, for the goal

- No Gatekeeper: there are no ConstraintTemplates or constraints, no
  `enforcementAction`, and no `gator`.
- No link to specd's lifecycle: nothing runs at spec time or at realize, and
  there is no audit and no kcp object.
- Policies are not stored as their own artifact. They live in the code
  repository's `opa/` directory, not on an orphan branch next to the specs.
- Code is regex over added diff lines, with no call graph. The rule "the host
  never reaches into the guest for vm.onNetwork" is about reachability and
  initiator direction, which diff regex cannot express.
- Everything is specific to deno-kcp (`package deno_kcp`), so there is no
  portability.

## What plan 0008 kept and changed

| opa-first-stab | hydradb |
| --- | --- |
| violation object plus metadata join | Gatekeeper `{msg, details}` joined with template annotations (title, level, severity, requirements) |
| calibration discipline, "not a policy" list | kept: plan 0008 "Not in scope", and every ported rule carries its calibration note |
| Python loader to one JSON | Go builders: CodeGraph (codegraph sqlite), CodeDiff, effects, ArchitectureModel, passed as Gatekeeper reviews and inventory |
| `opa/` in the code tree | orphan branch `open-policy/<repo>[--<branch>]`, two-way with kcp ConstraintTemplate/constraint objects |
| `run.sh` gate in CI | specd spec-time gate, realize gate with agent feedback, audit on every indexed commit |
| 10 sets, 238 ids | 7 rules ported to Gatekeeper in `policies/library` (plan 0008 E): change-succeeded-with-failed-acceptance, requirement-text-has-machine-path, provisioning-container-in-test, provisioning-manual-key-material, provisioning-cloud-init-bypass, provisioning-new-guest-transport, security-disabled-verification |
| `code_safety` provisioning regex | model-level forms in `policies/packs/rfp-guest-isolation` (plan 0009 G4) |
