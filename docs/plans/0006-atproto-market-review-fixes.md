# PLAN 0006 - review 0002 fixes: atproto-market#1 works, and the tool sees TypeScript

Status: done. Owner: coordination agent. Executors: headless `deepseek-claude -p`.
Source: `docs/reviews/0002-atproto-market-iroh.md`.

## F1 - hydradb (recommendations 5-8, plus what the review implies)

1. CHANGES.md always has a baseline. When no default-branch architecture branch
   exists, `specctl up` on a feature branch first populates and persists the
   architecture of the base code branch (`open-architecture/<repo>` or, for a
   non-default base, `open-architecture/<repo>--<base>`), then branches the
   feature's architecture off it; a branch already populated without a base
   falls back to its own last commit before the first `origin=clm` spec edit.
2. Example runs record the hydradb commit they ran (`git -C hydradb describe
   --always --dirty` into the run log and the example doc's table) and refuse a
   dirty or stale binary (binary built from a commit other than HEAD).
3. TypeScript dependencies: resolve `deno.json` workspace members and import
   maps (bare `@scope/pkg` specifiers to the member directory, relative imports
   across members) into `depends_on`; package partition is the default for a
   deno workspace (one context per member); generated code directories
   (`@atproto/lex` codegen, the repo's lexicon output) fold into their package.
4. A realize commit subject names every member context of its batch.
5. A SpecToCode that touched no file of its context, or whose diff does not
   implement an added/changed requirement, is not silently `Succeeded`:
   specd runs a requirement-coverage judgment (DeepSeek, one call per change,
   the delta plus the diff) and records per-requirement `implemented|missing`;
   any `missing` marks the change `Succeeded` with condition
   `RequirementsUnimplemented` and lists them in CHANGES.md, and the harness
   sees them in `arch_changes`. Off when no model is configured.
6. No-op spec commits and commit-id-only status rewrites are suppressed.
7. The `impl/runlock` `TestTwoLiveSuitesRunInParallel` flake: assert overlap
   deterministically (barrier), not by timing.

## F2 - atproto-market#1 through its spec flow (recommendations 1-4, 9)

Status: **done** (2026-10-04, hydradb `e9f129e`, fresh clone
`/home/johnandersen777/specd-atproto-iroh-f2`, branch
`spec/iroh-dumbpipe-20261004141803`). `specctl up` restored the 81-context
architecture from the orphan branch; 12 `SpecToCode` changes were applied and
realized as one 23-file commit (`1e1cd3c`, +1065/-234), then 3 more after the
review of that commit. `spec.verify` grew the three new suites and the
Repository gained its first `spec.acceptance` step
(`test/bidder_container_integration_test.ts`, green). Full record:
[`docs/examples/atproto-market-iroh-pr.md`](../examples/atproto-market-iroh-pr.md#round-2-after-review-0002).

Two things the flow did not do on its own, and their state:

- Requirement 4 is met as "delivered privately (no public record, no direct
  addresses)"; the accept ref that authorises the report endpoint is itself
  public, so a reader of the contract records can race the guest's first report.
  The requirement and the pull request state this as a gap; closing it needs a
  guest-held credential the cloud-init cannot carry (workload-identity /
  secrets-capability channel).
- Requirement 6 is met for the harness's own assertions (bid collection through
  real RFP + accept + cloud-init), not as an ssh proof: the container-mode
  provider still fails to provision in this environment
  (`TypeError: fetch failed` after the assertions), and the harness runs
  `skipSsh: true`.

On the PR's branch, through `specctl clm apply` only:
1. dumbpipe install extracts `./dumbpipe` from the real v0.39.0 archive (guest
   and host), with a test that extracts the real archive.
2. The guest's iroh identity persists (`IROH_SECRET` generated once, 0600
   environment file), the log truncates per start, the ticket is re-extracted.
3. The guest delivers its ticket itself through the accept bundle /
   `/v1/on-network` path, not a provider hook missing at the pinned revisions.
4. The ticket is not published in a public record: delivered privately or
   encrypted to the requester; no direct addresses.
5. Spec gaps closed: onNetwork and requestComputeVM lexicons, READMEs,
   contradictions (`r.xrpc-ingress-not-ssh`, the `/v1/on-network` requirement),
   an explicit out-of-scope note for the XRPC and secrets ingress, unit tests for
   transport selection.
6. A live check if the baseline harness can be made green (its 401
   `cannot resolve signing key` at `pre-iroh`), else stated.

## F3 - the two gaps F2 left, closed through the same spec flow

Status: **done** (2026-10-04, hydradb `516497a`, clone
`/home/johnandersen777/specd-atproto-iroh-f2`, branch
`spec/iroh-dumbpipe-20261004141803` at `ffac22e`, pushed; PR #1 rewritten). The
branch and its orphan `open-architecture/atproto-market--spec-iroh-dumbpipe-20261004141803`
branch are on origin; every change is a requirement applied with
`specctl clm apply` (full document rendered, ids diffed before applying),
realized by specd, gated by `spec.verify` and the `spec.acceptance` container
harness. Full account: [`docs/examples/atproto-market-iroh-pr.md`](../examples/atproto-market-iroh-pr.md#round-3-f3-closes-the-two-gaps-round-2-left).

**Gap 1: live ssh over dumbpipe, proven by the acceptance.** `specctl accept` on
the realized branch: `ok | 1 passed | 0 failed` in 32 s (26 s on a repeat). The
guest is born from the RFP flow's cloud-init, installs dumbpipe, reports its
ticket to the requester's `POST /v1/on-network` (200), and ssh with
`ProxyCommand=dumbpipe connect <ticket>` runs `test -x /usr/local/bin/dumbpipe &&
echo SSH_OK_VIA_IROH` inside the guest, exit 0. Four defects had to go, all
observed in the live run, none of them "the environment" (`docker info` green):

1. **The harness tore down during provisioning** -- a `Promise.race` cap plus the
   `finally` cleanup aborted the dispatcher while the bidder was mid-provision,
   and the in-flight record resolve rejected as `TypeError: fetch failed`.
2. **The guest could not mint its report token** -- the decisive blocker. The
   report script read the provisioning JWT's `sub` with a fixed `==` pad (as the
   secrets module does); this provider's token payload is 435 base64url chars
   (three short of a multiple of four), so `base64 -d` failed, `_sub` was empty,
   no token was minted and the report was never sent. Fixed by padding to a
   multiple of four.
3. **The listener unit could not run under the container-mode `systemctl` shim**
   -- a quoted `ExecStart` dies with `unexpected EOF while looking for matching
   quote`, and the shim has neither `ExecStartPost` nor a `StandardOutput=append:`
   redirect. `ExecStart` is now a bare path to `iroh-listen.sh`, which owns the
   identity, the log and the reporter.
4. **The harness wiring** -- two `0.0.0.0` listeners (plain for the in-process
   subscribers, TLS for the guest) with SANs `relay.localhost` /
   `*.relay.localhost`; the fetch interceptor installed with the **TLS** port and
   the CA, because the requester fetches the portless `issuer_uri`'s discovery +
   JWKS; the provider given `guestTlsPort`, `caCertPem` and the OIDC provisioning
   enricher.

One earlier diagnosis did not survive: `/root/.curlrc`'s `resolve` rule is
port-scoped, so it never touched the `github.com:443` archive download; the live
guest extracted `./dumbpipe` before `-q` existed. `-q` was applied anyway.

Requirements: `r.container-harness-proves-live-ssh-over-iroh` (test),
`r.iroh-unit-runs-under-container-shim` and `r.iroh-install-ignores-guest-curlrc`
and the pad rule in `r.iroh-report-carries-workload-token`
(lib-common-cloud-init-common), `r.iroh-fixture` (test-fixtures-cloud-init).
Realized as `2630773` (fixture, 3 files) and `ffac22e` (module + requester +
harness, 8 files, `Acceptance: acceptance passed (gate)`).

**Gap 2: the report carries the guest's workload identity.** With no new
service: the guest mints the exchanged workload-identity token the winning
provider issued it, and the requester verifies it against the provider's
published JWKS through the injected fetch (`jose`), failing closed with 401
unless issuer == `bid_config.issuer_uri`, audience ==
`api://ATProto?actx=<requester DID>` and subject == the provider tag-derived
subject. `r.iroh-report-workload-identity` (lib-requester-xrpc) and
`r.iroh-report-carries-workload-token`; `r.iroh-report-endpoint` now points at
them instead of stating the race as a residual gap. The private-report suite adds
the 401 case and the JWKS-verification case (36 verify tests total, green).

Operator note: three realize attempts failed first (two on a plain/TLS port
mixup at the subscriber's nonce fetch, `invalid HTTP version parsed`; one on the
15-minute agent cap). `clm apply` during a `Running` change is answered `no
change: folded into the running change ...` and silently does not write the
amendment -- render and grep the context after every apply. This run's
`bin/specctl` predates F4 (below), which makes such an apply its own queued
change instead; the F4 work was already on `main`, and the live instance here
still ran the older binary.

Not in scope, unchanged: `lib/cocore-api` `CodeSynced=False`
(`InterfacesMissing`) and the three `CodeRefsUnresolved` contexts after a
realize (plan 0004 E2).

## F4 - two tool defects the atproto-market round-2 run found

Status: **done**. Source:
[`docs/examples/atproto-market-iroh-pr.md`](../examples/atproto-market-iroh-pr.md#what-round-2-found-about-the-tool)
(the two rows marked "new").

1. **An apply that drops requirements is refused unless the removal is stated.**
   `specctl clm apply` of a document an operator sliced removed five `test`
   requirements silently (`-5 ~1`); only the rendered count revealed it. Now the
   model zone may carry a `removed:` list in its spec block, and `apply` refuses
   a delta that deletes a requirement unless each id is listed there or passed to
   `--allow-remove r.a,r.b`. Either way the delta is printed by id
   (`+added ~changed -removed`) on stderr before it lands. The guard lives in
   `impl/clm.Apply`, so `cc-clm-mod`'s `arch_edit` tool and the `pi-hydradb-clm`
   extension get it for free (both run `specctl clm apply`); both hosts relay the
   summary and the refusal.
2. **A second apply while a `SpecToCode` change is `Running` gets its own
   change.** Before, the edit was "folded into the running change" and its delta
   was dropped. Now the edit is always written, and specd raises one change per
   spec hash (`spec.EpisodeOpen`), so the second edit becomes its own `Pending`
   change. The per-repository serialization of plan 0002 B (`RepositoryBusy`)
   leaves it queued until the running realization settles; it keeps its own
   `SpecChange` record and its own `Spec-Change:` trailer. An apply that arrives
   between "Running" and the agent reading the spec is realized by that change
   (it reads the current desired state), and the queued change then finds nothing
   to do; nothing is lost either way.

| # | what landed | test |
| --- | --- | --- |
| 1 | `removed:` marker + `--allow-remove`; `delta.Details` prints the delta by id before apply; the refusal names the ids and the marker | `abc/delta` `TestDetailsNamesEveryEntryByID`, `TestDetailsOfAnEmptyDeltaIsEmpty`; `abc/clm` `TestADocumentReadsItsRemovalMarker`, `TestTheRenderedZoneNamesTheRemovalMarker`, `TestAnEmptyMarkerRemovesNothing`; `impl/clm` `TestApplyRefusesAnImplicitRemoval`, `TestApplyRemovesRequirementsTheDocumentNames`, `TestApplyRemovesRequirementsTheFlagAllows`, `TestApplyPrintsTheDeltaByIDBeforeItApplies`; live `test/e2e` `TestF4AnImplicitRequirementRemovalIsRefused` |
| 1 | the hosts surface the summary and the refusal | `clm` `an apply queued behind a running change reports the queue and the summary`, `the delta summary keeps only the by-id lines`; `cc-clm-mod` `the delta summary keeps only the lines the apply prints by id` |
| 2 | `EpisodeOpen` replaces the coarse `unfinished` gate; `impl/clm.Apply` writes the spec and reports `queued behind <change>` | `abc/spec` `TestEpisodeOpenBlocksTheSameTargetNotANewerOne`; `factory/specd` `TestASecondSpecEditWhileAChangeRunsGetsItsOwnChange`, `TestACLMSpecEditWhileAChangeRunsRaisesItsOwnChange`; `impl/clm` `TestApplyQueuesBehindARunningChangeAndWritesTheSpec`; live `test/e2e` `TestF4TwoQuickAppliesMakeTwoChanges` (two applies, two changes, one batch commit naming both) |

The runnable example `make example-phase8` (and `scripts/example-phase8.sh`)
shows the queue instead of the fold, and `test/e2e` `TestPhase8TheModPathReportsIntoKcp`
asserts the third change.

## F1 - shipped (plan6-f1)

| # | what landed | commits | test |
| --- | --- | --- | --- |
| 1 | `CHANGES.md` baseline: a feature's first persist writes the base code branch's architecture once populate is complete; `readBaseline` tries the base branch, the default branch, then the feature branch's own last commit before the first `-s2c-` change; the doc names the branch it used | `1d6977f`, `d1b0d29`, `daead65` | `impl/persist` `TestAFeatureBranchFirstPopulatedWritesItsBaseArchitecture`, `TestAFeatureBranchWithNoBaseFallsBackToItsOwnLastPreEditCommit`, `TestAPartialPopulateDoesNotPinABaseArchitecture` |
| 2 | the example script records `git describe --always --dirty`, refuses a dirty checkout or a `bin/specctl` built from another commit, writes the commit into the run log; `make build` stamps the commit and rebuilds when it moves; `specctl version` reports it | `2efc610`, `daead65` | script check + `specctl version` (no Go test: a script) |
| 3 | deno import resolution (`ModuleResolver`), workspace-member package partition by default, generated-directory folding | `fd52860` | `abc/sync` `TestPartitionDependenciesResolveDenoImports`, `TestPartitionFactsFoldGeneratedDirectories`; `impl/ingest` `TestDenoImportMapHoldsTheMapAndTheMemberNames`, `TestDetectPartitionPicksPackageForADenoWorkspace`, `TestGeneratedDirectoriesFoldsOnlyAllGeneratedTrees`; `impl/codegraphsqlite` import query |
| 4 | the realize subject names every member context of the batch | `8e2ea2e` | `impl/realize` `TestCommitMessageNamesEveryContextOfTheBatch` |
| 5 | one DeepSeek call per change judges the diff against the added/changed requirements; `status.requirementCoverage`, condition `RequirementsUnimplemented`, CHANGES.md section, `COVERAGE` column; off without a model | `6b40d26` | `impl/coverage`, `impl/realize` `TestCoverBatchFeedsTheDiffToTheJudge`, `factory/specd` `TestRecordBatchSuccessRecordsRequirementCoverage`, `abc/oabranch` `TestChangesDocumentListsUnimplementedRequirements` |
| 6 | no-op spec commits and commit-id-only status rewrites are suppressed | `1d6977f` | `abc/oabranch` `TestCoalesceNoiseDefersADerivedOnlySpecRewrite`, `TestCoalesceNoiseDefersACommitOnlyStatusRewrite`; `impl/persist` `TestACommitIdOnlyReingestMakesNoCommit` |
| 7 | `TestTwoLiveSuitesRunInParallel` holds both runs at a begin barrier instead of asserting overlap by timing | `53121df` | itself (`SPECD_LIVE_LOCK_BARRIER`) |

Live validation (worktree `bin/`, clone `/home/johnandersen777/atproto-market-plan6`,
branch `plan6-f1-validate` off `pre-iroh`, `specctl up --remote ""`, then
`specctl down`): 46 contexts, 46 summarized, 0 failed; 32 contexts with a
non-root `depends_on`, 142 non-root edges; 27 generated directories folded into
`lib-common-market-lexicons` (133 files); base architecture branch
`open-architecture/atproto-market--pre-iroh` written with 46 specs before the
edit; a `specctl clm apply` of one MUST requirement made `CHANGES.md` show that
one requirement and nothing else.
