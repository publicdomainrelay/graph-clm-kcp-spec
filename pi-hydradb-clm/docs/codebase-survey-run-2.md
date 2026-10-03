# Survey run 2: context file as brain, graph as index

Second recorded run, after the redesign. Same repository, same model, fresh
database. The difference is the architecture: the live context file is now the
central artefact and the graph indexes it, instead of the graph being the
context.

## Setup

- Fresh ArcadeDB 26.9.1, empty `databases/`, database `clm` created at boot.
- `HYDRA_BACKEND=arcadedb`, Bolt `127.0.0.1:7688`, session key `codebase-survey-2`.
- pi 1.0.0, `--provider deepseek --model deepseek-flash --thinking low`, headless.
- CodeGraph index present at `.codegraph/codegraph.db` (built by the operator
  before this run: 248 nodes, 678 edges, 18 files).
- Context file: `/tmp/pi-hydradb-clm/codebase-survey-2/live-context.md`
  (directory `0700`, file `0600`).

## What the two halves produced

| | value |
| --- | --- |
| context file, model zone | 104 lines, 5,268 chars (~1,317 tokens) |
| context file, managed zone | 21,734 chars (~5,433 tokens) — see defect 1 |
| `PiMemory` | 12 concepts |
| `PiCodeRef` | 206 nodes |
| `-[:REFERENCES]->` | 236 |
| `PiFile` / `PiTurn` / `PiSession` | 27 / 16 / 1 |

Code reference kinds resolved: 66 imports, 54 functions, 37 constants,
18 files, 14 interfaces, 10 methods, 3 type aliases, 3 variables, 1 class.

## Analysis of the model's understanding

**The file reads as a map, not a transcript.** It opens by naming the two halves
and the module boundary between them, then goes function-level:

> `run` resolves a remote repository via `oras-go`, selects the platform
> manifest (`selectPlatform`, `isIndex`), fetches layers (`fetchManifest`),
> extracts only `payloadFiles` … then packs a new OCI image layout (`pack`,
> `buildLayer`) tagged `hydradb-binaries:<tag>`. `resetLayout` refuses to delete
> a non-layout dir.

That last clause is the interesting one: it did not just list `resetLayout`, it
recorded the invariant it enforces. The same happens elsewhere — "Always UNWIND
batch, never bare project a node" for `cypher.ts`, "Node ids hash content keys,
so re-remembering merges" for `ids.ts`.

**It captured constraints the code does not state.** The context file records
the repository's governance (AGENTS.md: caveman-terse STE style, commits as you
go, never push, never edit outside `$PWD`) alongside the architecture. Those are
process rules, not code, and they are exactly the kind of thing an agent loses
on compaction and a code index can never hold.

**It self-reported its own blind spot.** Closing note: "CodeGraph index is
partial — shell script, docs, and `codegraph.ts` are not indexed, so those refs
stay unresolved." That is correct: `.codegraph/` was built before `codegraph.ts`
existed, so the extension's newest module has no ids, and the model noticed
rather than silently citing nothing.

**The graph adds what the file cannot.** The file says what the agent believes;
the graph says what each belief is attached to. Each concept carries its code:

| concept | refs |
| --- | --- |
| Conformance probe and measured engine divergences | 40 |
| Test harness and recorded survey defects | 29 |
| `cypher.ts`: builders for the engine-portable Cypher subset | 26 |
| Go: OCI payload extractor and packer in `main.go` | 25 |
| `graph.ts`: Bolt client over neo4j-driver, pinned `~5.26.0` | 22 |
| Graph model: labels, edges, stable FNV-1a node ids | 20 |
| Extension wiring: hooks, tools, command in `extension.ts` | 19 |
| Dual backend config | 18 |
| `context-doc.ts`: CLM document, managed zone, token budget | 15 |
| Repo `hydradb`: Go OCI extractor + pi TS extension | 14 |
| `codegraph.ts`: resolve backticked refs to CodeGraph ids | 8 |
| Shell: install graph binaries | 0 (not in the index) |

Compare with run 1, which produced 13 prose summaries and no code attachment.
This run produced fewer, better-scoped concepts, each anchored to the symbols it
describes, plus a readable document tying them together.

## Defects this run exposed

### 1. The index was four times the size of the brain (fixed)

Managed zone 5,433 tokens against a 1,317-token model zone, and it ignored
`HYDRA_CLM_BUDGET` entirely — the budget only ever governed the old
graph-rendered document, not the file. Every request paid for a 206-entry list.

The managed zone is now rendered through the same `selectWithinBudget` the rest
of the document uses, capped at 60% of the configured budget, with a trailing
line giving the count of references withheld and pointing at `hydradb_neighbors`
for the full set.

### 2. Duplicate `REFERENCES` edges (fixed)

`CREATE` in this Cypher subset is not idempotent and the model called
`hydradb_remember` with repeated references, so one concept ended up with the
same edge three times. `linkOnce` now tracks `(concept, codegraph_id)` pairs for
the life of the session and creates each edge once.

### 3. Bare symbol names attached the wrong code (fixed)

The `Conformance probe` concept was attached to `4d8ca5bd…` (`reason`) and to
the variables `graph` and `backend` — all from `conformance.test.ts`, not from
the files the concept describes. A bare name such as `graph` matched symbols in
several files and the resolver returned them all.

Resolution is now ordered — exact `qualified_name` first, then `name` — and a
bare name that matches symbols in more than one file resolves to nothing.
`hydradb_remember` reports those as ambiguous and names the candidate files, so
the agent can quote a path instead of the extension silently attaching the wrong
node. Missing a reference is recoverable; attaching a wrong one is not.

## Caveat

The three defects above were found by inspecting this run's output, and were
fixed afterwards. The counts and the analysis in this document describe the run
as it happened, before those fixes.
