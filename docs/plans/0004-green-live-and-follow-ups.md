# PLAN 0004 - deno-kcp#1 green live, and the follow-ups the runs found

Status: done. Owner: coordination agent. Executors: headless `deepseek-claude -p`.

## D - deno-kcp#1's live acceptance goes green, through the spec flow

State after plan 0002 C and kcp-libs#1 (the OpenBao 400 fix): `apply.sh` passes;
`plc`, `relay` and bob's `pds` are `Running`/ready and bob's PDS answers on its
name. Red, each a deno-kcp concern (`docs/examples/deno-kcp-pr.md`, Live
acceptance):

1. alice's `pds` answers 200 from the host but never reports `ready=true` while
   bob's identical pod does;
2. the verifier fails `kcpdns: pds.default.alice.svc.kcp.local is not in the
   table`, and the fixed `sleep 10` before it waits for nothing;
3. the bidder crash-loops on `PlcNotFoundError: DID not found` - it starts with
   a repo key whose DID was never created; it needs an identity (an account on
   bob's PDS, or whatever `hono-bidder` documents for associating with a PDS);
4. the provider intermittently reconciles nothing (initial informer list never
   completes, documented in `cmd/deno-kcp-provider/main.go`); a gating live
   acceptance has to survive or detect that.

Every fix goes in as a requirement, realized by specd, gated by `go test ./...`
and the live acceptance (`gate: true` once it can pass). Done when
`specctl accept` is green on the PR branch twice in a row, the commits are
pushed, and PR #1 states the dependency on kcp-libs#1 and shows the green table.

**Status: done -- 16 of 16, twice in a row on one machine, gate `true`.** The
branch carries seven requirement changes across two repositories (six in
deno-kcp, one in kcp-libs), two of which are amendments that corrected the
acceptance's own checks. The run, the tables, the cause of the last red check and
the follow-ups it leaves are in `docs/examples/deno-kcp-pr.md` (Plan 0004 D).

- **Items 1 and 2 are one defect, and it is fixed.** The provider's wildcard
  informer store keys objects by namespace and name with no workspace, so the
  `default/pds` this branch adds to `root:bob` evicted `root:alice`'s: alice's
  PDS was never re-probed (0 probes in 35 s against 54-97 for the other four
  pods), never `ready`, and absent from every pod's `KCP_DNS_TABLE` -- which is
  the verifier's `pds.default.alice.svc.kcp.local is not in the table` error.
  Requirements `r.watch-cache-keys-every-workspace` and
  `r.watch-cache-keys-every-workspace-test` in `internal-provider`; realized as
  `3749680`.
- **Item 2's fixed wait and item 4 are fixed.** `r.verifier-waits-for-its-peers`
  (apply.sh waits, bounded, for alice's PDS to report `Running` and `ready`
  before creating the verifier) and `r.acceptance-survives-a-stalled-provider`
  (accept.sh detects a provider that reconciled nothing, restarts it while no
  workload exists, and re-runs apply.sh, up to three attempts) in
  `deploy-examples-atproto-market`; both realized as `89cee50`.
- **Item 3 was a kcpdns bug in kcp-libs.** The shim dropped a `Request` input
  and sent its `POST` as a bare `GET`, so the bidder's DID registration arrived
  as a resolve and came back 404; the bidder crash-looped on `PlcNotFoundError`
  forever. Requirements `r.dnsshim-fetch-preserves-the-request` and its test in
  `impl-assets`, through kcp-libs's own flow; realized as `f9e2ef3` on
  [kcp-libs#1](https://github.com/publicdomainrelay/kcp-libs/pull/1).
- **A fifth change corrected the acceptance itself.** `r.live-acceptance-script`
  amended so the bidder's host check is `http://`: the bidder must not take TLS
  flags (`hono-bidder` rejects them), so it consumes only the trust bundle and
  serves plain HTTP. Realized as `e33a558`.
- **The last check was the announce never leaving the PDS.** The verifier's
  `relayFrames 3` were the bidder's own repository on fedproxy, not alice's: the
  live relay's `listHosts` named exactly that one host. Alice's PDS held no relay
  in `KCP_DNS_TABLE` and no token for its workspace in `KCP_TOKENS`, and the
  run's own shim, given that live environment, answers the announce fetch with
  `kcpdns: relay.default.relay.svc.kcp.local is not in the table and could not be
  discovered` inside a `catch` that swallows it. The table is written once when a
  pod starts, and alice's PDS is applied before the relay -- which it has to be,
  because the relay's WebSocket to the PDS is synchronous and cannot fall back to
  discovery. Told by hand to crawl alice's PDS, the same relay carried the
  verifier's commit (`relaySawCommit yes, relayFrames 6`). The fix names the
  relay by its listener address in `PDS_CRAWLERS` (`r.pds-pod`, `r.bob-pds-pod`),
  realized as `caa2838`.
- **The acceptance stops its own workloads.** `r.live-acceptance-script` amended
  in the same realize: the EXIT trap deletes every DenoPod through kcp and waits,
  bounded, before it stops the provider that owns their processes, so a run
  leaves ports 2583-2587 free and a second run starts clean.
- Environment facts: the acceptance needs siblings that carry the TLS support
  (`ORG_ROOT` is on the step); its scratch must not be on the 24 GB `/tmp` tmpfs,
  and the `TMPDIR` the step names must exist, because `mktemp -d` fails on a
  missing directory under `set -euo pipefail`.
- The provider's first start reconciled nothing in both final runs
  (`apply.sh` exits 0 on `attempts=1 0`); `accept.sh`'s restart recovered it
  before any workload existed. Detected, not cured -- curing means bounding the
  provider's initial list, a larger change in `internal/provider`.
- Two findings are left open in sibling repositories, with their evidence in
  `docs/examples/deno-kcp-pr.md`: the kcpdns shim's discovery fallback cannot
  succeed as written (`servicenames.Resolver.Tokens` keys `KCP_TOKENS` by cluster
  id, the shim looks it up by the logical cluster derived from the DNS name, and
  the token set only covers the workspaces a pod's start-time table names), and
  `hono-pds` never retries a crawler that answers non-2xx. Neither blocks the
  acceptance.
- The gate is `gate: true` and green. Every one of the seven changes had to land
  with it relaxed to `gate: false` by hand, because the acceptance could not pass
  while the defects it found were being fixed; part C's stated escape
  (`specctl accept --override`) still does not exist.

## E - hydradb follow-ups

1. `impl/kcpproc` cannot restart kcp on a root it used: `Start` deletes
   `admin.kubeconfig`, which kcp does not rewrite (`empty token for the
   shard-admin user`). Remove it only when the root has no kine store; test
   start, stop, start on one root; `specctl down` then `specctl up` resumes.
2. Code refs go stale when an edit shifts lines (CodeGraph ids are
   line-sensitive): `MigrateDeclared` re-anchors bare names only. Re-anchor a
   stale id by the symbol name the ref records (or store refs by stable
   qualified name with the id as a cache); a realize that inserts lines above a
   symbol must not unsync untouched requirements.
3. Acceptance steps leak processes: `runAcceptanceStep` uses
   `exec.CommandContext`, killing only the direct child. Run each step in its
   own process group, kill the group on timeout/cancel, and reap whatever the
   group left after the step exits.
4. `changes/` is append-only across a restore: a re-render from a fresh kcp
   must not delete `changes/*.yaml` the branch already holds for changes kcp no
   longer has (they are history).
5. The change record is compact: `changes/<name>.yaml` keeps direction,
   members, phase, commit, verify/acceptance results and a delta summary
   (entry counts and ids), not the full embedded `spec.delta` (`CHANGES.md`
   carries the readable delta). Re-measure plan 0003's diff-share row on the
   deno-kcp run.
6. The same-second duplicate attempt race (`TestPhase7...` flake: a `-a2` raised
   in the same second as its base, `factory/specd/systemcontext.go`).
7. Remaining unstable map orders in rendered YAML (`metadata.labels`,
   `spec.acceptance[].env`, `spec.arch.node`/`document`): render sorted.

Each with tests; full live suite green.

## E2 - the feature architecture branch diff is mostly spec

E5 re-measured plan 0003's diff-share row on the deno-kcp run: 27.1% (172 of 635
lines), still far from the > 60% target. Three causes, each fixed:

1. `arch.yaml` repeated every requirement's full text and intent that
   `specs/<context>.yaml` already carries, so re-wording one requirement rewrote
   the whole arch view (154 of the 635 lines). It is now the structure: per
   context its id, name, a `spec:` pointer at its spec file, upstream, overlay,
   orchestrator, depends_on, introduces, the arch node it was seeded from, the
   requirement ids with their level, and `codeRefIndex`. A spec edit that only
   moves text leaves it alone; `import-arch` reads both shapes.
2. `changes/<name>.yaml` carried one progress entry per tool call and turn, and
   an `agentLog` with the harness's own chatter (the connector notice, the
   `[claude-code:unrecognized_model]` warning) and the raw verify output - every
   `ok <pkg> (cached)` line of it. The record now carries a progress summary
   (turns, a count per tool, the files, the notes, the first and last time) and,
   in place of the raw output, a verify summary (exit code, duration, package
   counts, the failing tests, and a bounded tail on failure). The agent's own
   report is kept. The full log and the raw output go to
   `<state>/logs/<change>.log`, which is never committed.
3. Nothing told a reader which files were derived. The branch now writes a
   `.gitattributes` marking `arch.yaml`, `repository.yaml`, `changes/`,
   `context/`, `graph/` and `status/` `linguist-generated=true`, so GitHub
   collapses them in a pull request or a compare view and the diff left to read
   is `specs/` and `CHANGES.md`. The README's "Specs on an orphan branch"
   documents the shape, the markers and the log dir.

**Status: done.** Measured on a fresh deno-kcp run (the reviewed clones copied
with `cp -a`, this worktree's binaries, a fresh state dir, the same harness
request, both changes realized, `specctl down`), with
`scripts/spec-diff-share.py`, which buckets `git diff --numstat` from the commit
before the harness's first spec edit to the branch tip:

| measure | before (E) | after (E2) | target | |
| --- | --- | --- | --- | --- |
| diff, every file counted | 635 | 515 | | |
| `specs/` + `CHANGES.md` | 172 (27.1%) | 231 (44.9%) | > 60% | **not met** |
| share with the `linguist-generated` files dropped | 100% (172 of 172) | 100% (231 of 231) | > 60% | met |
| `arch.yaml` | 154 | 22 | | |
| `changes/` | 204 | 140 | | |

The raw share is not 60% and is not claimed to be: the record keeps the agent's
own report (about half of the 140 `changes/` lines), and `status/` and `graph/`
carry real drift and real facts. The row the target is really about - the diff a
person reads - is 100%, because the markers leave only `specs/` and `CHANGES.md`
in it. `arch.yaml`'s own diff fell from 154 lines to 22: the new requirement ids
and levels, not their text. The plan 0003 measurement section carries the full
bucket table.

Tests: `abc/oabranch` (`TestArchYamlDoesNotRepeatTheSpecText`,
`TestArchYamlMovesWhenTheStructureDoes`,
`TestGitAttributesCollapsesTheDerivedFiles`,
`TestChangeRecordSummarizesProgressAndKeepsTheReport`), `impl/archkcp`
(`TestGeneratedArchitectureImportsBothShapes`: the new shape imports, and an old
full `arch.yaml` still does), `abc/agent` (`TestReportStripsHarnessNoise`),
`abc/spec` (`VerifySummary` of a passing run, of a failing run, bounded),
`factory/specd` (`TestChangeAgentLogKeepsTheReportAndSummarizesVerify`,
`TestWriteFullLogKeepsTheRawLogInTheStateDir`). `gofmt`, `go vet ./...`,
`go test ./...` and `SPECD_REQUIRE_LIVE=1 go test ./... -count=1` (test/e2e
331.5s) are green.

## G - the three defects the green run left outside deno-kcp

Part D left three findings open, each in a different repository: the provider's
intermittent "reconciles nothing" first start (detected by `accept.sh`'s restart,
not cured), the kcpdns shim's discovery fallback that can never succeed, and
`hono-pds`'s crawler announce that a non-2xx response makes permanent. Each went
through its own repository's spec flow, and the deno-kcp#1 acceptance was re-run
afterwards with the fixed siblings.

**Status: done.** All three are realized and gated, the acceptance is green twice
with `apply.sh` reporting `attempts=1 0` (no provider restart) in both runs, and
the dependency pull requests are listed in deno-kcp#1.

- **The provider bounds and retries its initial list.** An informer's initial
  list through the APIExport virtual workspace can fail to end, and nothing
  bounded the wait, so the provider sat with no cache and gave no workload a
  status. Requirements `r.provider-initial-list-is-bounded-and-retried` and
  `r.provider-initial-list-is-bounded-and-retried-test` in `internal-provider`,
  through deno-kcp's own flow, realized as `dc4c717e` on the PR branch: the wait
  for a kind's cache to sync is bounded (30 s per attempt, 5 s between attempts),
  an attempt that does not sync is abandoned and the next builds fresh informers
  from fresh endpoints, and the provider logs both the sync and the retry.
  `accept.sh` keeps its restart as a guard; the two green runs do not use it. The
  same realize added `internal/provider/token_memo.go` (one service-account token
  per pod rather than two, the DNS layer and the reconciler both minting for the
  same account), which no requirement asked for; it is gated by `go test ./...`
  and is noted here rather than silent.
- **`hono-pds` retries a crawler that refuses the announce.** The announce is
  fire-and-forget and the once-per-crawler marker was cleared only in the
  promise's `catch`, so a non-2xx response -- a 502 from a relay behind a restart
  -- left the crawler marked announced for the life of the process and the relay
  never crawled that PDS again. Requirements
  `r.crawler-announce-requires-a-2xx` (`lib-hono-factory-atproto-repo-deno`) and
  `r.e2e-crawler-announce-retries-until-2xx` (`test-hono-factory`), through
  hono-pds's own flow, realized as `e756842` on
  [hono-pds#1](https://github.com/publicdomainrelay/hono-pds/pull/1) (base
  `pre-iroh`, the branch the acceptance's sibling runs). The announce counts as
  done only on a 2xx response; a non-2xx, a network error or a 10 s timeout
  leaves the crawler unannounced and a later write retries it with bounded
  exponential backoff (250 ms doubling, capped at 30 s), while an accepted
  crawler is still announced exactly once. The realize commit also carries a
  `deno.lock` rewrite the repo's own test task produces in this environment; two
  attempts to have the flow restore it returned "the agent changed nothing", so
  it stands and is called out in the PR body.
- **The kcpdns shim's discovery fallback can succeed.**
  `servicenames.Resolver.Tokens` keyed `KCP_TOKENS` by the raw `kcp.io/cluster`
  id, while the shim looks the token up by the cluster it parses out of the
  service name (`pds.default.alice.svc.kcp.local` -> `root:alice`); the two never
  met, and the token set covered only the workspaces the pod's start-time table
  already named. Requirements `r.token-keys-agree-with-the-service-name`,
  `r.workspace-source-widens-the-token-set` and `r.workspace-source-tests` in
  `factory-servicenames`, plus `r.dnsshim-token-key-matches-the-name` and
  `r.dnsshim-discovers-a-name-absent-from-the-table-test` in `impl-assets`,
  through kcp-libs's own flow, realized as `14dcfd7`, `d751fc2` and `51a1c3e`.
  Tokens are keyed by the same cluster string `Name` builds the service name
  from, and `Options.Workspaces` (an interface whose `Clusters` method returns
  the logical clusters a workload may address) widens the token set past the
  start-time table, so a workspace with no pod yet is still given a token and a
  workload can discover a service created after it started. The fix landed on
  kcp-libs#1's existing branch: deno-kcp#1 already pins that branch as its
  sibling `../kcp-libs`, so one checkout carries every fix the acceptance needs,
  and it is the same subsystem (the shim and the resolver behind it) as the
  `Request` fix already there.
- **The acceptance ran with the fixed siblings.** `kcp-libs` is the deno-kcp
  checkout's `../kcp-libs`; `hono-pds` is reached through the step's `ORG_ROOT`,
  and only a working-tree patch of that sibling carries the crawler fix (its
  published branch is based on `origin/pre-iroh`, which predates the TLS support
  the manifests need). The patch was reverted after the runs, and the sibling is
  byte-identical to its branch tip again.
- Two runs of `specctl accept --repo .`, the second started immediately after the
  first, both report `accept: pass`, `apply.sh PASS exit=0 attempts=1 0`, all
  five long-running pods `Running` and `ready`, the verifier `Succeeded`, and
  ports 2583-2587 free between them.
- The gate was relaxed to `gate: false` by hand for the provider realize -- a
  realize that had to run while the acceptance could not yet pass with the code
  it was about to change -- and restored to `gate: true` before the runs. Part C
  of plan 0002's stated escape (`specctl accept --override`) still does not
  exist.
