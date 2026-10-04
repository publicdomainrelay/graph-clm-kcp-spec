# PLAN 0002 - one kcp per branch, realize a spec edit as one change, prove it live

Status: active. Owner: coordination agent. Executors: headless `deepseek-claude -p`.
Follows PLAN 0001 and the deno-kcp worked example (`docs/examples/deno-kcp-pr.md`,
publicdomainrelay/deno-kcp#1), which exposed the three gaps below.

## A. One kcp instance per (checkout, branch)

**Today.** `specctl up` keys the instance on the checkout path
(`$SPECD_STATE_DIR/repos/<name>-<hash(path)>/`). Two clones or two
`git worktree`s already get two instances. But a `git switch` inside one
checkout keeps the same kcp: it still holds the old branch's specs, and its
specd indexes the new HEAD as if it were the old branch.

**Target.** The instance is keyed by (checkout path, code branch):

- state root `$SPECD_STATE_DIR/repos/<name>-<hash(path)>/<branch-slug>/`
  (kcp, kine, logs, session record), the branch slug made the way
  `oabranch.BranchFor` makes its suffix;
- `specctl up` on a branch starts (or adopts) that branch's instance and
  restores from that branch's architecture
  (`open-architecture/<repo>--<branch>`, falling back to the default's);
- only one specd per checkout is active: `up` on a new branch stops the specd
  of the branch the checkout left (its kcp keeps running, or stops with
  `--stop-others`), because a specd indexes the checkout's HEAD. Branches
  worked on at the same time each get their own `git worktree`, and so their
  own path, instance and specd;
- specd refuses to index or realize when the checkout's HEAD is not its
  Repository's branch (a condition `BranchMismatch` on the Repository, no
  writes), so a stale specd can never misattribute code to a branch;
- `specctl status | env | down | arch | retry` resolve the instance of the
  checkout's current branch; `specctl ls` lists every instance with repo,
  branch, kcp url and state.

**Validated by** a live test: one checkout, `up` on `main`, `git switch -c
feature`, `up` again, edit a spec on feature; assert two instances on two
kernel ports, feature's restored from main's architecture and persisting to
`open-architecture/<repo>--feature`, main's specs untouched, main's specd
stopped; switch back to main and `up` adopts main's instance unchanged. Plus
two `git worktree`s on two branches running their specds at once.

## B. A spec edit is realized as one change

**Today.** One harness request edited two contexts. specd made two
independent `SpecToCode` changes; the registry change depended on files the
example change creates, so its first attempt ran on a base without them and
failed; the two also raced to fast-forward the branch.

**Target.**

- **Serialize per repository.** At most one `SpecToCode` realization runs per
  Repository at a time (admission keyed by repository, not context), oldest
  first. No two realizations race the branch.
- **Batch what is pending.** When a realization starts, every `Pending`
  `SpecToCode` change of the same Repository joins it: one worktree, one agent
  run that receives all the deltas (grouped by context, in creation order), one
  verify, one commit carrying one `Spec-Change:` trailer per change; every
  member is marked `Succeeded` (or `Failed`) together with the same commit.
  A short gather window (`--batch-window`, default 5s, measured from the
  oldest pending change) lets a harness that edits several contexts in a row
  land them in one batch.
- **A failure caused by a sibling is not the change's fault.** If verify fails
  and a sibling change of the same repository landed since the attempt's base,
  the attempt is retried once on the new base without counting against the cap.

**Validated by** unit tests of the pure batching and admission deciders, an
e2e with the scripted agent where one spec edit to two contexts, the second
depending on files the first creates, lands as one commit on the first
attempt, and a live DeepSeek rerun of the deno-kcp request: one batch, first
attempt.

**Shipped** (branch `plan2-b`).

- *Serialize per repository.* SpecToCode admission is keyed by Repository: the
  oldest pending change of it leads, and no other realization of it starts while
  one is `Running`. CodeToSpec keeps its per-context rule. The deciders —
  `PendingForRepository`, `BatchMembers`, `BatchLeader`, `BatchGatherWait`,
  `RepositoryBusy`, `SiblingLanded` — are pure and live in `abc/sync/batch.go`
  with unit tests.
- *Batch what is pending.* The leader waits `--batch-window` (specd flag,
  default 5s) measured from its own creation, then every pending SpecToCode
  change of the repository joins it. `RealizeRequest` grew `Members`;
  `impl/claudecli` groups the deltas and the target specs by context in creation
  order, and `impl/scriptedagent` applies each member's steps in the same order.
  One worktree, one agent run, one verify, one commit with one `Spec-Change:`
  trailer per member. Every member lands `Succeeded` or `Failed` together with
  the same commit, `filesTouched` and verify exit code. Progress: a batch of two
  or more records one entry naming the batch and its leader on *every* member
  (chosen over a leader-only record so a member's own status is self-contained).
  `ingest.Options` grew `AdoptAll`, so one settle pass writes every member's
  realized hash.
- *A sibling failure is not the change's fault.* When verify fails and the
  managed branch has moved since the attempt's base, the attempt is retried once
  in the same reconcile on the new base; because no new change object is
  created, the retry does not count against `--max-attempts`.
- *Tests.* `abc/sync/batch_test.go`, `factory/specd/batch_test.go`, and
  `test/e2e/batch_live_test.go`: the two-context edit as one commit on the first
  attempt, two independent edits realized one after the other, and a verify
  failure whose branch moved retrying once under `--max-attempts=1`. All live
  tests pass with `SPECD_REQUIRE_LIVE=1`, and `make test-live-model` still
  passes. The live DeepSeek rerun of the deno-kcp request itself is part C's
  acceptance run.

## C. "Running" is proven live, not only offline

**Today.** The deno-kcp change was gated by `go test ./...`, which decodes
the manifests and checks them against the schemas. Nothing brought the market
stack up, so "a running bidder" was never observed running.

**Target.**

- **An acceptance stage.** `Repository.spec.acceptance`: a list of commands,
  each `{name, command, timeout, gate}`, run in the realize worktree after
  `verify` passes. `gate: true` blocks the commit like verify; `gate: false`
  records the result only. Results go to `SpecChange.status.acceptance` (name,
  exit code, duration, output tail) and the commit message as
  `Acceptance: <name> passed|failed`. `specctl accept --repo .` runs the same
  commands on demand against the current tree.
- **The deno-kcp live check.** For deno-kcp the acceptance command brings up a
  private kcp, the provider, OpenBao and the market example with
  `deploy/examples/atproto/market/apply.sh`, and asserts what the request
  asked for: the `bob` workspace exists, `DenoPod/pds` in `root:bob` and
  `DenoPod/bidder` reach phase `Running` and stay up, and bob's PDS answers on
  its name. Where the example's verifier cannot see bob, that is a spec gap
  and goes back through the flow: a requirement added with `arch_edit`, realized
  by specd, accepted live.
- **PR #1 follow-up.** Run the live acceptance on the PR branch; every fix it
  needs lands through the spec flow as new commits on the same branch; the PR
  description reports the live result.

**Validated by** an e2e where a gating acceptance command fails and the
change does not land, and a passing one lands with `Acceptance:` trailers;
and the live market run on deno-kcp's PR branch with its output recorded in
`docs/examples/deno-kcp-pr.md`.

## Order

A and B touch different code (`cmd/specctl`, `impl/session`, `impl/kcpproc`
for A; `factory/specd`, `impl/realize`, `abc/sync` for B) and run in parallel
in two git worktrees, merged by the coordinator. C builds on B (the acceptance
stage sits in the realize path) and runs after the merge. After each, an
independent `deepseek-claude` analysis pass reports what is left.
