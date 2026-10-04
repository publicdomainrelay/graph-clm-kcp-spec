# PLAN 0003 - architecture branches worth reading

Status: active. Owner: coordination agent. Executors: headless `deepseek-claude -p`.

An independent review (Opus 5.5) of the two branches the deno-kcp run produced,
`open-architecture/deno-kcp` and `open-architecture/deno-kcp--spec-bidder-and-bob-pds`,
found the per-context specs accurate (spot checks against
`internal/denopod`, `internal/trigger`, `cmd/deno-kcp-provider` held), but the
branches weak as architecture and as review material:

- all 17 contexts `upstream: self`, no `dependsOn`, no repo-root context, none of
  the upstreams, orchestrators or trust boundaries deno-kcp's hand-written
  `arch.yaml` has; cross-context facts absent;
- code refs are opaque hashes in `specs/` and `arch.yaml`; the graph has no
  `REFERENCES` edges (no CodeRef map passed to `graph.Build`);
- the same spec appears three times (`specs/`, `context/`, `arch.yaml`); the
  generated `arch.yaml` borrows the hand-written format's apiVersion/kind but
  fails its schema (96 errors); `metadata.branch` is wrong on feature branches;
- 130 commits titled "N modified", 79 of them SpecChange progress ticks; the two
  commits that carry the spec edit are indistinguishable; only 2 trailers;
- the feature-branch diff is 84% noise (`changes/`, duplicates, status-only
  `observedCommit` rewrites); the reviewable part is `specs/` (16%);
- content: a requirement overclaims (`r.flag-env-fallback`), `cmd` contexts lack
  their flag/env table, 70 of 118 `api-v1alpha1` interfaces are generated
  `DeepCopy*`, a machine-local absolute path sits in `r.bidder-pod`, `file:` refs
  to sh/md/yaml show as unresolved although the files exist.

## 3a - the branch reads as architecture and as a reviewable change

1. `metadata.branch` in `arch.yaml` is the branch actually persisted to
   (`BranchFor`, not `Branch`).
2. The graph carries code refs: CodeRef vertices and `REFERENCES` edges built from
   `status.observed` (interfaces with codegraph ids) and `file:` refs.
3. Reviewable history: commit subjects name what changed
   (`spec(<context>): +r.x -r.y ~r.z`, `status(<context>): …`, `change(<name>): <phase>`),
   `Spec-Change:` trailers on spec commits, and SpecChange progress-only updates
   coalesced (a change's file is persisted on phase transitions and at the end,
   not on every progress record).
4. A per-branch `CHANGES.md` on feature architecture branches: the requirement-
   level delta against the default branch's architecture (added/removed/changed by
   id with text, intent changes), and the realization outcome (SpecChanges,
   commits, verify/acceptance results).
5. Less duplication and noise: `context/*.md` no longer repeats the spec block
   (prose + managed refs only, the spec lives in `specs/`); `observedCommit`
   recorded once in `repository.yaml`, not rewritten in every `status/` file;
   `changes/` keeps attempt history (failed attempts summarized in the surviving
   record instead of deleted files); the generated `arch.yaml` either validates
   against deno-kcp-style `arch.schema.json` or uses its own `kind`
   (`GeneratedArchitecture`) with its own documented shape (choose; document).
6. Readable refs: `specs/` and `arch.yaml` render each code ref as
   `name@path:line` beside the codegraph id (from `status.observed`).

Validate: unit/golden tests on `abc/oabranch`; persist tests; a scripted e2e that
checks commit subjects, trailers, coalescing and `CHANGES.md`; then the deno-kcp
run (`scripts/example-deno-kcp-pr.sh`, `PUSH=0`) with the measures below.

## 3b - the spec says more, and says it more truly

7. `file:` refs resolve against the git tree, not only the code index (sh, md,
   yaml), so `deploy-examples-atproto-market` can be `CodeSynced=True`.
8. Cross-context relationships: `dependsOn` derived from the code index's import
   edges between partitions; a repo-root context (Makefile, go.mod, README,
   docs/) as the root the others hang from.
9. Seed from an in-repo architecture document when present: populate imports
   `.tools/open-architecture/arch.yaml` (or a path in `Repository.spec.populate`)
   with `archkcp` to carry upstreams, orchestrators, trust boundaries onto the
   generated contexts (matched by code path), and the summary refines rather than
   replaces them.
10. Generated symbols (`zz_generated*`, `DeepCopy*`) are not declared interfaces.
11. Model output held to a higher bar (summarize prompt + `ParseDraft` /
    validation): a `cmd/` context declares its config surface (flags, env names,
    defaults); requirement text with an absolute machine path is rejected;
    requirements that only enumerate method names are flagged for a rewrite.

Validate: unit tests for each decider; a fixture-level e2e; then the deno-kcp run.

## Measures (deno-kcp run, before -> target)

| measure | before | target |
| --- | --- | --- |
| contexts with a non-`self` upstream or `dependsOn` | 0 / 17 | most |
| `REFERENCES` edges in `graph/edges.jsonl` | 0 | > 0, every requirement ref joins |
| commits on the feature architecture branch | 99 | < 20 |
| share of the feature diff that is `specs/` + `CHANGES.md` | 16% | > 60% |
| `api-v1alpha1` declared interfaces | 118 | ~48 |
| `deploy-examples-atproto-market` CodeSynced | False | True |
| `cmd-deno-kcp-provider` flags declared | 0 | all 17 |

3a and 3b run in parallel in two worktrees; the coordinator merges, reruns the
deno-kcp example, and an independent review compares the new branches with the
old ones.
