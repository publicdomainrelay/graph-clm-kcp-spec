# PLAN 0004 - deno-kcp#1 green live, and the follow-ups the runs found

Status: active. Owner: coordination agent. Executors: headless `deepseek-claude -p`.

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

**Status: not done -- two of the four red items are fixed and landed, two are
outside deno-kcp.** The full run, the tables and the evidence are in
`docs/examples/deno-kcp-pr.md` (Plan 0004 D).

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
- **The acceptance is red on two checks, both outside deno-kcp.** The bidder
  crash-loops on a 404 from its PLC genesis `POST`; the client, `hono-plc` and
  the kcpdns shim all succeed standalone and through the shim, so the 404 exists
  only in the cluster topology and needs the PLC's request log. The verifier now
  creates the account, resolves the DID and makes both writes (all 200) and
  fails only on `relayFrames: 0` / `relaySawCommit: timeout` -- the
  PDS-to-relay `subscribeRepos` path.
- Environment facts: the acceptance needs siblings that carry the TLS support
  (the clones handed to the run predate it; `ORG_ROOT` is now on the step), and
  its scratch must not be on the 24 GB `/tmp` tmpfs (`TMPDIR` is now on the
  step), because a crash-looping pod writes ~14 MB per restart.
- The gate is back to `gate: true` while red. The two fixes above had to land
  with it relaxed to `gate: false` by hand; part C's stated escape
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
