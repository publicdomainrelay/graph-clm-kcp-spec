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

**Status: 3b shipped** (branch `plan3-b`, plus the delivery branch it runs on).
What shipped, and the decisions behind it:

- *7. `file:` refs resolve against the git tree.* ingest reads the tracked
  files (`gitrepo.TrackedFiles`, `git ls-files --cached --exclude-standard`) and
  each context carries the ones the code index does not cover as
  `status.observed.treeFiles` (`abc/sync.Partition.TreeFiles`). They do not
  enter the fingerprint and they are not merged into `spec.codeRefs`, so no
  context's digest churns because a README moved; they are resolvable refs
  (`ResolvableRefs`), the draft parser accepts them (`canonicalRefs`) and the
  prompt lists them, so a requirement may name `apply.sh` and be right.
- *8. Cross-context relationships.* `PartitionDependencies` derives `dependsOn`
  from the index's `imports` edges (`codegraphsqlite.Imports`, joined to the
  source file and the imported path) against the `go.mod` module path, and every
  non-root context also depends on the repository root. The root context — the
  depth-0 tracked files plus `docs/` — is created when `spec.populate.root` is
  set (what `specctl up` applies), because a fixture repository whose root files
  are its build files would otherwise grow a context no scenario describes.
  ingest fills `spec.dependsOn` only when the spec declares none, so a human
  edit is never overwritten.
- *9. Seed from the in-repo architecture document.* `archkcp.Seed` runs inside
  ingest when `populate.arch` names a document (default
  `.tools/open-architecture/arch.yaml` when it is there). `abc/sync.MatchArch`
  scores every arch node against every partition by code path — a node's `code`
  and its `source` (now both feed `archyaml.Node.Code`), counting the files it
  names, how many of its paths are inside the partition and how many are
  outside, then shallower first — and the root context takes the document's own
  root node (`metadata.root`, or the node named after the repository). The
  matched node's `upstream`, `overlay`, `orchestrator`, `depends_on`,
  `introduces` and node body land on the context, merged only where the context
  has nothing; the document's refs are resolved onto the generated context
  names (`sc.kind.denopod` becomes `sc.internal-denopod`; `upstream` follows a
  chain, so the root's `sc.kcp-local` becomes `up.kcp`), an overlay ref that no
  generated context carries keeps the document's own spelling, and a
  `depends_on` to a context that is not generated is dropped. The seed writes
  the merged hash as both the origin hash and `status.realizedSpecHash`, so the
  facts never read as a spec edit. `DraftSpec` keeps `upstream`, `overlay`,
  `orchestrator`, `dependsOn` and `arch`, so the summary refines the seed.
- *10. Generated symbols are not declared interfaces.*
  `abc/sync.IsGeneratedSymbol` drops a symbol from a `zz_generated*` file, or
  one whose name (bare or qualified) begins with `DeepCopy`, before anything is
  observed.
- *11. Model output held to a higher bar.* `abc/spec/quality.go` holds the pure
  deciders: `AbsoluteMachinePath`, `CommandContext`, `DeclaresConfigSurface`,
  `ArchDeclaresConfigSurface`, `NamedFlags`, `NamedEnvironments`,
  `EnumeratesNames`. `ParseDraft` rejects a requirement naming a machine path,
  drops a declared interface the facts do not name, and warns about a
  requirement that is only a list of names; `ValidateDraft` requires a
  configuration-surface requirement for a `cmd/` context whose arch node knows
  flags (so a fixture command with no flags is not failed for having none); the
  prompt states all four rules and renders the seeded arch node.
- *Schema.* kcp prunes unknown fields, so `populate.arch`, `populate.root` and
  `observed.treeFiles` needed the CRDs; `deploy/apiresourceschemas` is
  regenerated at revision 5 and `deploy/specs-apiexport.yaml` names them.
- *Found while validating.* Restarting a kcp on an existing root fails with
  `cannot create the 'admin.kubeconfig' file with an empty token for the
  shard-admin user` (`impl/kcpproc`), so each measurement run used a fresh state
  dir; that restart is a bug for another change, not this one.

Measured on the deno-kcp run (`specctl up --remote ""` on a fresh branch of a
copy of the review's clone, DeepSeek summaries, `specctl down` at the end), my
half's rows:

| measure | before | after | target |
| --- | --- | --- | --- |
| contexts with a non-`self` upstream or `dependsOn` | 0 / 17 | 18 / 18 | most |
| `api-v1alpha1` declared interfaces | 118 | 48 | ~48 |
| `deploy-examples-atproto-market` CodeSynced | False (InterfacesMissing) | True (InterfacesObserved) | True |
| `cmd-deno-kcp-provider` flags declared | 3 of the document's 19 named in requirement text | 19 / 19 | all 17 |
| contexts `CodeSynced=False` | 4 / 17 | 0 / 18 | — |
| contexts carrying an arch node | 0 | 16 / 18 | — |
| requirement `file:` refs with no file in the code tree | 0 (the model could only name indexed files) | 0 (it may now name any tracked one) | — |

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
