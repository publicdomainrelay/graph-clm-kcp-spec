# PLAN 0005 - the atproto-market iroh run, and the follow-ups it found

Status: done. Owner: coordination agent. Items A-E are closed by plan 0007:
A and B by items 6 and 2, C by item 1, D was already fixed in
`scripts/example-pr.sh`, and E by item 3. Evidence:
[`docs/examples/atproto-market-iroh-pr.md`](../examples/atproto-market-iroh-pr.md),
[publicdomainrelay/atproto-market#1](https://github.com/publicdomainrelay/atproto-market/pull/1).

The second worked example (iroh / dumbpipe transport for atproto-market, base
branch `pre-iroh`) ran end to end and produced the pull request. The flow held:
81 contexts, one harness pass, ten `SpecToCode` changes, three review-driven
spec amendments, seven commits, `deno check` plus the cloud-init snapshot test
green on every one. What follows is what that run found in this repository.

## A - a realize that rewrites a file leaves sibling requirements' codeRefs unresolved

Evidence: after the run, `specctl arch outline -o json` reports three of 81
contexts `CodeSynced=False`. Two of them name it:

```
context lib-common-cloud-init-common  CodeSynced=False  CodeRefsUnresolved
  unresolved requirement code refs: r.deprecated-wrappers: function:40149c72...,
  r.deprecated-wrappers: function:46c9fd0c..., r.inject-jsr-url: function:4466755...
context lib-market-bidder-compute     CodeSynced=False  CodeRefsUnresolved
  unresolved requirement code refs: r.accept-bidconfig-required: function:5ec21894...,
  r.provider-ref-callback-delegation: function:aed470ef...
```

Every unresolved id belongs to a requirement that the change did **not** touch;
the ids went stale because the realize rewrote
`lib/common/cloud-init-common/mod.ts` (and `lib/market-bidder-compute/mod.ts`)
and the CodeGraph ids are line-sensitive. This is plan 0004 E2 with a second,
independent reproduction on a TypeScript repository, and it is the reason the
architecture cannot be marked synced after a successful realize even though the
realized code is correct.

Proposal (as 0004 E2): re-anchor a stale id by the symbol name the ref records,
or store refs by stable qualified name with the CodeGraph id as a cache. A
realize that inserts lines above a symbol must not unsync untouched
requirements.

## B - `specctl retry` reuses the original change name

`specctl retry <context>` prints "specd raises a fresh attempt for what is still
unrealized" and clears the failed change(s) of the context. What actually
happens is that the **original** change object goes back to `Pending` or
`Running` -- the same `<context>-s2c-<hash>` name, no `-aN` suffix and no new
`status.attempt`. That is fine for the operator (the context is what matters)
but it makes the retry invisible in an audit of `specctl get specchanges`:
after this run's recovery, the table shows the ten original changes as
`Succeeded` and gives no hint that each had already failed three times and been
retried by hand. Either the message or the behaviour should change: a fresh
change name (or a recorded attempt/reason on the same object) makes "this
context was retried, and why" answerable from kcp alone.

## C - `specctl get <kind> -o json` returns a `List` wrapper

`specctl get repository atproto-market -o json` returns
`{"apiVersion":..., "kind":"List", "items":[...]}`, so an operator that reads a
Repository, edits `spec.verify` or `spec.acceptance` and applies it back gets

```
specctl apply: /tmp/work/repository.json: specapi: unknown kind "List"
```

and has to unwrap `items[0]` first. `scripts/example-pr.sh` does exactly that.
Either `get <kind> <name> -o json` should return the object (as `-o yaml` and
`kubectl get` do for a named object), or `apply` should accept a `List`. The
example script's workaround is a symptom, not a fix.

## D - an example script's sibling detection must accept a worktree

Not this repository, but found here and fixed here: `scripts/example-pr.sh`
cloned a sibling from the org root only when `-d "$ORG_ROOT/$dir/.git"`, which
is false for a git worktree (`.git` is a file). Every sibling of the
atproto-market clone silently came from GitHub, at revisions the base branch was
not developed against, and nine contexts failed three times each on `deno check`
before the cause was visible. The test is now
`git -C "$ORG_ROOT/$dir" rev-parse --git-dir`.

## E - batching does not order a dependent context after its dependency

`test-fixtures-cloud-init` realizes before `lib-common-cloud-init-common`, i.e.
before the module whose rendered output it pins. It coped by implementing the
module itself (and then the owning context amended it), so the repository is
correct, but the commit-to-context mapping is wrong and the same shape of change
could just as easily fail three times instead. Nothing in the spec says "this
context depends on that one", so specd has no way to order them. Worth deciding:
either a `dependsOn`-aware realize order, or a rule that a context's realize may
not create code owned by another context.
