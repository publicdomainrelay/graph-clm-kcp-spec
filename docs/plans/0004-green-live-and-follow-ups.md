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
