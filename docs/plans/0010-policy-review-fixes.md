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

Status: done. Commits `02af779` (code fixes), `ce480b1` (layering, derived
CRDs, docs digest), `c2744a4` (a bind draft reads the branch unresolved),
`a5a6c86` (comments), `079a64d` (limits), and the re-run written into
`docs/examples/atproto-market-policies.md`.

D1. **Bind does not see the answer.**
- Bind mode prompts carry the model built **without** the branch's existing
  binding: no vocabulary, globs or roles from it. `02af779`.
- The harness runs in a scratch directory that does not contain hydradb's
  `examples/`. `02af779`; the run is masked further because specd does not
  confine the harness -- recorded as a limit in `docs/policies.md`.
- Re-run the DeepSeek bind on atproto-market and report honestly how close
  the result is to the hand-written binding, field by field (0003 D1, D2).
  Run at `7a2e9d9`, first attempt, byte-identical; `c2744a4` was needed for a
  branch that holds only the pack import to be readable at all. The run found
  two further copies of the answer (the main checkout, then the agent's own
  earlier transcript) before the masks closed them.

D2. **Stronger generated-policy checks.** `02af779`.

D3. **Pack format everywhere.** `02af779`, `079a64d` (the git test pulls from a
bare repository).

D4. **Docs and examples.** `02af779` (propose-interactions, `--gator` with no
suites, the worktree repository inference, the duplicate `vm.onNetwork`, the
dead `draft.summary`, the classifier note), `ce480b1` (the stale digest).

D5. **Style and layering.** `ce480b1` (the kcp writer takes a
`policy.ConstraintCRDBuilder`; `policy.Unstructured` moves to `abc/policy`),
`a5a6c86` (375 comment lines removed from `abc/policy`, `impl/policyeval` and
`impl/policykcp`). Limit: the pack Rego keeps its comments.

D6. **Real Gatekeeper parity.** `ce480b1`.

## Order

R, S and D run in parallel worktrees. R1 to R3 and R9 come first, because
they decide whether the two example rules hold.

After all three tracks merge:

- re-run the atproto-market and deno-kcp policy runs;
- update the docs tables;
- re-run the DeepSeek analysis and the Opus review on the result.
