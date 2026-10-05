# Plan 0010: policy review fixes

Inputs:

- `docs/reviews/0003-policies.md` (Opus): adversarial runs, design, sync and
  gates.
- `docs/reviews/0004-policies-analysis.md` (DeepSeek): plan-vs-code check,
  docs commands and portability.

Ids below cite the review sections: `0003 B1` and so on, and `0004 bug 1` and
so on.

Done means:

- every item is fixed, or explicitly recorded as a limit with the reason;
- tests reproduce each defect before the fix and pass after;
- the review's adversarial variants live in the pack suites;
- gofmt, vet, `go test ./...` and the live suite are green.

## Track R: rules and facts (the two example rules must hold)

R1. **Attribution comes from the call site, not the file.**
- Attributes and hints (proxyCommand, channel, targets) are read from the
  site's own call. A test file's other ssh calls never lend their ProxyCommand.
- Covers 0003 B1. Evidence: `abc/policy/effectmatch.go:411-421`,
  `modelbuild.go:728-746`.

R2. **Relay means a relay.**
- An ssh counts as relayed only when its ProxyCommand (or the resolved
  transport) names a term from vocabulary `channels/relay`.
- `ProxyCommand=nc <guest> 22` and any unnamed tunnel are denied; a binding
  can list extra relay terms.
- This replaces the "any non-empty proxyCommand" clause (0003 B2;
  0004 portability 6).

R3. **Reach-in by default.**
- In the host role, `container.exec`, `ssh.connect` and `net.dial` /
  `http.request` effects whose target is the guest or is unknown are reach-ins
  by default. This includes `container inspect`, a fetch to the guest IP, and
  ssh into the guest over the relay for the purpose of address discovery.
- The binding names exceptions, not the reach-in verbs, so a new repository
  no longer has to rediscover `getNodeId`-like verbs.
- Add `net.dial` to the guest's ssh port (`pollSsh`).
- Covers 0003 B3, B4 and 0004 portability 1, next-step 10.

R4. **Aliases.**
- Resolve simple aliases in classifiers: `const X = "ssh"` used as argv0, a
  host held in a variable. Use intra-file constant propagation in the
  TypeScript and Go packs.
- Measure recall again on the G2 hand-labelled sample and on new labels for
  the alias cases (0003 B4).

R5. **Model accuracy.**
- Flow merge keeps the union of payloads (0003 B5).
- A trigger needs a call edge, not a shared node (0003 B6).
- The event class resolves constants and NSID imports, not the spelling of
  the first argument (0003 B7).
- Payload terms in the guest-reports require are matched as whole tokens in
  the report's body or arguments (0003 B8).

R6. **One glob dialect.**
- Go and Rego use the same doublestar semantics (`**` across directories,
  `*` within one).
- Shared conformance tests run the same table through Go and through Rego
  (0003 A1).

R7. **Fresh facts.**
- eval, model, effects, the audit and both gates detect a stale `.codegraph`.
  Stale means: index mtime or recorded commit older than the tree, or a dirty
  tree.
- They resync, or index a copy, and never evaluate stale facts.
- No index is written into a user's checkout unless `--index-in-place` is
  given (0003 G5; 0004 bug 1).

R8. **Diff parser.** A removed line that starts with `--` (or `++`) inside a
hunk is content, not a header (0003 G4).

R9. **The review's adversarial variants become permanent tests.**
- The 8 variants from 0003 (vB, vC, vD, vE, vH1 to vH3, and the rest) become
  fixtures and gator cases in `policies/packs/rfp-guest-isolation`.
- They also become `fixtures/market-mini` violating variants.
- Each one must deny.

## Track S: sync, gates and kcp

S1. **Real two-way sync.**
- The branch and kcp keep a recorded base: the last synced branch commit and
  the kcp resourceVersions, stored in a status or annotation.
- Branch edits made after the base are applied to kcp, and kcp edits are
  persisted to the branch.
- When both changed, a conflict is reported in a condition and nothing is
  overwritten (0003 S1).

S2. **Per-repository ownership.**
- kcp policy objects carry the label
  `specs.publicdomainrelay.dev/repository`.
- `policykcp.Read` filters on it, so a repository's branch never receives
  another repository's policies (0003 S2).

S3. **Imports stay imports.**
- Persist writes only the repository's own templates. Imported pack templates
  stay as an import plus the lock.
- Upgrading a pack works after a sync (0003 S3).

S4. **No deletes on errors.**
- Only a confirmed not-found removes a constraint from the branch. Other list
  errors are retried and reported (0003 S4).
- Swallowed errors are logged and surfaced (0003 S5).

S5. **The spec-time gate honors** `Repository.spec.policy.disabled`, the
enforcement cap and overrides (0003 G1). It also carries deltas and members
(0003 G3).

S6. **Change-scoped gates (baseline).**
- Both gates evaluate the base (the SpecChange's base commit, or the
  pre-delta specs) and the head.
- A deny blocks only when it is new: present at the head, absent at the base,
  keyed by constraint, object and site.
- Pre-existing violations are reported as `inherited`, as warn.
- `--no-baseline` / `spec.policy.baseline: none` restores whole-repo gating
  (0003 G2).

S7. **CLI restore** defaults to `--prune=false` (0004 bug 2).

S8. **`policy test` is read-only.** It does not rewrite `dist/`, the
catalogue or the lock, and it does not commit when run against a branch.
`policy build` does those things (0003 e).

### Track S status

Each item, its commit and its test. All commits are on `policy-fix-s`.

| item | commit | test |
| --- | --- | --- |
| S1 | `fcda4df` | `factory/specd/policy_test.go`: `TestPolicySyncActionPicksTheSideThatMoved` (the decision table: only the side that moved is written, both-apart is a conflict). The status carries `kcpFingerprint` beside `policyCommit` as the recorded base. |
| S2 | `128e7ef` | `impl/policykcp/policykcp_test.go`: `TestReadAndPruneAreScopedToOneRepository`. Objects carry `specs.publicdomainrelay.dev/repository`; `Read(ReadOptions{Repository})` and `prune` filter on it; the CLI grows `--repository` on `apply` and `ls`. |
| S3 | `fcda4df` | `factory/specd/policy_test.go`: `TestPersistPolicyKeepsImportedTemplatesAsImports`. The branch library's `Imported` map carries across to the kcp library, so persist skips what the pack contributed. |
| S4 | `128e7ef` | `impl/policykcp/policykcp_test.go`: `TestReadReportsANonNotFoundListError` and `TestReadTreatsANotFoundConstraintListAsNoConstraints`. Only a confirmed not-found is skipped; the read retries once; the sync logs and sets a condition instead of dropping the error. |
| S5 | `baf3077` | `impl/policyeval/specgate_test.go`: `TestCheckSpecsHonorsDisabledAndTheEnforcementCap`, `TestCheckSpecsHonorsAnOverride`, `TestCheckSpecsReportsAMemberItCannotResolve`. Deltas apply in sequence in `factory/specd/specgate.go`; `CheckSpecs` builds the model through `BuildEvaluationModel` so members merge. |
| S6 | `baf3077` | `abc/policy/baseline_test.go` (`TestDecideBaseline*`) and `impl/policyeval/specgate_test.go` (`TestCheckSpecsBaseline*`, including `TestCheckSpecsBaselineComparesAgainstThePreChangeSpec`). The realize half reads the base commit from a temporary detached worktree and is exercised by the live gate tests. `spec.policy.baseline: none` and `specd --no-baseline` restore whole-repo gating. |
| S7 | `128e7ef` | `runPolicyRestore` defaults to `--prune=false`, matching the controller. |
| S8 | `dbc49e8` | `cmd/specctl/policy_test.go`: `TestPolicyTestLeavesThePolicyDirectoryAlone` (fails before the fix: it rewrote `CATALOGUE.md`, `lib/specd.rego` and `dist/`). |

### Track S limits

- The offline `specctl policy eval --specs-only` has no base and stays
  whole-repository. Recorded in `docs/policies.md` Limits.
- The change-scope key is (constraint, object, site); a model-level violation
  carries no site, so two of the same rule on one object share a key. Recorded
  in `docs/policies.md` Limits.
- No live test lands a pre-existing violation through the realize gate with a
  second round; the inherited/denied split is unit-tested on the shared
  decision function and the base evaluation runs in the live deny tests.
  Recorded in `docs/policies.md` Limits.

## Track D: generation honesty, packaging, docs, style

Status: done. Commits `02af779` (code fixes), `ce480b1` (layering, derived
CRDs, docs digest), `c2744a4` (a bind draft reads the branch unresolved),
`a5a6c86` (comments), `079a64d` (limits), `0c126cc` (the re-run, the plan
status, the `--gator` rule), `5cd9c99` (the accepted change keeps the refused
attempts in its agent log), `0e71f0a` (a library imports the conformance
pack), `e0100ab` (merge of origin/main, track S), `4bfab7c` (comments the
merge brought back) and `382d072` (track S's policykcp tests pass the CRD
builder).

D1. **Bind does not see the answer.**
- Bind mode prompts carry the model built **without** the branch's existing
  binding: no vocabulary, globs or roles from it. `02af779`.
- The harness runs in a scratch directory that does not contain hydradb's
  `examples/`. `02af779`; the run is masked further because specd does not
  confine the harness -- recorded as a limit in `docs/policies.md`.
- Re-run the DeepSeek bind on atproto-market and report honestly how close
  the result is to the hand-written binding, field by field (0003 D1, D2).
  Run at `7a2e9d9`, first attempt, byte-identical; `c2744a4` was needed for a
  branch that holds only the pack import to be readable at all. The run found
  two further copies of the answer (the main checkout, then the agent's own
  earlier transcript) before the masks closed them.

D2. **Stronger generated-policy checks.** `02af779`.

D3. **Pack format everywhere.** `02af779`, `079a64d` (the git test pulls from a
bare repository).

D4. **Docs and examples.** `02af779` (propose-interactions, `--gator` with no
suites, the worktree repository inference, the duplicate `vm.onNetwork`, the
dead `draft.summary`, the classifier note), `ce480b1` (the stale digest),
`0c126cc` (the `--gator` rule in the testing section). `--gator` over a
directory with no suites now exits 1, so `policy test --dir D --gator` is
green for `examples/policies/{atproto-market,deno-kcp,market-mini}`,
`policies/library` and both packs, and refuses by design for
`atproto-market-cross-repo`, `greenfield-market` and `hono-compute-provider`,
which carry no suites.

D5. **Style and layering.** `ce480b1` (the kcp writer takes a
`policy.ConstraintCRDBuilder`; `policy.Unstructured` moves to `abc/policy`),
`a5a6c86` (375 comment lines removed from `abc/policy`, `impl/policyeval` and
`impl/policykcp`). Limit: the pack Rego keeps its comments.

D6. **Real Gatekeeper parity.** `ce480b1`.

## Track R: done

Every item below has a test that failed before the fix. Branches:
`policy-fix-r`.

| item | commit | what landed |
| --- | --- | --- |
| R1 | `fe98e18` | a call site reads its own text; a file-level site reads a window around the call, and the model's site text and hint walk use the same rule |
| R2 | `82e63b0` | `proxied` needs a `channels/relay` term in the ProxyCommand; `market-mini` names `relay-subscriber` |
| R3 | `82e63b0` | a host acting on the guest or on an unresolved target is a reach-in; `reachInKinds` gains `net.dial` and `http.request`; `vocabulary.reachInExceptions` names what is not the guest; atproto-market resolves its PDS clients to the requester role |
| R4 | `b02bf70` | intra-file constant propagation and named-import resolution; `fixtures/effects-aliases` with hand labels, recall 1.000 |
| R5 | `58f155e` | `Carries` unions on merge; a trigger needs a real node that contains the leaf; `term_in` and `containsFold` match whole tokens |
| R6 | `80ae841` | `common/glob` is the one doublestar dialect, `lib.specd` passes `["/"]`, and a conformance table runs through both |
| R7 | `ee36a47` | an index is read only when a stamp proves it fresh; otherwise it is rebuilt over a copy, never in a checkout, unless `--index-in-place` |
| R8 | `f21cd9c` | the diff parser counts the hunk its `@@` header declares and reads headers only outside it; `diff --git` names the file, so a rename is reported |
| R9 | `2d1345e` | the review's variants are pack suite cases and `fixtures/market-mini/violating-v{b,c,e,h1,h2,h3}`, each asserted to deny by `TestViolatingVariantsDeny` |

Measurements after the track, at the documented commits:

- recall on the G2 hand labels is unchanged: total 42/0/45, `0.483`;
- recall on the alias labels is 1.000 for `ssh.connect` and `net.dial`;
- the atproto-market three-ref eval is unchanged: 6 deny at `7a2e9d9`, 2 at
  `d20070c`, 2 at `ffac22e`, the same constraints at the same sites.

Limits recorded in `docs/policies.md`: the classifier resolves names it can
see (a runtime argument and a constant of another Go package stay unresolved);
a real trigger chain longer than three hops is reported as no path rather than
followed; a reach-in whose target a binding does not name is a false positive
until the binding names it.

Also fixed on the way: `Makefile` did not rebuild `bin/specctl` when the
embedded policy library or a pack template changed, so a policy build rewrote
the stale library over an edit.

## Order

R, S and D run in parallel worktrees. R1 to R3 and R9 come first, because
they decide whether the two example rules hold.

After all three tracks merge:

- re-run the atproto-market and deno-kcp policy runs;
- update the docs tables;
- re-run the DeepSeek analysis and the Opus review on the result.

## User corrections (2026-10-05)

U1. **Scope.**
- No more porting of opa-first-stab rules; `policies/library` (7 rules) stays
  opt-in only and is not seeded into example runs by default.
- `rfp-guest-isolation` holds only the user's two rules. The two provenance
  templates move to a separate opt-in pack.

U2. **Relay means "not a direct connection".** Any indirection counts as a
relay: the org relay, fedproxy, websocat, iroh/dumbpipe, any tunnel or
overlay. `RfpRelayOnlyGuestSsh` denies only direct connections:
- ssh with no ProxyCommand to a guest address;
- a ProxyCommand that only dials the guest (`nc`/`ncat`/`socat TCP:`/
  `/dev/tcp`/`ssh -W host:port` to the guest);
- a `net.dial` from a test to a guest.

The vocabulary `channels/relay` becomes an optional naming hint, not a
requirement. This replaces R2's "unnamed tunnel denies".

U3. **Rule 2 is enforced on the delta.**
- The gates block only new violations (S6). Pre-existing ones are
  `inherited`.
- The bidder's host-emitted `vm.onNetwork` carrying the provisioned IP is a
  known, accepted pre-existing violation. That IP may be a public IPv4 the
  client can judge, so it is **not** to be fixed.
- Phase I's first retry removed it under whole-repo gating. That run was
  stopped before any push and will be re-run with change-scoped gates.

U4. **Find existing violations and decide.** A documented, demoed workflow:
1. List the inherited violations of a repository.
2. For each one, either fix it (turn it into a SpecChange) or waive it with a
   recorded reason (a durable, site-scoped exception on the policy branch,
   reported as `waived`).

The onNetwork IP is the demo's waiver example.

### User corrections: status

All on `policy-u`. R2 is superseded by U2: the relay rule no longer requires a
vocabulary term, and the limits it recorded in `docs/policies.md` and in the
pack README are rewritten.

| item | commit | what landed |
| --- | --- | --- |
| port | `6e0fac9` | `fix(sync)`: `MigrateDeclared` does not rewrite a bare interface name onto one the spec already declares (policy-i `7f99527`), with its test |
| port | `a39ac48` | `fix(policy)`: `policy test --gator` finds gator on `PATH` and names every candidate it looked at (policy-i `8e80b41`), with its two tests |
| U2 | `6801724` | `RfpRelayOnlyGuestSsh` and the concrete `relay-only-ssh` template deny only a direct connection; `channels/relay` leaves the pack's required vocabulary; `chisel` and an unresolved proxy command pass, `nc <guest> 22` still denies, `iroh connect` is an allowed case in the pack, the examples and the opa tests. R9's variants needed no flips: vB, vC and vE are direct connections (no proxy, an argv0 constant, `ProxyCommand=nc <guest> 2222`), and the relayed ssh beside them in vB and vC passed under R2 as well |
| U1 | `04d61ae` | `rfp-guest-isolation` v2 keeps the three isolation rules; `rfp-provisioning-provenance` v1 is the opt-in pack the two provenance templates moved to; every importing binding bumps to v2 and relocks, none imports the new pack; `docs/policies.md` states that the library and every pack are opt-in |
| U3 | `fd7a900` | `impl/realize/policy_baseline_test.go` drives `runPolicyGate` on the violating market-mini worktree: an unrelated commit leaves the pre-existing reach-in and emission inherited, an added host reach-in is denied. Verified on the real repository: a fresh `/home/johnandersen777/policy-u-work/atproto-market` clone at pre-iroh `d20070c` plus an unrelated commit reports `0 new, 2 inherited` through `policy eval --inherited`, the bidder's host-emitted `vm.onNetwork` among them |
| U4 | `4030d39` | durable `exceptions/<key>.yaml` on the policy branch, honoured by the offline evaluation, both gates and the audit (reported as `waived`, never dropped); `policy findings`, `policy eval --inherited`, `policy waive <key>`, `policy fix <key>`; `policy fix` produces the SpecChange request and `impl/realize/policy_fix_test.go` drives it through the scripted agent and the gate again |
| U4 docs | `b072999` | `docs/policies.md` "Find existing violations and decide" and its limits; the `atproto-market-policies.md` demo on the fresh clone (list, waive the onNetwork IP, re-run) and the U2 rewrite of its `relay-only-ssh` non-vacuity proof, re-measured; `scripts/example-policy-findings.sh`; `impl/realize/policy_fix_test.go` |
| on the way | `52a0803` | the Repository and SpecChange CRDs (and the derived APIResourceSchemas) carry `waived` and `key`, which the live suite caught as `unknown field "status.policy.violations[0].key"`; the live bind scenario and its PolicyChange pin `rfp-guest-isolation@v2` |
| on the way | `3f44ab2`, `daca7b5`, `8b6969d` | an unparseable `--expires` is refused rather than read as permanent; the last `v1` and allowed-list references follow the split; the provenance pack's measurement is re-run on the split |

U4's durable waivers are site-scoped by key, not by constraint like an
acceptance override, but they reuse `policy.Override`, so `Decide`,
`DecideBaseline` and the audit's waived reporting share one code path. An
exception that has expired is dropped and reported, never silently honoured.

## Round 2 (review 0006): done

Branch `policy-r2`, on top of the `policy-i2` merge (`539bbbd`, which brings
`e12560a`: a violation's key is its declaration, not its line).

| item | commit | what landed |
| --- | --- | --- |
| N1 | `e65167d` | `relay_carried` is gone from both rule-1 clauses: the exemption comes from the ssh's own arguments. Pack suite cases and `fixtures/market-mini/violating-vn1-{callee,comment}` deny the review's two runs |
| N2 | `71e4e6d` | a violation with no site keys on the clause that reported it and the facts it reported; the realize gate grows the reviewer's line-shift case over the three violating fixture files |
| N3 | `12e3c0e` | exception files decode strictly, an unknown field is refused, a site-scoped exception needs its key, a rule-wide one says `scope: rule`, and a key matching no violation is reported stale |
| N4 | `d133dad` | the classifier reads the ssh family (scp, sftp, autossh, sshpass, absolute paths, shell strings, `node:child_process`), the destination, `ProxyJump`/`-J` and `-F`, and resolves a constant a template literal interpolates. The rule decides on the effect's own arguments: direct when the ProxyCommand's destination is the guest, the ssh's own target, `%h` or unresolved; a hop, a SOCKS proxy or a config is a relay. Nine violating fixtures and `compliant-relays` pin both halves |
| N5 | `d133dad` | the test-dial clause reads the same way: a dial to the guest or to an unresolved target denies unless `reachInExceptions` names it, so review 0003's vD (`violating-vn5-test-dial`) denies on real code |
| N6 | `d133dad` | a shell string whose command is an ssh or a container command, and the same through `node:child_process`, are classified by their embedded argv0 (`violating-vn6-{sh-container,child-process}`) |
| N7 | `2118a75` | both offline eval paths read the library's exceptions through the gates' `Waivers`: the text report prints `waived`, `-o json` carries the waived keys, `--strict` fails only on a deny that survives them |
| N8 | `1018a3f`, corrected by `c019eb7` | a new `file.read` effect kind, and market-mini's concrete rule denies an emitter a host source reaches (`hostSourcePatterns`), which is the review's DHCP run. The pack cannot carry the check: reachability over the call graph denied atproto-market's compliant route handler, so the pack's half is recorded as a limit |
| N9 | `fc821a9` | `policy fix --apply --system-context S --spec-hash H` builds and creates the SpecChange (prompt, constraint and site on its annotations); `--dry-run` prints it, `--write` writes it |
| N10 | `88083f3` | the bind leak: a test over a perturbed fixture proves the request carries an empty binding and the perturbed repository's own paths, and fails when the bind branch is made to keep the binding. The live re-run on a perturbed clone is not part of round 2; the remaining copy path is the unconfined harness, recorded in `docs/examples/atproto-market-policies.md` and in the limits |
| low | `1840cbe`; `e134d54` fixes a `--diff-base` the documented deno-kcp run could not resolve | A3 stale inlined lib, G6 (one `GateLock`, guarded `inventory[:1]`, the change source's `TestGlobs`), D3 (the mutation check reads the generated rule only), B9 (a spec-time clause for rule 1), `policy build` prunes a deleted template's dist, `pack.yaml` says two rules, `abc/policy`'s comment lines removed |

The low items that are recorded rather than fixed: D2 (the harness is not
sandboxed by specd) and A2 (specd evaluates the derived kinds, not an
in-cluster Gatekeeper) stay as they were; both already have a Limits entry.

## Round 2 (review 0006)

`docs/reviews/0006-policies-follow-up.md` re-checked everything above on
`1805860`.

- **N1.** An ssh is exempted only by its own arguments, its ProxyCommand or
  ProxyJump, or an ssh config alias. Nearby relay words never count.
- **N2.** The key survives line shifts: file + enclosing declaration + effect
  kind/attrs. Phase I's `e12560a` on `policy-i2` is the start.
- **N3.**
  - Strict exception files: unknown fields are refused.
  - `key` is required unless a waiver says `scope: rule`.
  - A violation without a site never shares a key.
  - A waiver whose key matches nothing is reported as stale.
- **N4.** "Direct" is decided by target, not tool name.
  - Direct: the ssh family (`ssh`, `scp`, `sftp`, `autossh`, `sshpass`,
    absolute paths, `sh -c` wrappers) reaching a guest-role or unresolved
    target, or a ProxyCommand that only dials the target (`nc` variants,
    `socat TCP*:`, `openssl s_client`, `/dev/tcp`, sockets in a script).
  - Not direct: `-J`/ProxyJump/`-W` through a jump host, a relay or SOCKS
    hop, and ssh to non-guest hosts.
- **N5.** A test's `net.dial`/`Deno.connect` to a guest-role or unresolved
  target denies on real code. The review's vD becomes a fixture.
- **N6.** `sh -c` strings and `node:child_process` are classified for host
  reach-ins.
- **N7.** Waivers apply in plain `policy eval`. `--strict` ignores waived
  violations, as docs/policies.md says.
- **N8.** Rule 2b also checks provenance where the model can show it: the
  emitted address comes from the guest report's payload. Where it cannot,
  that is recorded as a limit.
- **N9.** `policy fix` creates the SpecChange (with `--dry-run` to only
  print).
- **N10.** The bind leak is investigated again. Either prove the binding is
  derived (a different repository, or a perturbed copy) or record it.
- **Low items:** vD/B9/A3/G6/D3 from 0003; the `pack.yaml` text "three rules"
  becomes "two rules"; the 55 new comment lines in `abc/policy` are removed.

## Found in phase I (2026-10-05): a specd killed mid-realize deadlocks the queue

The phase I retry of `atproto-market` (the run in
`docs/examples/atproto-market-iroh-pr.md`, "Round with policies (PR #2)") lost
about 25 minutes to a recovery defect: the previous `specd` was killed
mid-realize, three `SpecChange`s were left `Running`, a restarted `specd` never
re-drove a `Running` change, and the per-repository batch queue behind them
deadlocked. The operator marked them `Failed` by hand with the message the
restore path uses before the queue moved.

The fix is `docs/plans/0010`'s track R/S/D work applied to the controller
itself, on branch `fix-running-recovery`:

- A change in flight records its driver: `status.owner` (a token unique to the
  specd process), `status.ownerPid` and `status.ownerStartedAt`. `SpecToCode`
  records it in the same patch that sets `Running`, `CodeToSpec` in its
  summarize patch, and a `PolicyChange` claims `Drafting` (or `Evaluated` while
  it applies) the same way, so there is no in-flight window without an owner.
  Both CRDs (and the derived APIResourceSchemas) carry the three fields.
- On startup, before the workers, and on every reconcile of a `Running`
  change, specd treats a change whose owner is not this process and whose pid is
  gone (or that has no owner at all, from before this fix) as orphaned.
- Recovery removes the stale `specd-worktree-*` worktree that still holds the
  realize branch (`git worktree add` refuses the branch otherwise:
  `cannot force update the branch ... used by worktree`) and the branch, marks
  the orphan `Failed` with `specd restarted mid-realize`, and creates the next
  attempt `Pending` with the attempt counted, so the cap still applies and the
  queue moves. Only worktrees under a `specd-worktree-*` temporary directory are
  touched.
- A `PolicyChange` whose owner is gone has its claim released and is drafted
  again; its `Drafting` and `Testing` phases were already re-driven by the
  normal reconcile. The acceptance steps run inside the realize, so a specd
  killed during one leaves the change `Running` and the same recovery re-drives
  it; the acceptance process itself is not signalled and can outlive the specd
  until its own timeout (recorded as a limit).

Tests:

- `impl/procowner/procowner_test.go`: the owner decision table (own token,
  another dead token, another live token, no token) and pid liveness.
- `impl/gitrepo/gitrepo_test.go`: `Worktrees` and `PruneWorktrees`.
- `factory/specd/recovery_test.go`: a `Running` change with a dead owner is
  requeued with the next attempt and the reason; a change with no owner is
  recovered; a change owned by this process or by another live process is left
  alone; the attempt is counted and the episode name kept; the stale worktree
  and branch are pruned and the branch accepts a fresh worktree; the startup
  sweep handles both a `SpecChange` and a `PolicyChange`.
- `test/e2e/recovery_live_test.go`, live against a private kcp: a scripted
  agent realizes a change while a scripted verify blocks, so the change is
  `Running`; a second spec edit queues behind it; specd is SIGKILLed; a fresh
  specd starts, fails the orphan with the reason, re-drives the episode, and
  both changes land.
