# Review 0004: policies analysis (DeepSeek)

DeepSeek analysis of plans 0008 and 0009 against main at d79f3d0. Plan 0010 implements the recommendations.

# Policies: plans 0008 and 0009 against main

Read-only analysis of `docs/plans/0008-policies.md` and
`docs/plans/0009-portable-policies.md` against the code at `d79f3d0`
(HEAD, branch `main`, working tree clean apart from the untracked
`tmp_regocheck/`).

Nothing was edited, committed or pushed. Scratch work went to
`<tmp>`.

## What was run (verification, not reading)

| check | result |
| --- | --- |
| `go build ./...` | clean |
| `gofmt -l .` | clean |
| `go vet ./...` | clean |
| `go test ./... -short -count=1` | all packages ok |
| `SPECD_REQUIRE_LIVE=1` policy e2e (`TestPolicy*`, `TestPortablePackGatesGreenfieldSpecs`, `TestSpecctlUpDiesWithItsStarter`) | 12/12 PASS, 120 s |
| `specctl policy test --dir policies/packs/rfp-guest-isolation --gator` | 83/83 unit, 14/14 suite |
| `specctl policy test --dir policies/library --gator` | 86/86 unit, 18/18 suite |
| tutorial: `init` (plain / `--with-library` / `--from` / `--path`), `new`, `build`, `test`, `--gator` | all pass |
| `policy eval` market-mini (fresh copy) | compliant 0 denies, violating 7 denies |
| `policy eval --repo atproto-market` at `7a2e9d9` / `d20070c` / `ffac22e` | 6 / 2 / 2 denies (matches docs) |
| `policy eval --repo deno-kcp --commit dc4c717e --diff-base main --library policies/library` | 7 denies, `security-disabled-verification` (matches docs) |
| G5 proof A (`atproto-market` + `hono-compute-provider` member at `fb11e74`) | reproduced: `host -> guest` at `hono-compute-provider/lib/compute-provider-local/mod.ts:455` |
| G5 proof B (`hono-compute-provider` alone) | reproduced: 2 denies, one real `rfp-host-reach-in` at `:455` |
| G5 proof C (`--specs-only --strict`) | clean exits 0, host-initiated exits 1 |
| pack pin: flip one hex of `policies.lock` | build refuses, `--relock` rewrites (exactly as docs) |

## Plan 0008, item by item

### A. Engine, CodeGraph, offline CLI -- DONE

- A1 engine and pins: `go.mod:7-9` holds
  `open-policy-agent/frameworks/constraint v0.0.0-20260928232141-53076f8d5ce5`,
  `gatekeeper/v3 v3.23.1`, `opa v1.21.0` -- exactly the versions the plan
  names. Conformance half: `impl/policyeval/conformance_test.go:45-123`
  (`TestExampleSuitesAgreeWithGator`) runs every suite under `examples/`,
  `policies/*`, `policies/packs/*` and `testdata/*` through both engines and
  compares case for case; `SPECD_REQUIRE_GATOR=1` makes a missing gator fatal
  (`:51-57`).
- A2 `abc/policy`: `types.go` (Template/Constraint/Report/Violation/Enforcement),
  `match.go:13-14,26-33,119-145` (scope, namespaces, excluded namespaces,
  labelSelector, namespaceSelector, name glob), `branch.go:12-14,55-74`
  (`open-policy/`, `Resolve` reuses `oabranch.Slug`, `RefFor`, `RepositoryOf`),
  `gate.go:16-33,45-51,67-95` (cap + one-shot override + `Decide`),
  `codegraph.go`, `diff.go`. `ConstraintAPIVersion` is
  `constraints.gatekeeper.sh/v1beta1` (`types.go:326`) as the plan decided.
- A3 `impl/codegraphfacts/codegraphfacts.go`, `impl/policyeval/{engine,suite,opatest}.go`,
  `impl/policygit` all present.
- A4 `impl/policyeval/lib/specd.rego`: 81 rules, `specd_test.rego` 51 tests.
  All the A4 helper names exist (`code_graph` at `:51-53`, `contexts` `:65`,
  `repository` `:47`, graph walks `:182-235`, effects/model `:252-357`).
- A5 command tree `cmd/specctl/policy.go:43-73` (`init|new|build|test|eval|effects|model|generate|bind|accept|changes|apply|ls|report|restore`);
  `scripts/install-policy-tools.sh` pins opa v1.21.0 / gator v3.23.1 by sha256
  (`:10-11`, `:29-40`).
- A6 `fixtures/market-mini/{compliant,violating}` and
  `examples/policies/market-mini` exist; the fixture test passes.

### B. The two example policies and the real run -- DONE

- B1 `examples/policies/atproto-market/` holds `relay-only-ssh` plus
  `guest-report-reach-in`, `guest-report-driven-emission`,
  `guest-report-driven-onnetwork`, `guest-report-cloud-init` (templates,
  constraints, suites, inventories).
- B2/B5 results reproduce. Current run of the library (which now also imports
  the pack) prints 6 denies at `master`, 2 at `pre-iroh` and 2 at the spec
  branch, exactly the numbers
  `docs/examples/atproto-market-policies.md:271-275` states.
- B3 `docs/policies.md` (1346 lines) covers the model, layout, lib reference,
  writing, testing, evaluating, limits, troubleshooting.
- B4 `scripts/example-policies.sh` and
  `docs/examples/atproto-market-policies.md` exist.
- B5 the follow-up constraint `guest-report-driven-onnetwork` and the
  `definition_node` handler fix are in place.

### C. kcp integration and the gate -- DONE

- C1 `deploy/crds/gatekeeper/templates.gatekeeper.sh_constrainttemplates.yaml`;
  `deploy/crds/specs.publicdomainrelay.dev_policychanges.yaml`;
  `common/specapi/specapi.go:21,79,119,123` (kind, `PolicyValid`,
  `PolicyDenied`, `PolicyDeniedAtSpec`); `abc/spec/types.go:87`
  (`RepositoryPolicySpec`), `:115` (`PolicyStatus`), `:351` (`EnforcedBy`).
  Constraint CRDs are created through the frameworks client at
  `impl/policyeval/engine.go:272-277` (`CreateCRD`), called from
  `impl/policykcp/policykcp.go:235`.
  Note: the plan says "the schema revision is 6"; it is now 8
  (`impl/schemagen/schemagen.go:13`). Stale plan text only.
- C2 `impl/policykcp/policykcp.go` has `Files:362`, `Stale:396`,
  `Distinct:441`, `Read:167`, `Apply:212`. specd restore applies without
  pruning (`factory/specd/policy.go:252`), as the plan says.
- C3 `factory/specd/policy.go:345-465` audits, fills
  `Repository.status.policy` (`:444`), sets `PolicyCompliant`
  (`:503-506`), records `enforcedBy` (`:512`) and writes the report
  (`:456`).
- C4 `impl/realize/policy.go:53` (`PolicyError`), `impl/realize/realize.go:426-444`
  (gate inside `runGates`), `abc/policy/gate.go:45` (`ParseOverride`).
- C5 the live tests exist and pass (see the verification table).

### D. Generation -- DONE (folded into 0009 G6)

The plan itself records the one deliberate deviation (mutation of the allowed
fixture instead of the harness's denied fixture). Verified: the reconciler and
checks exist, see G6 below.

### E. Library port from opa-first-stab -- DONE

- E1 `policies/library/` holds the seven templates named, with constraints,
  suites, `dist/`, `CATALOGUE.md`, `embed.go` and `library_test.go`.
- E2 `impl/codediff` + `policy eval --diff-base`. The plan records the
  `0f1078d` / 5-deny run; `docs/policies.md:802-861` now records the later
  `dc4c717e` / 7-deny run, which I reproduced. The plan text is older than
  the docs; the docs command works.

### F. Review and fix (fix list 1) -- DONE

- Item 1 keeper: `cmd/specctl/keeper.go`, `SPECD_DIE_WITH`,
  `kcpproc.RecordSpecd`/`Leaks`; proof test
  `TestSpecctlUpDiesWithItsStarter` PASSes here.
- Item 2 indexer limit: `docs/policies.md:1224-1248` ("The indexer emits
  declarations, not bodies").
- Item 3 `specGateLibrary`: verified at `factory/specd/specgate.go:71-84` --
  `--policy-library` is an override, otherwise it calls `loadPolicyGate`.
  **The fallback the plan claims does exist.**
- Item 4 unnamed transport: `policies/packs/rfp-guest-isolation/README.md` and
  `docs/policies.md:1273-1285`.
- Item 5 recall table: `docs/policies.md:1251-1272`.

### H. Retry deno-kcp#1 -- DONE

`examples/policies/deno-kcp/` holds 8 templates and 8 constraints (the seven
library templates plus `relay-only-ssh` re-bound);
`docs/examples/deno-kcp-pr.md` and `scripts/example-deno-kcp-pr.sh` exist and
the doc records PR #2.

### I. Retry atproto-market#1 under policies -- MISSING

No step of phase I carries a Done marker (contrast H, where every step does),
`docs/examples/atproto-market-iroh-pr.md` has Rounds 1-3 from plan 0006 but no
"with policies" round, and there is no `scripts/example-atproto-market-iroh-pr.sh`
run recorded. The baseline half of I.1 is available (B2 and G4 results) but
nothing was retried or published.

## Plan 0009, item by item

### G1. Effects -- DONE

`abc/policy/effects.go:13-23` fixes the vocabulary (11 kinds);
`abc/policy/effectmatch.go` holds the matchers and `Compile`;
`impl/effects/effects.go` with embedded `packs/typescript.yaml`, `go.yaml`,
`shell.yaml` and `--classifiers`/`<worktree>/classifiers/*.yaml`;
`specctl policy effects` with `--kind`, `--no-extras`, `--classifiers`
(`cmd/specctl/policy.go:1034-1050`). `scripts/effects-calibration.py`,
`scripts/effects-recall.py` and `testdata/effects-recall/atproto-market-master.yaml`
exist. The plan's own remaining gaps (comments/strings, shell not indexed,
computed argv0, the 256 KiB cap) are all still true in code.

### G2. Model and bindings -- DONE

`abc/policy/model.go` (`ComponentsWithRole`, `EffectsOfComponent`,
`FlowsWhere`, `TriggeredBy`, `Declared`, `Observed`), `binding.go`
(`Binding` = roles + vocabulary + imports; `RoleBinding` with `targets.attrs`),
`modelbuild.go` (`BuildModel`, flow resolution, triggers bounded at 3 hops /
4096 nodes, declared/observed merge), `abc/policy/modelbuild_test.go`.
`lib.specd` has the model helpers (`:252-357`, `:459`) and 51 unit tests.
`specctl policy model` works; `--propose-interactions` works on a real fixture.

### G3. Declared interactions -- DONE (three acknowledged remainings)

- Schema: `SystemContext.spec.interactions` in `abc/spec`, the CRDs
  (`deploy/crds/specs.publicdomainrelay.dev_systemcontexts.yaml`), the
  APIResourceSchemas, the delta golden `testdata/delta/interactions-edit.json`,
  and the CLM core (`clm/core/context-doc.ts:84-93`).
- Conformance pack `policies/packs/conformance/` with the three templates.
- `policyeval.CheckSpecs` (`impl/policyeval/specgate.go:44`) called from
  `factory/specd/specgate.go:22`; `specctl policy eval --specs-only --strict`
  verified (clean 0, host-initiated 1).
- `fixtures/greenfield-market` and `test/e2e/policy_gate_live_test.go` and
  `policy_portable_live_test.go` pass.
- Remaining (the plan states all three): the conformance pack has no
  `pack.yaml` so it is not importable; the drafting prompt does not receive
  observed flows; interactions are not seeded from an arch document.

### G4. Packs -- DONE

- Format: `policies/packs/<name>/pack.yaml` with `roles`, `vocabulary`,
  `parameters` (`abc/policy/pack.go:23-70`); `PackManifest.Missing` is enforced
  at build (`impl/policyeval/packs.go:137`) -- I proved it by deleting the
  `channels/relay` class from a market-mini copy: the build refused with
  `pack rfp-guest-isolation needs vocabulary:channels/relay, the binding of
  market-mini does not declare them`.
- Import and pin: `packs.go:203-350` (embedded / `git:` / `oci:` with oras),
  `policy build` writes `policies.lock`, a changed digest refuses, `--relock`
  rewrites -- all reproduced.
- `policies/packs/rfp-guest-isolation` v1 with the five templates, suites and
  unit tests (83/83, 14/14).
- Bindings in `examples/policies/{atproto-market,market-mini,deno-kcp}` plus
  the G5 ones; `Library.Imported` keeps imported templates off the repository
  branch (`impl/policykcp/policykcp.go:362-395`).
- `TestPolicyLibrariesApplyWholeIntoKcp` passes.

### G5. Cross-project and greenfield -- DONE

- `members:` implemented end to end: `abc/policy/members.go`,
  `impl/policyeval/members.go` (clone, checkout, commit verify against
  `policies.lock`), model merge in `impl/policyeval/model.go:35-90`, pins in
  `policies.lock` (`examples/policies/atproto-market-cross-repo/policies.lock`
  carries the `fb11e74` member pin).
- The model is built in the realize gate (`impl/realize/policy.go:108`) and in
  the audit (`factory/specd/policy.go:411`), and both review the model object
  (`impl/realize/policy.go:126-137`, `factory/specd/policy.go:435-438`).
  **This claim is true in the code**, not only in the plan.
- Proofs A and B were reproduced exactly (same sites, same counts); proof C is
  the passing live test plus the offline `--specs-only` runs.

### G6. Portable generation -- DONE

- Contract: `abc/policy/generate.go` (`GenerateRequest:32`, `GenerateResult:64`,
  `Check:73`, `Generator:83`, `ForbiddenIdentifiers:91`, `Mutations:252`,
  `CheckBinding:516`); `claudecli` and `scriptedagent` implement it;
  `agentfactory.GeneratorFor:167`.
- Checks: `impl/policyeval/generated.go:96` (`CheckGenerated`), `:239`
  (`CheckGeneratedBinding`); portability via `PortabilityFindings` (`:116`).
- Reconciler: `factory/specd/policychange.go:58-191` (draft, retry with the
  refused messages as the next instruction, `recordPolicyEvaluated`,
  `applyPolicyChange`), slug-collision refusal at `:118`/`:311`.
- Bind mode and the CLI (`generate|bind|accept|changes`) present; the live
  `TestPolicyGenerateLive` and `TestPolicyBindLive` pass.
- Requirement linkage: `abc/spec/types.go:351` and
  `factory/specd/policy.go:512`, `abc/oabranch/changes.go:572`
  (`(policy-guarded)`).

### G7. Review and fix -- DONE via 0008 fix list 1

The three gaps G7 names (indexer bodies, spec-time library, unnamed transport
and the recall table) are all closed in docs and code.

## Correctness bugs

### 1. `--worktree` silently reuses a stale codegraph index (HIGH)

`codegraphfacts.Build` opens an existing `.codegraph/codegraph.db` and never
refreshes it: `impl/codegraphfacts/codegraphfacts.go:41-56` calls
`codegraphsqlite.OpenRepo` first and only calls `Ensure` (which runs
`codegraph sync`) when the database is absent
(`impl/codegraphsqlite/codegraphsqlite.go:74-77`).

Reproduced in isolation, same tree, only `.codegraph` differing:

```
$ specctl policy eval --repo market-mini --worktree <copy WITH .codegraph/compliant>
violations: 1 (deny 1, ...)
deny  rfp-guest-reports-network  ...
$ rm -rf <copy>/compliant/.codegraph
$ specctl policy eval --repo market-mini --worktree <copy>/compliant
violations: 0 ... clean
```

Any checkout that already has an index (a specd-managed repository path, a tree
the reader indexed once by hand, `fixtures/market-mini` after one doc command)
therefore yields a verdict from stale facts -- including a **false pass**.
`impl/realize/policy.go:161-176` (`buildGateGraph`) has the same shape: it
removes the index only when it created it.

Impact: `docs/policies.md:1169` and `:1182-1183` tell the reader to run
`policy eval`/`model`/`effects --worktree fixtures/market-mini/...`; on this
working tree that command now reports a false
`rfp-guest-reports-network` deny because `fixtures/market-mini/*/.codegraph`
is stale (mtime 2026-10-04 20:34). The Go tests are immune because
`test/fixture/fixture.go` `copyTree` skips `.codegraph` and `.git`.

Secondary effect: the same command writes `.codegraph/` into the tree it
evaluates (untracked, but pollution of a user's checkout).

### 2. `specctl policy restore` can delete a just-landed generated policy (MEDIUM)

Plan 0009 G6: "a restore no longer prunes, so a policy that lands cannot be
deleted by a concurrent reconcile". That is true of the controller
(`factory/specd/policy.go:252` uses `ApplyOptions{}`), but the CLI path
defaults to pruning: `cmd/specctl/policy_kcp.go:283`
(`prune := fs.Bool("prune", true, ...)`) and applies it at `:309`. A hand-run
`specctl policy restore` between a `PolicyChange` apply and the branch sync
deletes the new template and constraint from kcp. User-triggered, so lower
severity than 1, but the invariant the plan states does not hold on the CLI.

### 3. `draft.summary` is a dead assignment (LOW)

`factory/specd/policychange.go:166` sets `draft.summary = generated.Summary`,
then line 167 replaces `draft` with the value returned by `checkPolicyDraft`,
which builds a fresh `policyDraft{dir, manifest}` (`:249`) and never copies the
summary. Nothing reads `draft.summary` anyway (`evaluatedMessage` at `:830`
does not use it). Consequence: on a **first-attempt success**,
`PolicyChange.status.agentLog` stays empty; it is only written on failed
attempts (`recordPolicyAttempt:649`).

### 4. Repository inference from `--worktree` depends on process CWD (LOW)

`cmd/specctl/policy.go:1219-1238` (`repositoryForWorktree` / `hasExampleLibrary`)
stats `examples/policies/<name>` **relative to the current directory**, not to
the worktree. The same absolute worktree can resolve to `market-mini` or to
`compliant` depending on where `specctl` was launched; the latter then fails
with `no policies for compliant`.

### 5. Two example bindings carry defects (LOW)

- `examples/policies/hono-compute-provider/policies.yaml:45-46` lists
  `vm.onNetwork` twice in `vocabulary.events.network-report`.
- `specctl policy model --propose-interactions` emits `peer: unknown` entries
  (e.g. `i.host-unknown-network-discovery`); pasting them produces a spec
  naming a peer that is not a context.

## Untested paths

1. `git:` and `oci:` pack sources. Implemented
   (`impl/policyeval/packs.go:228` and `:262-350`, oras with
   `org.opencontainers.image.title` layout) but no test pulls a pack from a
   repository or a registry; the oras layer layout has no unit test. The plan
   admits this in G4 and G5.
2. Freshness of `policies/packs/rfp-guest-isolation/dist/` and its
   `CATALOGUE.md` is not pinned by any test.
   `impl/policyeval/conformance_test.go:203-214` walks `examples/policies/*`
   and `policies/*` only -- `policies/packs` is a directory without a
   `policies.yaml`, so it is skipped -- and
   `TestConformancePackIsCurrentAndGreen` (`:145-146`) hardcodes
   `policies/packs/conformance`. The rfp pack's suites read
   `../../dist/<slug>.yaml` (`tests/rfp-host-reach-in/suite.yaml:7`), so a
   commit that edits `src.rego` without rebuilding `dist/` passes CI while the
   suites test the old rule. I confirmed the committed `dist/` is currently
   fresh (a rebuild into a copy diffs clean), but nothing keeps it so.
3. `shell.yaml` is exercised only by unit fixtures; no corpus carries
   standalone shell files (the plan states this in G1).
4. `policies/packs/conformance/` is not a pack: it has `policies.yaml`, no
   `pack.yaml`, so `PackManifest.Missing` and the import/pin path do not cover
   it. It is a directory a repository copies. The plan admits it in G3, but it
   means a directory named `packs/conformance` does not satisfy the G4 format.
5. Generation is checked by its own fixtures plus derived mutations; nothing
   proves the fixture models the repository's real shape (the plan states
   this). `CheckGenerated` is otherwise well covered
   (`impl/policyeval/generated_test.go`).

## Documented commands: what happens when run

All documented commands were run except those that would modify tracked files
(`policy build --dir examples/...`, run against a copy) or need a cluster
(`policy generate|bind|accept|changes`, covered by the passing live tests).

| documented command | result |
| --- | --- |
| `policy init --repo X --dir D [--with-library]` | works |
| `policy init --from DIR` | works (33 files, imports and lock kept) |
| `policy init --path R --repo X --with-library` | works, writes `open-policy/X` orphan branch |
| `policy new <slug> --kind K --dir D` | works |
| `policy build --dir D` / `--path R --repo X` / `--relock` | works |
| `policy test --dir D [--gator]` | works; `--path` is not accepted (help says `--dir`, and the plan table's `[--dir D]` is bracket notation only) |
| `policy eval --worktree fixtures/market-mini/compliant` (docs:1169) | **wrong on this tree**: 1 false deny from the stale index (bug 1). Correct on a fresh copy |
| `policy eval --worktree fixtures/market-mini/violating -o json` | works |
| `policy eval --repo atproto-market --commit 7a2e9d9 --path ... --library ...` | works, 6 denies as documented |
| `policy eval --repo deno-kcp --commit dc4c717e --diff-base main --path ... --library policies/library` | works, 7 denies as documented |
| `policy eval --repo greenfield-market --specs-only --path ... --library policies/packs/conformance --strict` | works; exit 1 on the host-initiated spec, 0 on the guest-initiated one |
| `policy model --worktree ...` / `--propose-interactions` | works |
| `policy effects --worktree ... --kind ssh.connect` | works |
| `policy build --dir examples/policies/atproto-market` (docs:441) | works on a copy; the digest printed in the doc block (`a9d815a7e877`, `files: 43`) is stale -- the current pin is `93dd9ea2d6d5`, `files: 44`. The error-message example below it is illustrative |
| `scripts/install-policy-tools.sh` | not run (bin/opa and bin/gator already present, pinned by sha256) |

## Portability: where it is not really portable

1. **Repo-specific reach-in hints.** Every binding must name the vocabulary its
   own container-exec verbs use: `guest.targets.attrs: [getNodeId]`
   (atproto-market, market-mini), `[inspectIp, backend.exec]`
   (hono-compute-provider, cross-repo). Without the hint the target role
   resolves to `unknown` and the reach-in is invisible (the plan says so in
   G4). A new repository must re-discover its own verb; the pack cannot.
2. **Extra classifier files.** `examples/policies/hono-compute-provider/` and
   `examples/policies/atproto-market-cross-repo/` need
   `classifiers/compute-provider.yaml` (`inspectIp`, `backend.exec`), so
   "only `policies.yaml` changes between them"
   (`docs/examples/portable-policies.md:5`) is not accurate for two of the
   three G5 bindings -- a repo-specific data file changes too.
3. **Org vocabulary defaults.** `default_transport_patterns` hardcodes
   `websocat|wstunnel|chisel|frp[cs]|autossh|rathole|socat|dumbpipe|fedproxy`
   and `default_install_patterns` the container/apt verbs
   (`policies/packs/rfp-guest-isolation/templates/rfp-guest-transport-provenance/src.rego:7,37`).
   These are defaults, overridable by parameters, but they encode this org's
   transports and installers.
4. **A required vocabulary.** `pack.yaml` requires the classes
   `channels/relay`, `events/network-report`, `payloads/network-info`,
   `purposes/network-discovery`, `routes/report`, and `PackManifest.Missing`
   fails the build when one is absent. A repository whose concepts are named
   differently must invent a mapping before the pack can be used at all.
5. **Roles are mandatory.** `pack.yaml` requires `guest`, `host`, `test`. A
   repository that holds only one side of the split must still declare all
   three (or bind the other side as a `member`), and there is no "this role is
   not here" declaration (the plan records this in G5).
6. **Unnamed tunnel = unchecked.** `RfpRelayOnlyGuestSsh` accepts any ssh with
   a non-empty `proxyCommand`
   (`templates/rfp-relay-only-guest-ssh/src.rego:29-33`), so a repository whose
   tunnel the vocabulary does not name is passed by the rule that is supposed
   to check it (documented in `docs/policies.md:1273-1285`, but it is a real
   portability hole, not only a doc note).
7. **Cross-repo requires the other half as a `member`.** The model is built
   from one checkout plus declared members; a split repo alone reports a false
   `rfp-guest-reports-network` (proof B, reproduced here).

Positively: the pack's rules carry no repository identifier, no path glob and
no symbol name -- I grepped every `templates/*/src.rego`, `constraint` and
`pack.yaml` for `atproto-market|market-mini|deno-kcp|createMarketBidder|runComputeContract|getNodeId|hono-*|lib/market*|dumbpipe|websocat|iroh|fedproxy`
and the only hits are the transport vocabulary defaults above.

## The ten highest-value next steps

1. **Make `--worktree` refresh the index** (`impl/codegraphfacts/codegraphfacts.go:38-60`):
   always `Ensure`/`sync` when the worktree is not a pinned commit export, or
   add `--reindex`. Fix `buildGateGraph` (`impl/realize/policy.go:161-176`) the
   same way. Then the docs commands at `docs/policies.md:1169,1182-1183` are
   reliable. (Bug 1.)
2. **Pin pack freshness for `policies/packs/*`**: extend
   `TestExampleDistAndCatalogueAreCurrent` (`impl/policyeval/conformance_test.go:203-214`)
   to walk `policies/packs/*` like it walks `examples/policies/*`, so a
   `src.rego` edit without a rebuilt `dist/` fails CI. (Untested path 2.)
3. **Do plan 0008 phase I**: retry atproto-market#1 with the gate on, publish
   the PR, and write the "with policies" round in
   `docs/examples/atproto-market-iroh-pr.md`. It is the last unstarted phase
   and it is the user's original question for that repository.
4. **Test the `git:` and `oci:` pack sources**: one test that builds a local
   bare repository (and one oras OCI layout in `bin/`) and pulls a pack from
   each; assert the resolved digest is pinned. (Untested path 1.)
5. **Give the conformance pack a `pack.yaml`** and import it like the rfp pack,
   so every pack in `policies/packs/` obeys one format and `PackManifest.Missing`
   covers it. (G3 remaining.) Check `docs/policies.md:1176`, which currently
   points `--library` at a directory that is not a pack.
6. **Close the `restore` prune hole**: default `specctl policy restore` to
   `--prune=false` (or make the controller own pruning) so the G6 invariant
   holds on the CLI too. (Bug 2.)
7. **Make the portability check bite**: `ForbiddenIdentifiers` currently bans
   the repository, its contexts and the glob literals -- add the classifier
   file names and the binding's `targets.attrs` values to what a *generated
   policy* may not depend on, and add a test that a generated rule reading
   `getNodeId` is refused. Right now only `generated_test.go` checks the
   happy shape.
8. **Seed the drafting prompt with observed flows** (0009 G3 remaining), so
   `CodeToSpec` proposes `interactions:` and the spec-time gate has something
   to read on a repository that has code but no declared interactions.
9. **Fix the `--propose-interactions` output**: drop flows whose target role is
   `unknown` (or emit them commented out). Pasting `peer: unknown` produces a
   spec that names no context. (Bug 5.)
10. **Complete the reach-in kind list**: G5 records that `pollSsh`'s
    `Deno.connect` to port 22 and `RfpGuestReportsNetwork`'s `unknown` peer are
    gaps; add `net.dial` on the guest's ssh port to `RfpHostReachIn`'s
    `default_reach_in_kinds` and a gator case for it, and add a "role is not in
    this repository" binding option so a split repo does not need a member to
    stay clean.

Smaller, worth fixing with any of the above: `--propose-interactions` peer
quality (9), the duplicate `vm.onNetwork`
(`examples/policies/hono-compute-provider/policies.yaml:45-46`), the dead
`draft.summary` (bug 3), the CWD-relative repository inference (bug 4), and the
stale digest shown at `docs/policies.md:441-461`.
