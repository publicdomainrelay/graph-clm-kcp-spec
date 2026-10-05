# Review 0003: policies (plans 0008 and 0009)

Opus review of main at d79f3d0. Plan 0010 implements the recommendations.

# Review: policies over spec-driven development (48dad68..HEAD)

Reviewer: read-only. No repo edit, no commit. All experiments ran in
`<tmp>` (rsync copy of the repo, copies of
`fixtures/market-mini/*`, adversarial variants). Every finding below was checked
in code; findings marked **[run]** were also reproduced.

## Verdict

Layering idea (facts -> ArchitectureModel -> per-repo binding -> portable pack)
is right, and the Gatekeeper usage is faithful at the template level: dist
templates load in `gator verify` (market-mini, atproto-market, conformance,
library: PASS). Main problems:

1. The two acceptance rules have large, easy false negatives. Five of six
   realistic violations I wrote passed clean **[run]**.
2. The kcp <-> branch sync is not two-way: kcp always wins, reads every
   repository's policies, and freezes imported packs. It can revert branch
   commits and copy policies across repositories.
3. Gates are not scoped to the change: whole-repo violations block every
   realize and every spec change, and the spec-time gate ignores
   `policy.disabled`, the enforcement cap and overrides.
4. The bind "generation" result in the docs is circular: the hand-written
   binding was in the prompt.

## (b) Are the two example rules enforced? Adversarial runs

Base: copy of `fixtures/market-mini/compliant` (0 violations). Each variant adds
one realistic violation; library `examples/policies/market-mini` (pack
`rfp-guest-isolation` + concrete templates).

| variant | violation added | result |
| --- | --- | --- |
| vA | test: `new Deno.Command("ssh", {args:["-p","2222","root@10.0.0.7",...]})` | 1 deny (pack rule only; concrete `relay-only-ssh` misses it) |
| vB | vA plus a second `Deno.test` in the same file that uses `ProxyCommand=websocat` | **0** |
| vC | vA via alias `const SSH = "ssh"; new Deno.Command(SSH, ...)` | **0** |
| vD | test: `Deno.connect({ hostname: host, port: 2222 })` to the guest | **0** |
| vE | test: `ssh -o ProxyCommand=nc 10.0.0.7 2222 root@guest` | **0** |
| vH1 | bidder: `container inspect <vm>` for the address, then emits onNetwork | **0** |
| vH2 | bidder: `fetch(http://${guestHost}:7000/address)` | **0** |
| vH3 | bidder: ssh over relay into guest, runs `ip -j addr` | **0** |

### B1. critical: file-level context leaks ProxyCommand and channel to every ssh in a test file [run vB]
- `abc/policy/effectmatch.go:411-421`: an `extract` that misses at the call
  site falls back to `context` = enclosing node text + callee texts
  (`effectmatch.go:1110-1125`).
- codegraph does not index `Deno.test` bodies, so the enclosing node is the
  **file** node (`effectmatch.go:539-555`, `nodeAt`). The vB model shows both
  ssh effects at lines 4 and 10 with `proxyCommand: websocat --binary ws://relay/x`.
- `rfp-relay-only-guest-ssh/src.rego:16,26-29` (`proxied`) then exempts the
  direct ssh. `modelbuild.go:728-746` (`hintText`/`siteText`) has the same
  leak for `channel`, `carries` and `purpose`: any vocabulary term anywhere in
  the file (also in comments) labels every effect in that file.
- Fix: extract attrs from the call's own argument text only. Never fall back
  to a file node. When the node is a file node, use a window of lines around
  the site. Add a vB-style case to the pack suite and to `example_fixture_test.go`.

### B2. critical: any ProxyCommand counts as "relay" [run vE]
- `rfp-relay-only-guest-ssh/src.rego:26-29`: `proxied` only requires a
  non-empty `proxyCommand` attr. `ProxyCommand=nc guest 22`, `ssh -W`, or
  `ProxyJump` to a non-relay host all pass. The vocabulary (`channels/relay`)
  is consulted only when there is no ProxyCommand, so "transports not named in
  vocabulary" are accepted, not rejected.
- Fix: require `proxyCommand` to contain a `channels/relay` term (or an
  injected transport whose symbol a binding names). Treat an unresolvable
  proxy as `warn`, not as a pass.

### B3. high: reach-in rule only sees the `getNodeId` shape the binding names [run vH1-vH3]
- `rfp-host-reach-in/src.rego:18,27-31` needs a flow `acted_on_in_role(flow, guest)`.
  The target role comes from `modelbuild.go:598-638` (`targetRoles`): handle
  path, NSID, host/route hints, symbol/attr hints, else `unknown`.
  `container inspect`, a `fetch` to the guest IP, or ssh into the guest all
  resolve to `unknown`, so neither clause fires. The concrete market-mini
  templates miss them too.
- Fix: treat `container.exec` and `ssh.connect` from the host as reach-in
  unless the binding marks the target as not a guest (deny by default for
  unknown targets). Add a binding hint for guest addresses (`targets.hosts`
  from the contract fields such as `guestHost`). Add the vH* cases to the pack suites.

### B4. high: test ssh and direct dials are missed by aliasing and by the guest-target requirement [run vC, vD]
- `impl/effects/packs/typescript.yaml:3-8`: `deno-command-ssh` needs a literal
  argv0, so an alias makes the call `proc.exec`. `ts-string-ssh` needs
  `@[A-Za-z0-9]`, so `` `root@${host}` `` is missed.
- `rfp-relay-only-guest-ssh/src.rego` (second clause) fires on `net.dial` only
  when the flow resolves to the guest. `Deno.connect` to a variable host
  resolves to `unknown`.
- Fix: resolve `const X = "literal"` in the same file (small constant table in
  the scanner). In the test role, treat `net.dial` with port 22 or
  `ssh`-like ports, or an `unknown` target, as a violation.
  Recall 0.483 (plan 0009:464) means a MUST rule should not be `deny`-only on
  effects. Pair it with a positive requirement: "every test that imports a
  driver reaches `runComputeContract`" and "no `ssh.connect` in `test/**`".

### B5. medium: flow dedupe drops payloads
- `abc/policy/model.go:118-121`: `flowKey` excludes `Carries`.
  `modelbuild.go:938-940` keeps the first non-empty `Carries`. Checked in a
  temp test: flows `[other]` + `[network-info]` merge to `[other]`. This drops
  the evidence that `rfp-host-reach-in`'s `network_flow` (carries branch) and
  `rfp-guest-reports-network`'s `guest_reports` depend on.
- Fix: union `Carries` on merge (dedupeStrings).

### B6. medium: a trigger is created when root and leaf share a node
- `modelbuild.go:1035,1056`: `reachableNodes` seeds `seen[root]=true`, so any
  `event.emit` in the same node as an `http.handle` (the same factory
  function, or the same file node for unindexed bodies) counts as "driven by
  the report handler". This satisfies `rfp-guest-reports-network` clause 2
  (`driven_by_report_handler`) falsely. Also `maxHops=3` (`modelbuild.go:17`)
  cuts real chains, which gives false positives.
- Fix: for same-node pairs, require the leaf line inside the handler's span.
  Report "no path within N hops" as a separate, non-deny result.

### B7. medium: the event class depends on the first argument's spelling
- `typescript.yaml` `atproto-create-*` extract `type` from the first argument.
  `hono-bidder/mod.ts:499` in atproto-market calls
  `createSignedRepoRecord(collection, record)`, so the type is `collection`, and
  `lib/market-bidder-compute/mod.ts:309` uses `EVENT_NSID`. An onNetwork emit
  through a variable is not `network-report`. Same aliasing gap as B4.

### B8. medium: payload terms are too generic for the guest-reports require
- `examples/policies/atproto-market/policies.yaml` `payloads/network-info`:
  `address`, `route`, `ticket`. With `containsFold` over node text plus 3 hops
  of callees (`modelbuild.go:845-886`) and `reportPeerRoles` unset
  (`rfp-guest-reports-network/src.rego:39`), almost any guest `curl` meets
  "the guest reports". The docs say the strict reading (`guest -> host`) fails
  at every ref, so the default form is the only one that passes, and it is
  close to vacuous.

### B9. low: no rule runs at spec time for example (1)
- The relay rule reads only effects. A declared interaction
  `test -> guest, channel: direct` passes the spec-time gate. Add a declared
  flow clause: an initiator in the test or requester role acting on the guest
  with `channel != relayClass`.

## (a) Design and Gatekeeper fidelity

### A1. high: Go and Rego globs disagree; `**` does not work in Go [run]
- `abc/policy/modelbuild.go:424,479,716` use `path.Match`: `hono-bidder/**`
  does **not** match `hono-bidder/src/x.ts` (checked: false). It works today
  only because atproto-market directories are flat.
- `policies/packs/rfp-guest-isolation/lib/specd.rego:427,456,526` use
  `glob.match(p, [], path)`. With an empty delimiter list the default is `.`,
  so `test/*` does **not** match `test/foo.ts` and `*.ts` matches
  `test/foo.ts` (checked with `bin/opa eval`).
  `rfp-guest-transport-provenance` and `rfp-key-material-provenance` call
  `file_in_role` (`src.rego:14`).
- Fix: use one glob dialect: doublestar in Go and `glob.match(p, ["/"], path)`
  in Rego. Add a nested-path case to the pack suites.

### A2. medium: real Gatekeeper never sees the reviewed kinds
- No CRDs exist for CodeGraph, CodeDiff or ArchitectureModel (`deploy/crds/`
  has only the 4 spec CRDs + ConstraintTemplate). Templates are valid
  Gatekeeper and pass gator. But in-cluster admission and audit can never
  match them, so the ConstraintTemplate in kcp is only storage. specd's
  in-process `frameworks` client is the only enforcer. The docs should say so
  plainly, or publish these kinds (status-only CRDs) if a real Gatekeeper is a goal.

### A3. low: `hasLib` keeps a stale inlined lib
- `impl/policyeval/engine.go:75-79` + `suite.go:133-140`: a template that
  already carries any `package lib.specd` lib (for example a kcp object whose
  lib was inlined at `policykcp.Apply`, `policykcp.go:222-224`) is evaluated
  with that old lib, not the current one.

## (a/c) kcp <-> orphan branch sync

### S1. high: kcp silently reverts branch commits
- `factory/specd/policy.go:247-271`: once `status.policy.policyCommit` is set,
  any content difference sends kcp to the branch (`persistPolicy`). There is
  no base commit and no check for which side moved. `policykcp.Stale`
  (`policykcp.go:396-424`) removes branch templates and constraints that kcp
  lacks. The code says so itself (`factory/specd/policychange.go:393-395`).
- Documented branch-side flows (`specctl policy new|build --path ...`,
  `policy init --from`) do not touch kcp (only `cmd/specctl/policy_kcp.go`
  calls `policykcp.Apply`). The next Repository resync undoes them, even when
  the user runs `policy restore` right after (the race window). Specs avoid
  this with `persist` adopt/import/conflicts (`factory/specd/persist.go`).
- Fix: record the last synced branch commit and the kcp fingerprint in
  status. If only the branch moved, apply branch->kcp. If only kcp moved,
  persist. If both moved, raise a conflict condition. Do not guess.

### S2. high: kcp->branch persists every repository's policies into this branch
- `impl/policykcp/policykcp.go:167-198` (`Read`) lists **all** ConstraintTemplates
  and constraints in the workspace. The restore path knows kcp is shared
  (`factory/specd/policy.go:248-251`), but the persist path compares the whole
  workspace with one repository's branch (`policy.go:259`) and writes the
  union to that branch (`persistPolicy`, `policy.go:322-343`). With two
  repositories in one workspace, each branch gets the other's templates,
  and the audit and both gates then enforce foreign policies. The same slug
  in two repositories gives last-writer-wins.
- Fix: label every applied object with the repository
  (`specs.publicdomainrelay.dev/repository`). Filter `Read` and prune by that
  label.

### S3. medium: persist freezes imported packs into local copies
- `policykcp.Files` (`policykcp.go:362-392`) skips imported entries through
  `library.ImportedFrom`. But a library from `policykcp.Read` has
  `Imported == nil`, so every pack template and constraint is written to
  `templates/` and `constraints/` as local files. After that, a pack upgrade
  collides in `mergePack` (`impl/policyeval/packs.go:146-157`, "already has").
  A kcp edit of a pack template does the same. Fix: carry `Imported` across
  (from the branch library) or tag pack objects in kcp with an annotation.

### S4. medium: transient list errors delete constraints from the branch
- `policykcp.go:182-184`: any error listing a constraint kind is `continue`
  (the comment says only "no such resource"). The result has fewer
  constraints, `Distinct` is true, and `Stale` removes the constraint files in
  a "sync from kcp" commit. The same happens when a constraint CRD failed to
  establish (CreateCRDError). Fix: continue only on NotFound; on any other
  error, abort the sync.

### S5. low: errors swallowed or not retried
- `policy.go:226-229`: `policyChangeInFlight` error returns with no log or condition.
- `policy.go:266-268`: re-read after persist fails silently.
- `impl/policygit/policygit.go:78-102`: `Update` returns `oagit.ErrRaced`
  (`impl/oagit/oagit.go:229-234`) and nobody retries. A raced
  `applyPolicyChange` marks the change Failed (`policychange.go:418-421`)
  after kcp has already been changed (`policychange.go:401`).
- `restore` (no `policyCommit`) runs on every reconcile until an audit
  succeeds. While audits fail, the direction is branch->kcp; after the first
  success it is kcp->branch. The direction depends on audit health.

## (c) Gate correctness

### G1. high: the spec-time gate ignores disable, cap and overrides
- `impl/policyeval/specgate.go:110`:
  `policy.Decide(report, policy.RepositoryPolicy{}, nil)`. A repository with
  `spec.policy.disabled: true` or `enforcement: warn` is still denied before
  realize, and `specctl accept --override policy:<c>` cannot waive it.
  `factory/specd/specgate.go:71-84` never reads `repository.Spec.Policy`.
- Fix: pass `repositoryPolicy(repository)` and the parsed overrides, as
  `loadPolicyGate` does.

### G2. high: the gates are whole-repo, not change-scoped
- `impl/realize/policy.go:125-141` reviews the CodeGraph and model of the
  whole head, and `Decide` blocks on any deny. There is no baseline (grep:
  none). atproto-market already has 3 deny violations at `7a2e9d9`
  (docs/examples/atproto-market-policies.md), so every realize there fails
  after `MaxAttempts`, and the agent gets messages about code it did not touch.
- The spec gate (`factory/specd/specgate.go:22-65`) has the same problem: one
  bad context blocks every batch, and the denial is written to all members
  (`realspec.go:153-155`).
- Fix: evaluate base and head and block only on violations whose ID is new
  (`policy.ViolationID` already exists). Report the old ones as `warn`.

### G3. medium: the spec gate loses deltas and members
- `factory/specd/specgate.go:43-53`: `applied[ctx] = delta.Apply(realized, d)`.
  A second member for the same context overwrites the first. Apply the deltas
  in sequence.
- `impl/policyeval/specgate.go:81-85` builds a model with no members, and
  `:95-99` uses a synthetic Repository with `Branch: "main"`. A cross-repo
  library (`examples/policies/atproto-market-cross-repo`) sees a different
  model at spec time than at realize time.
- `specgate.go:132-138` (`marshalSpecObject`) drops a marshal error and
  returns nil.

### G4. medium: CodeDiff parser mistakes content lines for headers [run]
- `abc/policy/diff.go:55-63` checks `--- ` and `+++ ` before hunk state. A
  removed `-- comment` line (SQL, Lua, Haskell) appears as `--- comment` and
  ends the hunk. Checked: in the next added lines, `new Deno.Command("ssh")`
  disappeared (`added 0`). A content line `+++ ...` replaces `current` and
  loses the previous file's lines. Fix: count hunk lines from the `@@`
  header and parse headers only outside a hunk. Also handle `rename from/to`.

### G5. medium: eval reuses a stale `.codegraph` index [run]
- `impl/codegraphfacts/codegraphfacts.go:38-60` opens any existing index with
  no freshness check. Node text and lines come from the old index, and file
  texts come from disk. In the working tree,
  `fixtures/market-mini/compliant/.codegraph/codegraph.db` (Oct 4 20:34,
  gitignored) is older than the sources (Oct 5 01:17), and the documented
  `bin/specctl policy eval --worktree fixtures/market-mini/compliant` reports
  **1 deny** (`rfp-guest-reports-network`). After the index is deleted: 0.
  The audit (`factory/specd/policy.go:357`) and `repositoryModel`
  (`policychange.go:579`) use the same path.
- Fix: store the indexed commit or mtimes in the index, and re-sync or
  rebuild on mismatch.

### G6. low: other gate problems
- `repositoryModel` (`policychange.go:579-586`) builds without `TestGlobs`, so
  the model the harness sees has no `file.test` flags, unlike the audit and the gate.
- `checkPolicyDraft` `inventory[:1]` (`policychange.go:250`) assumes a non-empty slice.
- `gateLock` is duplicated (`factory/specd/policy.go:165`, `impl/realize/policy.go:146`).

## (d) Can bind or generate cheat?

### D1. high: the bind prompt contains the answer
- `factory/specd/policychange.go:129` builds the model with the **branch's
  current binding**. `renderModel` (`:796-802`) is the full YAML, and
  `ArchitectureModelSpec` carries `vocabulary` (`abc/policy/model.go:80`)
  and each component's `globs` (`model.go:32`). `writeBindingPrompt`
  (`impl/claudecli/generate.go:96-100`) puts that model in the prompt.
- The documented run seeded the branch from
  `examples/policies/atproto-market` (docs/examples/atproto-market-policies.md:320-325),
  so the harness was given the hand-written roles, globs and vocabulary. The
  claim "The binding it wrote is the hand-written ... role for role"
  (`:477-488`) shows that it copied them, not that it can derive them.
- Fix: in bind mode, build the model with an empty binding (contexts only,
  `roles: []`, no vocabulary), and strip `vocabulary` and `globs` from the
  rendered model. Re-run on a branch that holds only the pack import, and
  score the result against the hand-written binding.

### D2. low: the harness is not sandboxed
- Working dir: `$SPECD_STATE_DIR/policy-changes/<ns>-<name>`
  (`policychange.go:728-731`), default `~/.local/state/specd`, outside the
  hydradb tree. The prompt does not name hydradb or `examples/`. But nothing
  stops an agent with file tools from reading
  `/home/.../hydradb/examples/policies/atproto-market/policies.yaml`, or
  `git show open-policy/<repo>:templates/...` in the clone. Run the harness
  in bwrap or a container with only the scratch dir.

### D3. low: generated-policy checks are weak
- `mutationCheck` (`impl/policyeval/generated.go:550-592`) passes if **any**
  one mutation is denied. "head evaluation" passes whatever the count.
  `spec.apply: true` (`policychange.go:184-186`) then applies with no human
  review. The policy rule and the pack also agree at all refs because they
  read the same model with the same blind spots (B1-B4). That is not
  independent validation.

## (e) Docs commands

| command | result |
| --- | --- |
| `specctl policy test --dir examples/policies/market-mini --gator` | PASS (63 opa, 10 suites, gator) |
| same for `atproto-market`, `policies/packs/conformance`, `policies/library`, `policies/packs/rfp-guest-isolation` | PASS |
| `specctl policy test --dir examples/policies/greenfield-market --gator` (and `hono-compute-provider`) | `suites: 0/0`, gator not run, exit 0: a pass with no tests. The pack's suites are not re-run under the binding. |
| `specctl policy eval --worktree <copy of compliant> --library examples/policies/market-mini` | 0 violations |
| `specctl policy eval --worktree <copy of violating> ...` | 15 deny |
| `specctl policy eval --worktree fixtures/market-mini/compliant` (in tree) | 1 deny, wrong because of the stale index (G5) |

- `policy test` is a build: it rewrites `dist/`, `CATALOGUE.md` and the lock,
  and commits `policy(x): build` when the target is a branch
  (`cmd/specctl/policy.go:667-682`, `buildTarget` 582-660). A "test" command
  should not write. Add `--no-write`, or split the steps.
- In a gator run, `bin/gator` is found relative to cwd
  (`bin/gator not found; run scripts/install-policy-tools.sh` when run outside
  the repo root).

## (f) Layering and style

- `abc/policy` has no I/O. It imports only `context` (for the `Generator`
  interface), `regexp` and stdlib helpers. No abc -> impl arrow. OK.
- impl -> impl edges: `policykcp -> policyeval`, `policygit -> policyeval, oagit`,
  `policyeval -> effects, codegraphfacts`, `realize -> policyeval, effects, ...`.
  The org rules forbid cross-concept impl imports. `policykcp` needs only
  `ConstraintCRD` and `Unstructured`. Move `Unstructured` to `common` and pass
  the CRD builder in as a function.
- "No code comments" is not followed: comment lines in non-test files are
  210 in `abc/policy`, 118 in `impl/policyeval`, 34 in `impl/policykcp` and
  38 in `modelbuild.go` alone. The pack Rego is also commented
  (`lib/specd.rego` 26 lines).

## Prioritized recommendations

1. Fix the relay rule: attrs and hints from the call site only, not the file
   node (B1), and require a relay term in the ProxyCommand (B2). Add
   vA/vB/vC/vD/vE as pack suite and fixture cases.
2. Make reach-in deny by default for host `container.exec` and `ssh.connect`,
   and for `unknown` targets. Add guest-address target hints. Add
   vH1/vH2/vH3 cases (B3).
3. Scope kcp sync per repository (label + filter) (S2), and make the sync
   truly two-way with a recorded base and conflicts (S1). Keep `Imported`
   through the round trip (S3). Abort on non-NotFound list errors (S4).
4. Spec gate: honor `disabled`, the cap and overrides (G1). Both gates: block
   only on new violations against a base evaluation (G2).
5. One glob dialect in Go and Rego, with `**` support (A1).
6. Detect a stale `.codegraph` before every eval, audit and gate (G5).
7. Bind mode: no binding in the prompt or model. Re-measure against the
   hand-written binding (D1). Sandbox the harness (D2).
8. Union `carries` on flow merge (B5). Fix same-node triggers (B6). Fix the
   diff parser hunk state (G4). Resolve string-constant aliases in the
   classifier (B4, B7).
9. Docs: say that only specd enforces (A2). Make `policy test` read-only.
   Fail when there are 0 suites, or re-run the pack suites under the binding.
10. Remove comments and duplicated helpers (`gateLock`). Move `Unstructured`
    to `common`, which cuts impl->impl edges.
