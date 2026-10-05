# PLAN 0007 - the loose ends a final analysis found

Status: done. Owner: coordination agent. Executors: headless `deepseek-claude -p`.

What shipped, item by item:

- 1: a named `specctl get -o json|yaml` writes the object, `kcpclient.Decode`
  expands a `kind: List`, and the script's unwrap is gone.
- 2: `specctl retry --reason <text>` keeps the failed changes and creates the
  next attempt `<base>-aN` with `status.attempt`/`retryReason`/`retryBy`; specd
  exempts a deliberate retry from the attempt cap.
- 3: `specsync.OrderBatch` orders a batch by `spec.dependsOn`, and a realize
  whose `filesTouched` names a file a context outside the batch observes gets
  `FilesOutsideContext=True`.
- 4: `Repository.spec.acceptanceOverrides` plus `specctl accept --override
  <step> --reason <text>`; the commit trailer and the `AcceptanceOverridden`
  condition record it, and specd consumes the entry it used.
- 5: `oabranch.ChangeHistory` plus `persist.Restore` rebuild the `SpecChange`
  history, superseded attempts included.
- 6: a live e2e proves re-anchoring on a Go and a Deno/TypeScript edit;
  `abc/sync.ReanchorRefs` held as written, no fix was needed.
- 7: this document, the README's phase count and "what is next", the two worked
  examples' headline diffs, and the plan statuses.

Tests: unit tests for every item, and live tests
`TestRestoreRebuildsTheChangeHistoryLive` and
`TestReanchorKeepsUntouchedRequirementsCodeSyncedLive`. `gofmt`, `go vet ./...`
and every package's tests are green, offline and live, except
`TestPhase2IngestAndGraph/hydradb` on this machine: the shared HydraDB instance
lost its SlateDB objects during the session (a stray `rm` of `/tmp/hdb` while
freeing tmpfs space; the token file was restored), so its reads fail with
`object store error ... .sst not found`. ArcadeDB, the default backend, passes,
and so does every other live test.

Source: the follow-ups left open by plans 0002, 0004, 0005 and 0006, each named
there with evidence and never closed. Every item ships with tests; `gofmt`,
`go vet ./...`, the offline `go test ./...` and the live suite with `TMPDIR` on
a real disk are green, except the one HydraDB subtest named above.

## 1 - `get -o json|yaml` returns the object, `apply` takes a `List`

Plan 0005 C. `specctl get repository atproto-market -o json` wraps the one
object in `{"kind":"List","items":[...]}`, so reading a Repository, editing
`spec.verify` and applying it back fails with
`specapi: unknown kind "List"`; `scripts/example-pr.sh` unwraps `items[0]` by
hand. Fix both ends, because both are wrong:

- `printObjects` takes whether the caller named one object; for a named object
  `-o json` emits that object and `-o yaml` keeps emitting it (one document);
  for a list it keeps the `List` envelope, which is what a list output is.
- `kcpclient.Decode` expands a `kind: List` into its `items`, one level, so
  `specctl apply -f repository.json` accepts what `specctl get -o json` wrote.
  An item of unknown kind is still rejected with its own message.
- `scripts/example-pr.sh` drops its `items[0]` unwrap, now that it is not
  needed.

Tests: `cmd/specctl/print_test.go` (named object, list, yaml), a manifest test
for the `List` expansion and for the unknown-kind rejection inside a `List`,
and the script's own run in the example is not repeated here.

## 2 - `specctl retry` records the attempt and the reason

Plan 0005 B. `specctl retry <context>` deleted every failed change of the
context and set an annotation specd never read; specd then raised the **same**
name again, so `specctl get specchanges` showed ten `Succeeded` changes and no
hint that each had already failed three times and been retried by hand.

Target: a retry is visible in kcp alone.

- `specctl retry <context> [--reason <text>] [--by <user>]` keeps the failed
  changes (they are the audit trail) and creates the next attempt itself:
  `<base>-a<N+1>`, where `N` counts every attempt of the episode including the
  failed ones, carrying the failed change's direction, hashes and delta.
- The new change's status records `attempt: N+1`, `retryReason` and `retryBy`,
  and its message names the operator and the reason. `--reason` defaults to
  `manual retry` and `--by` to the environment's user, so the existing call
  sites keep working, and every retry still says what it was.
- A change that carries a `retryReason` is deliberate, so specd does not apply
  the attempt cap to it (`codespec.go`, `realspec.go`): the operator asked for
  it after the cap was reached. Automatic retries are still capped.
- The `SpecChange` status gains `attempt`, `retryReason`, `retryBy`, and the
  branch's change record carries them, so a restore can rebuild them (item 5).

Tests: a CLI test that a retry creates `-a2` (and `-a3` on a second retry),
records the reason, and leaves the failed changes in place; a controller test
that a change with a `retryReason` runs past the cap while an automatic one
does not; `abc/spec` coverage for the next-attempt name over a kept failure.

## 3 - a batch orders by `dependsOn`, and files outside the context are flagged

Plan 0005 E and plan 0001 13c. `test-fixtures-cloud-init` realized before
`lib-common-cloud-init-common`, the module its rendered fixture pins, so the
agent implemented the module from the dependent side; the commit-to-context
mapping was wrong and the same shape could fail the gate instead.

- **Order.** `specsync.OrderBatch` topologically orders the batch members by the
  contexts' `spec.dependsOn` (`sc.<context>` names a member): a change whose
  context depends on another changed context is applied after it, and the agent
  receives the deltas in that order. The leader is still the oldest pending
  change (admission is unchanged); the order only decides what the agent does
  first. A cycle keeps the age order for the members it cannot order.
- **Flag.** After a realize lands, specd checks `status.filesTouched` against
  the files the repository's other contexts own. A file that is observed by a
  context that is **not** in the batch is a file the change did not own, and
  every member of the batch gets the condition `FilesOutsideContext=True` with
  the files and their owners in the message, instead of the commit being
  silently accepted. A brand new file no context observes is not flagged: that
  is new code for the changed context.

Tests: `abc/sync` for the topological order (a chain, an independent change, a
cycle) and for the ownership check; `factory/specd` for a batch where a member
touches a file another, unbatch'd context owns and the condition is written.

## 4 - `specctl accept --override <step>`

Plans 0002 C and 0004 D name it; it does not exist. A red gating acceptance
freezes a repository - every `SpecToCode` realization ends `Failed`, so a spec
change can only land by relaxing the Repository's acceptance by hand. The
escape, stated and audited:

- `Repository.spec.acceptanceOverrides[]` is `{step, reason, by, at}`;
  `specctl accept --override <step> --reason <text> [--by <user>] [--repo .]`
  requires `--reason`, refuses a step the Repository does not name, records the
  entry on the Repository, and then runs the steps as usual: the overridden
  step's failure prints as overridden and does not fail the command.
- A realize reads the overrides: a gating step that fails and names an override
  does not block the commit, its `AcceptanceResult` carries `overridden`,
  `overrideBy` and `overrideReason`, the commit trailer is
  `Acceptance: <step> overridden by <user> (<reason>)`, and every member of the
  batch gets the condition `AcceptanceOverridden=True` naming the step, the user
  and the reason.
- The override is **one shot**: specd removes the entry it consumed after the
  commit lands, so the next realization is gated again. An override that was
  never needed stays.

Tests: `abc/spec` (a trailer line for an overridden step, `AcceptanceBlocked`
skips an overridden gate, validation of the override entry); `cmd/specctl`
(`--override` without `--reason` is a usage error, an unknown step is refused);
`factory/specd` (a gating step fails but the override lands the commit with the
trailer and the condition, and the entry is removed; without the override the
change fails).

## 5 - `specctl restore` rebuilds the `SpecChange` history

Plan 0002 C. A CRD's status is dropped when it is applied, so restoring an
architecture into a fresh kcp rebuilt the Repository and the SystemContexts but
not the changes: `specctl get specchanges` was empty, and the branch's
`changes/` re-rendered from the new run alone.

- `oabranch.ChangeFiles` already parses `changes/*.yaml` into `SpecChange`
  records, surviving attempt plus `superseded`. `persist.Restore` applies each
  one after the contexts: the surviving attempt with its `spec` (direction,
  hashes, delta) and its status (phase, commit, verify, message), and each
  superseded attempt with the same `spec` and its own phase and commit, so the
  episode and its attempt count survive.
- A phase the branch recorded but kcp never settled (`Pending`, `Running`, or
  none) is restored as `Failed` with a message that says the branch recorded the
  other phase, so a restore can never leave a change that blocks the repository
  by looking `Running` forever. The operator retries it with item 2.
- `specctl get specchanges` on a restored instance therefore shows the history,
  and `specctl get specchange <name>` shows the commit each attempt landed.

Tests: `impl/persist` for a branch fixture whose `changes/` holds an episode
with superseded attempts, asserting the rebuilt objects' names, phases, commits
and count; an e2e that re-renders a branch, restores into a fresh workspace and
reads the changes back.

## 6 - re-anchoring, proven on a real realize

Plan 0004 E2 and plan 0005 A: CodeGraph ids are line-sensitive, so a realize
that inserts lines above a symbol changes the ids below it and untouched
requirements' `codeRefs` go stale. `abc/sync.ReanchorRefs` is wired into
`impl/ingest` with unit tests; what is missing is the end-to-end proof on the
shape plan 0005 A describes.

An e2e test realizes a change that inserts lines above symbols in a Deno /
TypeScript package and in a Go package, then re-ingests and asserts that every
untouched requirement of both contexts stays `CodeSynced=True` and that the
touched requirement's ref resolves to the new id. If it fails, fix the
re-anchoring (a symbol that moved file or receiver, a name that is ambiguous
before the move) until it passes.

## 7 - the docs catch up with the code

- README "Status": "All ten phases are done" is wrong about the count and about
  what is left. Plan 0001 has phases 1-14 plus 13b and 13c; say which plans are
  done and which items remain open (this plan's list), instead of claiming the
  plan is complete.
- README "The plan is complete." - replace with what is actually done and what
  is still weak, and point at this plan.
- `docs/examples/deno-kcp-pr.md` and `docs/examples/atproto-market-iroh-pr.md`:
  the headline diffs are the first round's. Read the current PR stats with `gh`
  and say that the branch grew in the later rounds, with the current numbers.
- Plan statuses: 0002, 0003, 0004 and 0006 are done and say so in their bodies;
  their headers still read `active`. Mark them done, and mark 0005's items
  closed by this plan (1-6) with the one-line evidence.

## Order

1, 2 and 5 are CLI and persistence and are independent. 3 and 4 touch
`factory/specd` and `impl/realize`. 6 is a test that may or may not force a fix
in `abc/sync`. 7 is last, and its numbers come from the code that shipped.
