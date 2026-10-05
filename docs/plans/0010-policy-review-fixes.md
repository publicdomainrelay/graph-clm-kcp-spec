# Plan 0010: policy review fixes

Inputs:

- `docs/reviews/0003-policies.md` (Opus): adversarial runs, design, sync and
  gates.
- `docs/reviews/0004-policies-analysis.md` (DeepSeek): plan-vs-code check,
  docs commands and portability.

Ids below cite the review sections: `0003 B1` and so on, and `0004 bug 1` and
so on.

Done means:

- every item is fixed, or explicitly recorded as a limit with the reason;
- tests reproduce each defect before the fix and pass after;
- the review's adversarial variants live in the pack suites;
- gofmt, vet, `go test ./...` and the live suite are green.

## Track R: rules and facts (the two example rules must hold)

R1. **Attribution comes from the call site, not the file.**
- Attributes and hints (proxyCommand, channel, targets) are read from the
  site's own call. A test file's other ssh calls never lend their ProxyCommand.
- Covers 0003 B1. Evidence: `abc/policy/effectmatch.go:411-421`,
  `modelbuild.go:728-746`.

R2. **Relay means a relay.**
- An ssh counts as relayed only when its ProxyCommand (or the resolved
  transport) names a term from vocabulary `channels/relay`.
- `ProxyCommand=nc <guest> 22` and any unnamed tunnel are denied; a binding
  can list extra relay terms.
- This replaces the "any non-empty proxyCommand" clause (0003 B2;
  0004 portability 6).

R3. **Reach-in by default.**
- In the host role, `container.exec`, `ssh.connect` and `net.dial` /
  `http.request` effects whose target is the guest or is unknown are reach-ins
  by default. This includes `container inspect`, a fetch to the guest IP, and
  ssh into the guest over the relay for the purpose of address discovery.
- The binding names exceptions, not the reach-in verbs, so a new repository
  no longer has to rediscover `getNodeId`-like verbs.
- Add `net.dial` to the guest's ssh port (`pollSsh`).
- Covers 0003 B3, B4 and 0004 portability 1, next-step 10.

R4. **Aliases.**
- Resolve simple aliases in classifiers: `const X = "ssh"` used as argv0, a
  host held in a variable. Use intra-file constant propagation in the
  TypeScript and Go packs.
- Measure recall again on the G2 hand-labelled sample and on new labels for
  the alias cases (0003 B4).

R5. **Model accuracy.**
- Flow merge keeps the union of payloads (0003 B5).
- A trigger needs a call edge, not a shared node (0003 B6).
- The event class resolves constants and NSID imports, not the spelling of
  the first argument (0003 B7).
- Payload terms in the guest-reports require are matched as whole tokens in
  the report's body or arguments (0003 B8).

R6. **One glob dialect.**
- Go and Rego use the same doublestar semantics (`**` across directories,
  `*` within one).
- Shared conformance tests run the same table through Go and through Rego
  (0003 A1).

R7. **Fresh facts.**
- eval, model, effects, the audit and both gates detect a stale `.codegraph`.
  Stale means: index mtime or recorded commit older than the tree, or a dirty
  tree.
- They resync, or index a copy, and never evaluate stale facts.
- No index is written into a user's checkout unless `--index-in-place` is
  given (0003 G5; 0004 bug 1).

R8. **Diff parser.** A removed line that starts with `--` (or `++`) inside a
hunk is content, not a header (0003 G4).

R9. **The review's adversarial variants become permanent tests.**
- The 8 variants from 0003 (vB, vC, vD, vE, vH1 to vH3, and the rest) become
  fixtures and gator cases in `policies/packs/rfp-guest-isolation`.
- They also become `fixtures/market-mini` violating variants.
- Each one must deny.

## Track S: sync, gates and kcp

S1. **Real two-way sync.**
- The branch and kcp keep a recorded base: the last synced branch commit and
  the kcp resourceVersions, stored in a status or annotation.
- Branch edits made after the base are applied to kcp, and kcp edits are
  persisted to the branch.
- When both changed, a conflict is reported in a condition and nothing is
  overwritten (0003 S1).

S2. **Per-repository ownership.**
- kcp policy objects carry the label
  `specs.publicdomainrelay.dev/repository`.
- `policykcp.Read` filters on it, so a repository's branch never receives
  another repository's policies (0003 S2).

S3. **Imports stay imports.**
- Persist writes only the repository's own templates. Imported pack templates
  stay as an import plus the lock.
- Upgrading a pack works after a sync (0003 S3).

S4. **No deletes on errors.**
- Only a confirmed not-found removes a constraint from the branch. Other list
  errors are retried and reported (0003 S4).
- Swallowed errors are logged and surfaced (0003 S5).

S5. **The spec-time gate honors** `Repository.spec.policy.disabled`, the
enforcement cap and overrides (0003 G1). It also carries deltas and members
(0003 G3).

S6. **Change-scoped gates (baseline).**
- Both gates evaluate the base (the SpecChange's base commit, or the
  pre-delta specs) and the head.
- A deny blocks only when it is new: present at the head, absent at the base,
  keyed by constraint, object and site.
- Pre-existing violations are reported as `inherited`, as warn.
- `--no-baseline` / `spec.policy.baseline: none` restores whole-repo gating
  (0003 G2).

S7. **CLI restore** defaults to `--prune=false` (0004 bug 2).

S8. **`policy test` is read-only.** It does not rewrite `dist/`, the
catalogue or the lock, and it does not commit when run against a branch.
`policy build` does those things (0003 e).

## Track D: generation honesty, packaging, docs, style

D1. **Bind does not see the answer.**
- Bind mode prompts carry the model built **without** the branch's existing
  binding: no vocabulary, globs or roles from it.
- The harness runs in a scratch directory that does not contain hydradb's
  `examples/`.
- Re-run the DeepSeek bind on atproto-market and report honestly how close
  the result is to the hand-written binding, field by field (0003 D1, D2).

D2. **Stronger generated-policy checks.**
- `ForbiddenIdentifiers` also bans classifier file names and the binding's
  `targets.attrs` values.
- A test proves that a generated rule reading `getNodeId` is refused
  (0003 D3; 0004 next-step 7).

D3. **Pack format everywhere.**
- `policies/packs/conformance` gets a `pack.yaml` and is imported like the
  rfp pack.
- A freshness test walks `policies/packs/*` (`dist/`, `CATALOGUE.md`).
- Tests cover `git:` and `oci:` pack sources (a local bare repository, and an
  oras OCI layout) (0004 untested 1, 2, 4).

D4. **Docs and examples.**
- Fix the stale digest at `docs/policies.md`.
- greenfield `--gator` with 0 suites must say so, not PASS silently.
- `--propose-interactions` drops `peer: unknown` entries or comments them out.
- Remove the duplicate `vm.onNetwork` in the hono-compute-provider binding.
- Fix the dead `draft.summary`, so a first-attempt success records its
  agentLog.
- Repository inference is relative to the worktree, not the CWD
  (0004 bugs 3 to 5).
- `docs/examples/portable-policies.md` states that two cross-repo bindings
  also need a classifier file, until R3/R4 make that unnecessary.

D5. **Style and layering.**
- Remove the code comments in `abc/policy` and the other new packages (org
  no-comments rule; fixtures excepted) (0003 f).
- Move what `impl/policykcp` needs from `impl/policyeval` into `abc/policy`,
  or into a factory, so that impl packages do not import each other where a
  shared abc type would do.

D6. **Real Gatekeeper parity.**
- Document that CodeGraph, CodeDiff and ArchitectureModel are evaluated by
  specd, not by in-cluster Gatekeeper.
- Provide an optional `deploy/crds/derived/` set of CRDs for these kinds, so
  `gator` and a real cluster can at least hold them (0003 A2).

## Order

R, S and D run in parallel worktrees. R1 to R3 and R9 come first, because
they decide whether the two example rules hold.

After all three tracks merge:

- re-run the atproto-market and deno-kcp policy runs;
- update the docs tables;
- re-run the DeepSeek analysis and the Opus review on the result.
