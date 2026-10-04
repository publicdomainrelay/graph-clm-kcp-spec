# Review 0001 - the deno-kcp architecture branches

Reviewer: an independent Claude Opus 5.5 subagent, read-only. Subject: the
branches the deno-kcp worked example produced,
`open-architecture/deno-kcp` (31 commits, 862 KB, the architecture of `main`)
and `open-architecture/deno-kcp--spec-bidder-and-bob-pds` (merge-base `6a5a3de`,
+99 commits; diff 27 files, +793 / -72), checked against the deno-kcp code and
its hand-written `.tools/open-architecture/arch.yaml`. Plan 0003 implements the
recommendations.

## 1. Usefulness

Accurate where spot-checked: `specs/internal-denopod.yaml` (all 17 requirements
match `internal/denopod/denopod.go`, e.g. `RequeueAfter = 2s` :14, name check
before context check :106/:112, restart-policy default with `ProcessFailed`
:275-279), `specs/internal-trigger.yaml`, `specs/cmd-deno-kcp-provider.yaml`
(`KUBE_FEATURE_WatchListClient` reason main.go:72, exit 2 :115, `readIfSet`
:252). Feature-branch requirements `r.bidder-pod` and
`r.apply-includes-bob-and-bidder` match `70-bidder.yaml` and `apply.sh` exactly.

Errors and vagueness:

- `r.flag-env-fallback` claims every config value has an env fallback; false for
  `pod-timeout`, `token-ttl`, `write-status` (main.go:88-94). The flag/env table,
  the most useful fact for a newcomer, is absent; the context declares 0 interfaces.
- `internal-provider` (42 files, the core) has 19 requirements that mostly restate
  method lists; nothing on the APIExport virtual-workspace watch, how
  `denopod`/`trigger` are driven, token minting, or the OpenBao PKI flow.
- `r.registry-resolves-cluster-path` says "kubeconfig or cluster path"; the code
  only delegates to `r.store.ClusterPath` (`cluster_path.go:16-17`).
- `api-v1alpha1`: 70 of 118 interfaces are generated `DeepCopy*` methods.
- Refs in `specs/` and `arch.yaml` are opaque (`method:7392…`); only
  `context/*.md` maps them to names, and nothing gives line numbers.
- 4 of 17 contexts are `CodeSynced=False` (InterfacesMissing) on the default
  branch: declared names the index does not see (`admissionSource`,
  `summaryObserver`, shell variables, test functions).
- `file:` refs to `apply.sh`, `README.md`, `.tools/open-architecture/arch.yaml`
  are "unresolved" although the files exist; the resolver knows only indexed
  files (`abc/sync/sync.go:701`).
- Versus the hand-written `arch.yaml`: no `upstreams` (kcp, kine, openbao, deno,
  hono-pds), no orchestrators (15 there), no `types`, no trust-boundary tree, no
  repo root; all 17 contexts `upstream: self`, `dependsOn`/`overlay`/`orchestrator`
  empty; no contexts for the root (Makefile, go.mod, README), `docs/` (6 docs,
  3 ADRs), `third_party/`; no cross-context facts (e.g. `internal/provider`
  imports `denopod`). (The hand-written file is stale too: `validate.py` reports
  154 errors against the current tree.)

## 2. Structure and layout

- Layout navigable; `README.md` explains it.
- Duplication: the spec appears in `specs/` (18%), `context/` (21%, embeds the
  spec block) and `arch.yaml` (19%); observed interfaces again in `status/`;
  `graph/` 27.5%.
- The generated `arch.yaml` reuses the hand-written apiVersion/kind but fails its
  schema with 96 errors (`overlay`/`orchestrator`/`context` required, `'self'`
  invalid); on the feature branch `metadata.branch` says `open-architecture/deno-kcp`
  (bug: `abc/oabranch/oabranch.go:364` uses `Branch`, not `BranchFor`); flat list,
  not a tree.
- Graph: only `HAS_CONTEXT`/`REQUIRES`/`DECLARES` (614 edges); no `REFERENCES`
  and no CodeRef vertices (`oabranch.go:284` calls `graph.Build` without a
  `CodeRefs` map, `abc/graph/graph.go:315`); no `DEPENDS_ON`/`UPSTREAM`; numeric
  ids make the diff unreadable.
- `status/`: signal (CodeSynced, unresolved refs), but every code commit rewrites
  all 17 files for `observedCommit` alone.
- `changes/`: default branch has 17 code-to-spec records whose `agentLog` is the
  best plain-language summary of each context, buried; feature branch creates and
  deletes attempt files (`-a2`, `-a3`), so why attempts failed survives only in
  history; `progress` lists of 17+ timestamped entries.
- Commits: 130 subjects "open-architecture: deno-kcp: N modified", 79 of them "1
  modified" progress ticks; 2 of 130 carry a trailer (`Code-Commit`), none
  `Spec-Change` though the constant exists (`oabranch.go:48`); the two commits a
  human cares about (the `origin=clm` spec edits) are indistinguishable.

## 3. The feature-branch diff

`specs/` (135 of 865 diff lines, 16%) reads as an approvable spec change: intent
updated, `r.three-workspaces-at-root` replaced by `r.four-workspaces-at-root`,
7 new MUST/SHOULD requirements naming files, ports and flags, matching the code.
The other 84% is noise: `changes/` 391 lines, `arch.yaml` 138, `context/` 118,
`status/` 53, `graph/` 26. A machine-local path is baked into `r.bidder-pod`. The
diff lands with `CodeSynced=False` unflagged. It would be approvable as one
proposal commit with the spec delta, a generated per-branch `CHANGES.md`
(requirements added/removed/changed by id with text, plus the realization result),
and the derived files kept out of the reviewable diff.

## 4. Recommendations

1. `metadata.branch` follows the persisted branch (`oabranch.go:364`). Small.
2. CodeRef vertices and `REFERENCES` edges from `status.observed` + files
   (`oabranch.go:284`). Small.
3. Reviewable commits: subjects naming context and action, `Spec-Change:`
   trailers, progress-only SpecChange writes coalesced. Medium.
4. Per-branch `CHANGES.md`: requirement-level delta against the default branch
   plus the realize/verify outcome. Medium.
5. `file:` refs resolve against the git tree (`abc/sync/sync.go` ~:675-701). Small.
6. `dependsOn` from import edges in ingest (`impl/ingest/ingest.go:275` forces
   `self`) and a repo-root context. Medium.
7. Seed from an in-repo `arch.yaml` during populate (`impl/archkcp`). Large.
8. Stop duplication and noise: no spec block in `context/`, `observedCommit` once,
   generated `arch.yaml` schema-valid or its own kind. Medium.
9. Readable refs `name@file:line`; filter generated symbols (`zz_generated*`,
   `DeepCopy*`). Small.
10. Stronger model output: `cmd/` contexts declare their config surface, reject
    absolute machine paths, flag method-list-only requirements (summarize prompt
    + `abc/spec/validate.go`). Medium.
