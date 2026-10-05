# PLAN 0002 - one kcp per branch, realize a spec edit as one change, prove it live

Status: done. Owner: coordination agent. Executors: headless `deepseek-claude -p`.
Parts A, B and C shipped. The two findings C left open - a stated escape for a
red gating acceptance (`specctl accept --override`) and a restore that rebuilds
the `SpecChange` records - are closed by plan 0007 items 4 and 5. C's own
blocker was fixed through kcp-libs#1 and deno-kcp#1's acceptance is green (plan
0004 D and G).
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
  for kcp and kine, and the same (path, branch) key on the specd log and the
  session record, the branch slug made the way `oabranch.BranchFor` makes its
  suffix;
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

**Status: done.** What shipped, and the decisions behind it:

- The instance is keyed by (checkout path, code branch). `impl/session` puts the
  kcp and kine under `$SPECD_STATE_DIR/repos/<name>-<hash(path)>/<branch-slug>/`,
  the specd log under `logs/<name>-<hash>--<branch-slug>.specd.log`, and the
  session record under `sessions/<name>-<hash(path)>/<branch-slug>.json` — every
  one of them keyed by the same pair, the slug made the way
  `oabranch.BranchFor` makes its suffix (`oabranch.Slug`, now exported, is the
  one sanitizer). A record written before this change (per path, no branch)
  is read once on the default branch, rewritten as that branch's record and the
  old file removed; it keeps its own kcp root, because a running kcp cannot
  move. On any other branch `up` starts that branch's own instance instead.
- `specctl up` resolves the checkout's branch, starts or adopts that branch's
  instance, restores with `persist.Restore` on that branch (falling back to the
  default's architecture branch, already implemented in `BranchFor`/`Restore`),
  and stops the specd of the branch the checkout left; that branch's kcp keeps
  running unless `--stop-others`. `status | env | down | arch | retry | sync`
  resolve the current branch's instance, and `specctl ls` lists every instance
  with repository, path, branch, kcp url, ready and specd state.
- specd refuses to work when the checkout's HEAD is not `Repository.spec.branch`:
  `specsync.BranchCheck` decides (pure, unit tested), `Controller.branchStatus`
  reads the checkout's branch, `reconcileRepository` sets `BranchMismatch=True`
  on the Repository and returns before indexing or populating, and a matching
  HEAD clears it to False. CodeToSpec, SpecToCode and persist check the same
  condition and wait (the change stays Pending) rather than burn an attempt or
  write to a branch the code does not have. A checkout whose branch cannot be
  read at all (not a git tree) is not gated, so spec-only repositories are
  unaffected.
- Proven live by `TestPhase14OneInstancePerBranch` and
  `TestPhase14TwoWorktreesRunAtTheSameTime`, each on private kcps with
  kernel-assigned ports: one checkout, `up` on main, `git switch -c feature`,
  `up` again, a spec edit on feature — two instances on two ports, feature
  restored from main's architecture and persisted to
  `open-architecture/calc--feature`, main's kcp and architecture untouched,
  main's specd stopped, and switching back adopting main's instance unchanged;
  plus two `git worktree`s on two branches whose specds run at the same time.
  Unit tests cover `BranchCheck`, the session keying and the legacy adoption,
  and the controller's mismatch/clear/persist behaviour.
- Found while validating: `TestPhase13CloneUpPersistsAndASecondCloneRestores`
  now waits for the restored clone to be `Populated` before asserting a clean
  tree — the codegraph index directory is untracked until the indexer appends
  `.git/info/exclude`, so a check that lands mid-index sees `?? .codegraph/`.

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

**Shipped (hydradb half).** `Repository.spec.acceptance` is a list of
`{name, command, timeoutSeconds, gate, env}` (`abc/spec.AcceptanceStep`,
validated: a name that is required, unique and single-line, a command, a
non-negative timeout). `impl/realize` runs every step in the realize worktree
once verify has passed, once per batch and again after a rebase retry, with the
step's own timeout and environment; the runner is exported so the CLI and the
reconciler cannot disagree. A `gate: true` step that fails ends the attempt
`Failed` with a message naming the step (branch kept, managed branch still);
`gate: false` records only. Results — name, exit code, duration, passed and the
bounded output tail — land on `SpecChange.status.acceptance` on every member of
the batch, and the commit gets one `Acceptance: <name> passed|failed
(gate|report)` trailer per step. `specctl accept [--repo .] [--name <step>]`
runs the same steps against the current tree and exits 1 when a gating step
fails. The CRDs gained the field and the result list, `deploy/apiresourceschemas`
is regenerated at revision 4 and `deploy/specs-apiexport.yaml` names the new
schemas. Proven live by `TestPlan2cGatingAcceptanceKeepsTheChangeFromLanding`,
`TestPlan2cAcceptanceResultsLandWithTheCommit` and
`TestPlan2cSpecctlAcceptRunsAgainstTheTree`; the design sketch's `timeout` is
spelled `timeoutSeconds` and the sketch's field list gained `env`.

The realization work also had to fix `impl/kcpproc`: an instance adopted
through `Probe` (a `specctl up` on a kcp that was already serving, or a session
whose record carried no pids) came back with pid 0, because `Probe` read
`kcp.pid`/`kine.pid`, which only `specctl kcp start` writes. `Probe` and `Stop`
now find the real pids by scanning `/proc` for the kcp and kine whose command
line names the instance root, `Stop` waits for the process to be gone after
SIGTERM and again after SIGKILL, and `specctl down` fails, keeping its session,
when a process is still alive.

**Shipped (deno-kcp half), and what it found.** The acceptance run happened, on
deno-kcp's PR branch, through deno-kcp's own spec flow: one `MUST` requirement
(`r.live-acceptance-script`) added to the `deploy-examples-atproto-market`
context with `specctl clm apply`, realized first try as
`a41ca4a realize deploy-examples-atproto-market: +1`
(`deploy/examples/atproto/market/accept.sh`, 252 lines, plus a README section),
and the `deno-kcp` Repository's `spec.acceptance` set to run it as a gating step
(`market-live-acceptance`, `gate: true`, 1200s). `specctl accept` runs it in
4 min 37 s and exits 1. The design sketch's expectation that the first live run
would show a small deno-kcp gap -- a bidder flag, a port, a verifier that cannot
see bob -- is wrong in an instructive way: the run shows nothing about deno-kcp
at all, because `apply.sh` stops at its OpenBao gate and no workload is ever
applied. `kcp-libs/impl/openbaoclient.CASerial` maps only HTTP 404 to
`pki.ErrNoAuthority`, and OpenBao answers `GET /v1/pki/cert/ca` with HTTP 400
`no default issuer currently configured` on a pki mount with no issuer -- the
state `impl/pkiprovisioner.ensureRoot` creates by calling `EnsureMount` and then
reading the CA. So `EnsureAuthority` fails on every fresh vault, no namespace is
provisioned an intermediate, no DenoPod is issued a certificate, and the example
never starts. That is outside deno-kcp and outside this repository's remaining
work: the commit that moved the client into kcp-libs (`a24f43b`) is on deno-kcp's
`main`, and deno-kcp's own live test
`TestOpenBaoAuthorityIssuesTheCertificateADenoPodServesWith` fails with the same
message against the OpenBao pinned in `third_party/openbao`. The fix is one
condition in kcp-libs (read that 400 as `pki.ErrNoAuthority`); until then the
gate is red and blocks that repository's realizations, deliberately. The whole
run, the table and the PR #1 follow-up are in `docs/examples/deno-kcp-pr.md`
(see [Live acceptance](examples/deno-kcp-pr.md#live-acceptance)).

**The blocker was fixed, through kcp-libs's own spec flow.** kcp-libs was cloned
to a fresh directory, `specctl up` indexed its 44 contexts and summarized them
(44 of 44), a `MUST` requirement (`r.no-default-issuer-is-no-authority`) and its
test requirement (`r.no-default-issuer-tests`) were written into the
`impl-openbaoclient` context with `specctl clm apply` -- and the existing
`r.sentinel-error-matching` was amended so the spec does not contradict itself
-- and specd realized them first try as `17c6ff5 realize impl-openbaoclient: +2
~1` with `go test ./...` as the gate. `ResponseError.Is` now reports a 400 whose
body says no default issuer as `pki.ErrNoAuthority`, `CASerial` treats it like a
404, and an unrelated 400 stays an ordinary error; two unit tests cover both
halves. The change is [publicdomainrelay/kcp-libs#1](https://github.com/publicdomainrelay/kcp-libs/pull/1),
with the requirement on the orphan branch
`open-architecture/kcp-libs--fix/openbao-no-default-issuer`. deno-kcp's own live
test, the one that failed on `main` with the same message, then passed:
`TestOpenBaoAuthorityIssuesTheCertificateADenoPodServesWith` green in 25.52 s,
its `openbao.yaml` Ready with an intermediate and a leaf issued for
`openbao-tls-probe.default.runtime.svc.kcp.local`. With the fix beside it, deno-kcp
#1's acceptance brings the market up: `apply.sh` exits 0, all four OpenBao
authorities report ready with distinct serials, `plc`, `relay` and bob's `pds`
reach `Running` with `ready=true`, and bob's PDS answers on its own name. The
line is no longer stopped at OpenBao; it is stopped further in, on the three
defects listed below.

Two findings about the flow itself came out of the run and are worth a phase:

- **A gating acceptance that cannot pass freezes a repository.** `gate: true`
  makes every `SpecToCode` realization of that repository end `Failed`, so a
  spec change can only land by relaxing the Repository's acceptance from
  outside the flow. That is the right default -- a red live acceptance should
  stop the line -- but it needs a stated escape (for example `specctl accept
  --override` recording a reason on the change) rather than a manual
  `kubectl patch`.
- **`specctl restore` cannot rebuild `SpecChange` records.** A `SpecChange`
  keeps its outcome in the `status` subresource, and a CRD's status is dropped
  when it is applied, so restoring an architecture into a fresh kcp rebuilds the
  Repository and the SystemContexts but not the change history -- the branch's
  `changes/` then re-renders with only the new run's records. (Re-applying the
  old records by hand is worse than losing them: they arrive without status,
  read as `Pending`, and specd re-realizes them.) The restore should rebuild
  them as historical records without a phase, or the branch should say which
  part of itself is not restorable.
- **A step's own `EXIT` trap is not a teardown the runner can rely on.** The
  example's acceptance script stops everything it started from an `EXIT` trap,
  which is the idiomatic way and works when the script is run by hand -- both
  direct runs cleaned up completely. Run through `specctl accept`, the same
  script printed its final `accept: fail`, exited 1, and left its whole stack
  alive: kcp, kine, OpenBao, the provider and the state directory, with no
  `bash accept.sh` process left to run anything. `impl/realize`'s
  `runAcceptanceStep` uses `exec.CommandContext` and `CombinedOutput`, and
  `CommandContext` kills only the direct child -- never the process group -- when
  the context ends, so a step that spawns anything long-lived leaks it if its
  trap does not get to finish. The runner should put the step in its own process
  group, signal the group (SIGTERM, then SIGKILL after a grace period) instead of
  the one process, and not depend on the child's trap for cleanup. Found by
  looking at `ps` after the run, not by a test; it needs one.

- *The runtime's host check printed `000000` when nothing was listening* (`curl`
  writes `000`, the fallback appended another). Cosmetic, and left alone on
  purpose at the time because fixing it needed the gate relaxed first. Fixed
  with the fix below, in the same realize: `http_code` now prints the status
  `curl` wrote and falls back to `000` exactly once.

The kcp-libs fix landed, deno-kcp's own live test went green, and the acceptance
run then got past OpenBao and reported what is left. Three more findings:

- **`impl/kcpproc` cannot restart a kcp on a root it has already used.**
  Reproduced with three commands: `specctl kcp start --root <dir>` succeeds,
  `specctl kcp stop --root <dir>` stops it, and `specctl kcp start --root <dir>`
  then fails with `kcpproc: kcp exited; ... Error: cannot create the
  'admin.kubeconfig' file with an empty token for the shard-admin user`, leaving
  the root without an `admin.kubeconfig`. `kcpproc.Start` removes that file
  before starting kcp, which is only safe on a root that has never bootstrapped:
  kcp does not write it again. `specctl up` after a `specctl down` therefore
  cannot come back up on the same root -- the run that found this deleted the
  branch's state root and restored from the architecture branch, which is the
  documented path but not the one a user takes. `Start` should remove the file
  only when the root holds no kine store, and a test should start, stop and start
  one root.
- **A realize unsyncs every requirement code ref whose node moved.** CodeGraph
  node ids are line-sensitive, so an agent edit that inserts lines above a symbol
  changes the id of every symbol below it; the stored `codeRefs` then name
  symbols `status.observed` no longer has, and a context that was `CodeSynced`
  reports `False` with `unresolved requirement code refs: ...` for requirements
  the change never touched. Seen on kcp-libs's `impl-openbaoclient` right after
  its own fix landed. `abc/sync.MigrateDeclared` re-anchors bare names only, so
  stale ids survive. Either the ids must be stable across an edit or the
  migration must re-anchor a stale id by the symbol its stored name resolves to.
- **The market example is red past OpenBao, in three different places.** The
  acceptance's own table, and what each line is, is in
  `docs/examples/deno-kcp-pr.md`; the short form: `alice`'s `pds` runs and
  answers `https://127.0.0.1:2583/xrpc/_health` with 200 from the host, but
  reports `ready=false` for the whole run while `bob`'s identical pod reports
  `ready=true`; the verifier fails 12 seconds in with `kcpdns:
  pds.default.alice.svc.kcp.local is not in the table and could not be
  discovered`, so the peers a pod can resolve are the open question, and the
  fixed `sleep 10` before the verifier is not a wait for anything; and the
  bidder crash-loops on `PlcNotFoundError: DID not found:
  did:plc:wrpacy3svybmug6pnjcpjxad` (129 restarts in one run). The example's
  host checks were also written as `http://` against ports whose pods set
  `SERVICE_TLS: "true"` -- a check that could only ever pass while OpenBao was
  broken; that one was fixed through the flow (`6c1bbe4 realize
  deploy-examples-atproto-market: ~1`).

  The provider itself is intermittent, which two further runs showed: it came
  up, logged its startup line, and reconciled nothing -- the applied `OpenBao`
  objects stayed without a `status`, `apply.sh` waited out its deadline, and the
  process sat in `futex_do_wait` with no CPU. That is the failure
  `cmd/deno-kcp-provider/main.go` already documents in a comment ("an informer's
  initial list never completes and the provider sits with no cache and
  reconciles nothing ... a provider that looks healthy and a workload that never
  gets a status"), and it is timing-dependent: the same binary reconciled in
  seconds on the run before. A live acceptance that is expected to gate a
  repository has to survive that.

**Status of C: done.** The hydradb half (the acceptance stage, the CRD and
schema change, `specctl accept`, the trailer) shipped, and the blocker that made
the run worthless was fixed and proven -- kcp-libs#1, with deno-kcp's own live
test going from `FAIL (139.35 s)` to `PASS (25.52 s)`. deno-kcp#1's gate went
green in plan 0004 D (16 of 16 checks, twice in a row, `gate: true`) and plan
0004 G closed the three follow-ups the green run left; the escape C asked for is
plan 0007's `specctl accept --override`. The record below is what stood when the
gate was still red. What remains is entirely in deno-kcp or its provider -- `alice`'s
`pds` never reporting ready, the verifier's fixed `sleep 10` and the missing
resolved name beside it, the bidder's `PlcNotFoundError` crash loop, and the
provider intermittently reconciling nothing at all.

## Order

A and B touch different code (`cmd/specctl`, `impl/session`, `impl/kcpproc`
for A; `factory/specd`, `impl/realize`, `abc/sync` for B) and run in parallel
in two git worktrees, merged by the coordinator. C builds on B (the acceptance
stage sits in the realize path) and runs after the merge. After each, an
independent `deepseek-claude` analysis pass reports what is left.
