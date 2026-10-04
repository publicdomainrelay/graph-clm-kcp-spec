# PLAN 0006 - review 0002 fixes: atproto-market#1 works, and the tool sees TypeScript

Status: active. Owner: coordination agent. Executors: headless `deepseek-claude -p`.
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
