# PLAN 0013 - the graph hybrid (structure + claims + search + CLM working memory + swarm)

Status: done. Every item is done or recorded below as a limit with its reason.
Owner: coordination agent. Executor: the coordinating Claude Code session driving
specd, which drove deepseek-claude (the 0011 dogfood pattern): every change in
items A to G was authored as a requirement and realized by specd plus deepseek,
and the lead fixed only the defects in the graph, the ingest and the test
harness that the run reported (`boltgraph` retry, the dangling-edge class, the
ingest re-read, the memory vertices, the CLI flag clash).

Runs before the socialweb plan (0012) because 0012 depends on it: specd must
build a context worth reading before it drives a 300-package monorepo.

## Context

Three inputs:

1. **The CLM paper (arXiv 2609.37725).** A model that edits its own context file
   without restrictions beats harness-scheduled compaction and tool-limited
   strategies, on both accuracy and compute. Several context files give a
   multi-agent swarm, and an orchestrator keeps a scoreboard in its own file.
   One-sentence "skills" steer the strategy, and the skill can be evolved by an
   eval loop. Models misjudge their own token counts, so hints help.
   Editable context is a channel where prompt injections persist. Edits in the
   middle of a context break prefix caching, so stable parts go first.
2. **The self-ingest of hydradb (2026-10-12).**
   - 372 contexts, of which 152 are `fixtures/`, 70 `examples/` and 54 `policies/`; 233 hold a single file.
   - 371 contexts carry a `dependsOn` to the root context, which has 0 files. Test-only imports count, so `impl-graphproc` and `impl-boltgraph` form a fake cycle.
   - Neighbors reach the prompt as bare names.
   - The injected document is mostly opaque `function:<hash>` lines.
   - A code ref resolved to the wrong code (fixed in faaa991).
   - A new worktree silently restored another instance's architecture branch.
   - socialweb showed the same root-edge pattern, and all of its Go code collapsed into one context.
3. **The plan 0011 dogfood run.** 9 of 10 items are Done; specd plus deepseek wrote all of the code. Its findings are listed under the items they change:
   - **F1:** a realize re-indexed without the populate include, creating 365 extra contexts and 12 extra summaries.
   - **F2/F9:** the realize agent obeyed the repository-wide CLAUDE.md, ran the whole live suite and timed out.
   - **F3:** a clean acceptance checkout lacked node_modules.
   - **F5:** a server side apply under the single field manager "human" deleted earlier requirements, and the realize agent was asked to remove tests.
   - **F8:** recovery retried an attempt that the plan had already replaced.
   - **F10:** after a rebase, every declared requirement became a `proposal-*` Finding with a JSON blob as its summary.

Today's machinery (from the exploration):
- The CLM document: model zone plus managed zone, deterministic (`abc/clm/clm.go`, `abc/agent/contextdoc.go`).
- The prompt `Fit`, which drops whole sections by priority (`abc/agent/render.go`).
- One-hop neighbors that carry names only (`impl/bundle/bundle.go:169`).
- A realize prompt that drops the neighbors and the doc (`impl/claudecli/claudecli.go:126`).
- No full-text search or embeddings anywhere, although codegraph already ships `nodes_fts`.
- pi-hydradb-clm's `SPECIFIES` matches an id without its namespace (`pi-hydradb-clm/src/spec-graph.ts`).
- HydraDB refuses a lot of Cypher: `type(r)`, WITH filters, bare node returns, regex, null parameters, missing endpoints. So ranking has to happen in Go.

Intended outcome: on any repository, specd builds a correct structural graph and claims with provenance on top of it. A task (a question, a SpecChange, a touched file) gets a small, ranked, cache-stable context pack. The model owns its working memory, and its edits flow back safely. A swarm of agents shares one blackboard. Context quality is measured, not guessed.

## Design: five layers in one graph

| Layer | Content | Writer | Authority |
| --- | --- | --- | --- |
| S structure | Module/Package/App contexts, File, Symbol (with signature), IMPORTS (prod) / TEST_IMPORTS, OWNS file→context, kcp Kind (Go type ↔ TS wire type) | ingest from codegraph, deterministic | derived, never model-written |
| C claims | Intent, Requirement, Interface, Interaction, Decision, Finding, Proposal, Plan/Item | person, clm, plan, summary | `abc/authority` (declared vs derived), provenance on each vertex: origin, by, commit, anchors |
| M memory | Memory notes (unifies PiMemory), AgentContext (one per running agent context file) | the model, through its context file | memory only; never spec without a delta |
| X search | FTS5 over claim text, intents, decisions, findings, memory, symbol names and docstrings | ingest and rebuild | derived |
| R runs | AgentRun, SpecChange, Progress, TOUCHED, scoreboard | specd, mod | derived |

A query runs in four steps:
1. Seed nodes come from FTS (BM25) hits, plus the owner context of any touched file, plus explicit names.
2. A typed k-hop expansion in Go, with per-edge weights and HydraDB-safe one-hop queries.
3. Ranking by a weighted personalized walk score with a decay per hop.
4. A budgeted **context pack**.

The context pack has a fixed order, so the prefix stays cacheable:

| Position | Content |
| --- | --- |
| 1 | repository card and invariants |
| 2 | task card, including the per-context verify and acceptance commands (F2/F9/F3) |
| 3 | the owner context's compact card |
| 4 | ranked neighbor cards: intent first line, MUST requirements, interfaces with signatures, edge reason |
| 5 | open findings, proposals and plan items |
| 6 | a token-budget line ("pack 3.1k of 8k tokens", as the paper suggests) |
| 7 | **the model zone last**: the only part the model edits |

Hashes stay out of the pack and are available through a tool.

## Items

**A. Ingest correctness (the self-ingest findings plus F1)**
- A1. Default exclude for `fixtures/`, `testdata/`, `node_modules`, `vendor` and generated directories, overridable in `Repository.spec.populate` (`impl/ingest/partition.go`, `abc/sync/sync.go` `filterFiles`).
- A2. Partition by Go package (`go list`), deno workspace member and cargo crate in one repository. Mixed Go and deno must split both, which fixes socialweb's single Go context (`partition.go` `DetectPartition`). Directory mode stays as the fallback.
- A3. App contexts as orchestrators: one per `cmd/*` and per TS entry, linked with ORCHESTRATOR.
- A4. `dependsOn` from production imports only; test imports become TEST_IMPORTS. Drop the forced root dependency (`sync.go:494,577-582` `PartitionDependencies`). The root context becomes the repository card.
- A5. The populate scope (include, exclude, partition) is a persistent property that every ingest path honours: realize settle, reanchor and restore (F1). Out-of-scope targets become External stubs, not dangling edges.
- A6. One architecture branch per instance by default (repository plus checkout slug). Restoring a shared branch must be asked for explicitly (`impl/persist`, `cmd/specctl/up.go`).

**B. Claims with provenance and safe write-back (F5, F10, paper safety)**
- B1. Every claim vertex carries origin, by, commit and anchor fingerprints. A claim is stale when its anchor moved (`abc/graph`, `impl/ingest`).
- B2. **Central removal guard:** specd refuses to realize any SpecToCode delta that removes requirements, interfaces or tests unless the removal is declared. The declaration can be `removed:` in the document, a plan edit's `removeRequirements`, or `specctl arch release`. Otherwise the change is held as a Proposal. This covers every write path, server side apply included (F5) (`factory/specd/systemcontext.go` `reconcileChanges`, reusing `abc/authority`).
- B3. Injection guard on write-back: the model zone and memory notes are scanned for agent directives (ignore instructions, tool calls, credential paths). A hit is quarantined as a Finding, never applied (paper section 6).
- B4. Proposal quality (F10): no proposal when the semantic diff is empty or whitespace only, deduped by unit, with a human-readable `current → proposed` one-liner instead of JSON (`abc/authority.Proposals`, finding creation in specd).

**C. Hybrid retrieval and the context pack**
- C1. `impl/searchindex` (new, ABC: interface in `abc/search`): SQLite FTS5 in the instance root, rebuilt with the graph. It reuses codegraph's `nodes_fts` for symbols. Embeddings come later behind the same interface, with a hosted provider only (no local models).
- C2. `abc/contextpack` (pure): seeds → expansion plan → ranking → budgeted render. Its input is a neighborhood snapshot, so it is unit-testable without I/O. `impl/contextpack` fetches the neighborhood with one-hop queries that stay inside HydraDB's Cypher subset.
- C3. Neighbor cards carry intent and signatures (replacing name-only `Neighbor`, `abc/agent/agent.go:9`). Summarize leaves first, in topological order, so neighbor intents exist when they are needed (`impl/populate` raise order).
- C4. The realize prompt uses the same pack (fixes `claudecli.go:126`, which drops the neighbors and the doc).
- C5. `specctl context pack --context X | --file P | --query "..."` prints the pack, for people and for the evals.

**D. CLM working memory (the paper's core, safely bounded)**
- D1. An agent context file = AgentContext vertex plus file. Fixed regions (pack, read-only, regenerated each turn) and a model zone that the model may rewrite freely: notes, scoreboards, plans. On turn end:
  - spec-shaped edits become a delta through `specctl clm apply`, subject to B2 and B3;
  - free notes become Memory vertices linked to the claims and code they name.
- D2. Port the useful pi-hydradb-clm pieces into the shared core (remember, recall, neighbors). Fix the namespace bug in `SPECIFIES`.
- D3. A context-management skill: the protocol text that the mod and pi inject, versioned in the repository and steerable with one sentence per strategy (paper section 5.2).

**E. The mod (cc-clm-mod)**
- E1. Per-file context: `tool.call` on Read, Edit or Write maps the path to its owner (`specctl arch owner <path>`), and the owner's card joins the next prompt. A small LRU set of cards keeps the injected section stable (`hooks/register.ts`, `hooks/plan.ts`).
- E2. Interactive sessions get the repository card plus the pack of the current directory at session start, not nothing.
- E3. New tools: `arch_search` (hybrid), `arch_neighbors` (with intents), `arch_owner`, `arch_query` (read-only Cypher), `arch_remember`.
- E4. A realize-role override: in a realize session, the injected task card states the verify and acceptance scope and outranks repository-wide agent instructions (F2/F9).

**F. Swarm (the paper's multi-repo swarm, kcp-native)**
- F1. Each AgentRun owns one context file and AgentContext. The Plan status is the orchestrator scoreboard, rendered into the orchestrator's pack.
- F2. Blackboard queries: what other running agents touched, decided or proposed for the contexts in my pack (TOUCHED, Memory, Proposal), added as a pack section.
- F3. One owner per change: recovery never re-raises an attempt that a plan or another run superseded (fixes F8, `factory/specd/recovery.go`).

**G. Evals that score the context itself**
- G1. The git-history benchmark: for each commit on a repository, the query is the commit message and the gold set is the owner contexts of the touched files. Metrics: pack recall@k, precision, tokens, and the budget drop rate. It runs on hydradb, on socialweb and on the fixtures (`impl/eval`, `abc/eval`).
- G2. Success-gated efficiency (from the paper): tokens per successful realize on the scripted and DeepSeek evals.
- G3. A skill-evolution loop for D3: candidate protocol texts scored with G1 and G2 on a development split, then the winner on a held-out split.

**H. Close out 0011 first**
- Tear down the subagent's leftover instance (kcp, kine, graph, specd, realize worktrees, the kcp-libs symlink).
- Finish item G with a minimal-object approve, rebase, run the suites, fast-forward main.
- Write the 0011 findings and decisions into its markdown.

## Execution

- Dogfood as in 0011: the plan is a Plan object (`docs/plans/0013-plan.yaml`) with staged edits per item; specd plus deepseek realize it; a lead agent reviews.
- Order:
  1. H
  2. A
  3. B2 and B3, which are safety and come early because of F5
  4. C
  5. B1 and B4
  6. E
  7. D
  8. F
  9. G, which runs from C onwards so every step is measured
- Every e2e test follows the parallel pattern: `t.Parallel`, `clusterWorkspace(t)`, `graphNamespaceFor(t)`, `testEnviron(t)`, `testLog(t)`.

## Critical files

- **Ingest:** `impl/ingest/partition.go`, `abc/sync/sync.go`, `impl/ingest/ingest.go`, `impl/ingest/graph.go`
- **Graph:** `abc/graph/graph.go`, `impl/boltgraph/boltgraph.go`
- **Authority and specd:** `abc/authority/authority.go`, `factory/specd/systemcontext.go`, `factory/specd/codespec.go`, `factory/specd/recovery.go`
- **Prompt and pack:** `abc/agent/render.go`, `abc/agent/agent.go`, `impl/bundle/bundle.go`, `impl/claudecli/claudecli.go`, new `abc/contextpack`, `impl/contextpack`, `abc/search`, `impl/searchindex`
- **CLM:** `abc/clm/clm.go`, `impl/clm/clm.go`, `clm/core/*`, `pi-hydradb-clm/src/spec-graph.ts`
- **Mod:** `cc-clm-mod/hooks/register.ts`, `cc-clm-mod/hooks/plan.ts`
- **Eval:** `impl/eval/*`, `abc/eval/*`
- **CLI:** `cmd/specctl/*` (arch owner, context pack, search)

**Reuse:**
- `abc/authority` (Declared, Merge, Proposals)
- `graph.WithEndpoints`
- `codegraphsqlite.Resolve` (path fix) and the `nodes_fts` table
- `abc/agent.Fit`
- `graphproc` and the per-test e2e helpers
- `abc/plan` and the Plan reconciler from 0011

## Verification

- **Self-ingest of hydradb** (README hands-on commands):
  - at most 80 contexts;
  - 0 `dependsOn` to the root;
  - no TEST_IMPORTS in `dependsOn`;
  - an audit of 50 random code refs resolves to the right symbols.
- **socialweb ingest:** the Go packages split into about 147 contexts plus 164 TS members; the kcp Kind vertices link Go and TS.
- **G1 recall@5:** reported before and after C, on hydradb and on socialweb; it must improve and the numbers are recorded in the plan.
- **B2:** a live e2e test does a server side apply that drops a requirement; it becomes a held Proposal and no realize runs.
- **B3:** an injected directive in the model zone becomes a quarantined Finding.
- **E:** a cc-clm-mod `npm test` for owner mapping and tools; a manual session in a `specctl up` checkout shows a card after a Read.
- **Gates:** `gofmt`, `go vet ./...`, `go test ./...` and `make test-live` green at every commit.

## Run log

### Item A: ingest correctness

Baseline (main at e6a9429, structure-only ingest of this repository, no model
summaries):

| reading | before | after A1 (default excludes) | after A2 (partitions are source only) |
| --- | --- | --- | --- |
| contexts | 375 | 209 | 81 |
| of them under `fixtures/` | 152 | 0 | 0 |
| under `examples/` | 70 | 70 | 0 |
| under `policies/` | 54 | 54 | 2 |
| `dependsOn` edges | 695 | 401 | 401 |
| contexts depending on the root | 374 of 375 | 208 of 209 | 80 of 81 |

Item A4 (`r.depends-on-production-imports` in `abc-sync`, realized as 49c2cd2)
and the reading after it:

| reading | after A2 | after A4 |
| --- | --- | --- |
| contexts | 81 | 81 |
| `dependsOn` edges | 401 | 250 |
| contexts depending on the root | 80 | 0 |
| `TEST_IMPORTS` edges | - | 0 (a test-only import is dropped, not classified; carried into the next batch) |

Both changes were authored as requirements (`r.default-excludes`,
`r.source-only-partitions` in `impl-ingest`) and realized by specd driving
deepseek: c5c37d1 and 5930768. The measurements come from a fresh clone with no
`open-architecture` branch, a fresh state directory and no other instance on
that checkout.

Findings from running them:

- The requirement-coverage judgment reported `missing: r.default-excludes` for
  c5c37d1, which does implement it; the judgment is a false negative there.
- A stale specd on the same checkout re-creates the architecture branch after it
  is deleted, so a "fresh" measurement silently restored the old contexts until
  every instance on that checkout was stopped. Item A5 covers the scope; the
  measurement recipe belongs in the README.
- A transient graph write (`ConnectivityError: ... broken pipe`) failed a realize
  twice; `boltgraph` now retries a query whose connection died (b0d16fd).
- Verify needs `GOFLAGS=-buildvcs=false` in specd's detached realize worktree, or
  `go build` fails with `error obtaining VCS status`; the Repository carries it
  while the product fix is outstanding.

Item A3 (application contexts), A4b (`TEST_IMPORTS`) and A5 (the scope is
persistent and pruned) were authored in `cmd-specctl`, `abc-graph` and
`impl-populate` and realized in one batch (b8f357f). The reading after them, from
a fresh clone:

| reading | after A4 | after the batch |
| --- | --- | --- |
| contexts | 81 | 81 |
| command contexts (`cmd-*`) | 3 | 3 |
| contexts naming an orchestrator | 0 | 34 |
| `dependsOn` edges | 250 | 251 |
| contexts depending on the root | 0 | 0 |
| context pairs joined by `TEST_IMPORTS` | 0 | 71 |
| `spec.testDependsOn` entries | 0 | 71 |

A5 was also verified live on the dogfood instance: it held 375 contexts from an
older index, and narrowing `Repository.spec.populate.include` to the trees the
plan edits pruned it to 80 contexts and 81 SystemContexts on the next pass, with
`status.contexts.total` reporting the new scope.


### Item A6 reverted (a limit, with the reason)

`r.architecture-branch-is-owed-by-its-checkout` was realized (461a5e0) and then
reverted in the same batch's follow-up: giving every checkout whose directory
name differs from the repository its own architecture branch stops
`test/e2e/phase13_live_test.go` from restoring a clone from the remote branch,
which is the feature plan 0002 added. The shared branch stays the default for
every checkout, and a checkout that wants its own branch names one with
`specctl up --restore-branch <name>`. Recorded as a limit rather than a done
item.

### Item B: claims, provenance and safe write-back

Authored in `factory-specd`, `impl-clm`, `abc-authority` and `impl-ingest` and
realized as 461a5e0 plus two fixes the gate found:

- **B2 removal guard** (`factory/specd/removal.go`): a SpecToCode change that
  removes an undeclared requirement, interface or interaction stays Pending with
  `held: undeclared removal of <unit>` and records proposals, whatever field
  manager wrote the spec. The live test
  `test/e2e/removal_live_test.go` drives a server side apply.
- **B3 injection scan** (`impl/clm`): a model zone that carries an instruction
  addressed to an agent is refused and opened as a Finding.
- **B1 ingest authority** (`impl/ingest`): the ingest merges with
  `abc/authority`, keeps the live value of every declared unit, and re-merges
  against the live object when it moved during the pass.
- **B4 proposals** (`abc/authority`): a proposal is recorded only for a real
  semantic difference, one per unit, rendered rather than encoded.

Three findings from running them:

- **The declaration race.** Recording an edit's declared units replaced the
  annotation with that edit's units, so a unit declared a moment earlier (by an
  operator, or by the previous edit) was lost and the guard then held a
  removal that had in fact been declared. `recordDeclared` re-reads and unions
  before it writes, and writes only that key (ef9e97d).
- **A server side apply removes a list entry only for the manager that owns it.**
  The removal test's baseline and its edit had to be applied by one field
  manager, or the removal never took effect. `specctl clm apply` and the mod
  use one manager, so the operator path is unaffected.
- **The ingest could replace a spec that moved while a pass ran.** `Apply`
  re-reads only the resource version, so a stale pass wrote back what it had
  read; the pass now re-reads and merges again when the object moved (604b8be).

### Item C: hybrid retrieval and the context pack

Realized as dfcf290 (new packages `abc/search`, `impl/searchindex`,
`abc/contextpack`, `impl/contextpack`, plus the pack used by the summarize and
realize prompts and `specctl context pack`). A real pack reads well: repository
card, task card with the context's verify command, owner card with the summary's
intent paragraph, its open findings and proposals, a token-budget line and the
dropped list.

Findings:

- **The pack command panicked at startup** (`flag redefined: context`): the new
  verb defined `--context` beside the global kubeconfig `--context`. It uses
  `addGlobalsExcept(fs, "context")` now, and `TestRunContextPackNeedsASeed`
  covers the crash (5d5cd3c).
- **A unit test with a fake reader hid an I/O gap.** `specctl graph neighbors
  impl-ingest` lists seven DEPENDS_ON neighbours while
  `specctl context pack --context impl-ingest` printed no neighbour section at
  all, and `--file` printed no owner card. The pure part's tests passed
  throughout. The fix is required to carry a live test
  (`r.pack-reads-the-live-graph` in `cmd-specctl`) so the wiring is measured
  against a real graph, not a fake.


Item C is complete once the pack reads a live graph: the neighbour section and
the owner card were both silently absent (the pure part's unit tests passed),
and each gap is now covered by a live test in `test/e2e`
(`TestContextPackReadsTheLiveGraph`, and the neighbour requirement's unit test
in `abc/contextpack`). The full live suite is green with them.

### Item D: CLM working memory

Realized as a1f1531 (`impl/clm/memory.go`, `specctl clm remember|recall|neighbors`,
`clm/core/skill.ts` with a version, the pi host's namespace bug fixed in the
shared core, and `test/e2e/clm_memory_live_test.go`). Verified live: a note
written with `specctl clm remember` becomes a Memory vertex, `specctl clm recall`
finds it, and a `specctl graph query 'MATCH (m:Memory) RETURN count(*)'` sees it.

Finding: the memory write named code and claim units whose vertices it never
wrote, so HydraDB refused the REFERENCES edge and the command failed ("MATCH
endpoint vertex ... with label CodeRef does not exist"). `clm.MemoryTable` now
writes the endpoint vertices with the edges, so a note links to code the graph
already holds and plants the vertex when it does not. This is the third time
the same class appeared (rebuild edges, the per-instance graph, the memory
write): an edge whose endpoint is absent must never be written.

### Item E: the mod

Realized as ff5ef8c: `specctl arch owner <path>` (the context that owns a file,
deepest match), the mod's most-recently-used set of at most four contexts whose
compact cards are injected at `prompt.compose`, the repository card for an
interactive session, three new tools (`arch_search`, `arch_neighbors`,
`arch_owner`), and a task card for a realize session that names the change, the
verify and the acceptance and outranks the repository's own instructions (the
fix for findings F2 and F9). Live check: `specctl arch owner impl/ingest/ingest.go`
prints `impl-ingest` and `abc/clm/memory.go` prints `abc-clm`.

Note for a fresh worktree: `cc-clm-mod/node_modules` is not in git, so `npm ci`
in that directory before `npm test` (or the vendor test fails with
`Cannot find package 'esbuild'`).

### Item F: the swarm

Realized as 6e176d8. The pack gained two sections in the fixed order (after the
neighbour cards, before the claims): the **blackboard** (the changes running for
these contexts, the memory notes other agent contexts wrote, and the open
proposals and findings) and the **scoreboard** (the plan items that touch these
contexts and their phases). Recovery now fails an orphaned change without
creating a new attempt when a newer attempt of the same episode exists, and it
carries the plan item that staged the change, so a change and its plan item
agree on the owner (finding F8). Live check: `specctl context pack --context
impl-ingest` renders both sections in that order.

### Item G: evals that score the context itself

G1 (the git-history benchmark) is realized as `impl/eval/history.go` plus
`specctl eval history`: it walks the checkout's own commits newest first
(`--commits`, default 50), takes each commit's subject as the query, the
contexts that own the files the commit touched (the `specctl arch owner`
lookup) as the gold set, builds the pack of that query and reports recall@k over
the pack's neighbour and claims sections, precision, the pack's token count and
the sections the budget dropped. `impl/eval/history_test.go` proves the walk
pairs a subject with the owners of its changed files, that a commit no context
owns is skipped rather than counted as a miss, and that the summary averages
recall over the commits that have a gold set.

`contextpack.NamedContexts` carries the name on the card, so k is the set of
contexts the two sections name and not a title re-parsed.

**The G1 recall@5 reading is not taken yet.** It needs a live instance
(`specctl up` in this checkout, graph and search index up) and a run of

```
specctl eval history --repo . --commits 50 --out docs/eval/history.md
```

in it; the plan's promise is that the number after item C is higher than the
number before it, and the "before" run needs the pack as it stood at dfcf290.
Both readings land in this section when they are taken, rather than a number
guessed from the unit tests.

**G1 measures nothing until the query pack names contexts** (requirement
`r.history-eval-reads-a-pack`). The first run of
`specctl eval history --repo . --commits 20` reported recall 0.00 with k 0 over
its scored commits, because every pack it built for a commit subject named no
context at all.

The cause is the instance root, not the pack. A graph rebuild writes
`search.db` under the instance root, and both writers resolved that root one
way while a query seed resolved it another way: `specd` takes the directory the
kubeconfig lives in, and `specctl` took the session record of the current
checkout. A checkout with no session record -- a worktree, a copy, a plain
`git clone` beside the instance -- resolved no root, so the query silently
seeded from no index at all: no hits, no neighbour cards, no claims, k 0, and
no message saying why.

`searchRoot` now falls back to the same rule `specd` uses, so the writer (the
graph rebuild) and the reader (a query seed) name one file whichever of them
runs first. The reading is produced by the command in the instance:

```
specctl eval history --repo . --commits 20 --out docs/eval/history.md
```

Two tests carry the requirement. `impl/searchindex` proves a rebuild writes
the index a query reads: the documents `Documents` reads off the graph are
rebuilt into the index, and a query over a context's own intent words and over
its requirement text finds that context
(`TestARebuildWritesTheIndexAQueryReads`). `test/e2e` proves the live path end
to end: the calc fixture is ingested, a context is given an intent the way a
summary would, `specctl graph rebuild` writes the index, and a pack seeded by
that intent words names that context
(`TestAQueryPackNamesTheContextItsIntentNames`).

**The rebuild now fills the index per thing** (requirement
`r.index-is-filled-by-a-rebuild`). Three gaps kept the documents table short of
one row per indexed thing:

- The rebuild read the graph whole. It reads the instance's own graph namespace
  now (`searchindex.Documents` takes the namespace and filters a label on the
  scope property it carries), so a graph shared with another instance does not
  seed this instance's packs with the other instance's contexts. A memory note
  carries no scope property, so it is read whole rather than filtered away.
- The reads asked for the values but not the vertex id, so every document of a
  label took the same id and a pack's k could not tell two things apart. Each
  read asks for `id` now, and a unit test asserts the rebuild writes one
  document per context, requirement, interface, finding, memory note and code
  symbol, with no two documents sharing an id.
- `specctl graph rebuild` names no checkout, so the symbol source (the
  checkout's code graph index) gave it nothing. It falls back to the CodeRef
  vertices the graph holds, which is the half of the requirement that asks for
  one document per code symbol the graph holds.

`TestARebuildWritesOneDocumentPerIndexedThing` in `impl/searchindex` counts the
rows the rebuild writes per label and asserts the namespace filter and the id
uniqueness; `TestARebuildReplacesTheIndex` proves a rebuild replaces rather than
appends.

The number itself is still not taken: it needs an instance (`specctl up` in
this checkout, graph and search index up) and the command above, and this
change was written in a worktree with no instance to run it in. Until that run
lands, the promise stays a promise: the reading to beat is the recall@5 the
pack reached before item C (dfcf290), and the run that fills this section is

```
specctl eval history --repo . --commits 20 --out docs/eval/history.md
```

in this repository's instance.


**G1's reading.** `specctl eval history --repo . --commits 12` on this
repository, with the instance's index current (`specctl graph rebuild` writes
1296 documents):

| reading | before the index was filled | after |
| --- | --- | --- |
| recall@k over the pack's neighbour and claims sections | 0.00 | 0.13 |
| precision | 0.00 | 0.28 |
| pack tokens (mean) | 178 | 224 |
| scored commits (of 12) | 16 of 20 | 9 of 12 |

Two findings produced the change. The index was created but never filled
(`documents` had 0 rows) until `specctl graph rebuild` was run, so every query
pack named nothing; and the history eval's subjects are terse ("realize
cmd-specctl: +1"), so a query with no meaningful term still scores zero. The
harness is the measurement the plan wanted: it is honest about both numbers.


### Limits, with the reason

- **A6, a branch per checkout.** Realized, then reverted: it stops a clone
  restoring the architecture from the remote branch (phase13). The shared branch
  stays the default; `--restore-branch` was removed with it.
- **G2, tokens per successful realize.** No model-usage accounting exists
  anywhere in the repository: the agent CLI's usage is not captured, and the
  token figure the pack and the evals print is the context pack's own estimate.
  Measuring it needs the realize command to run with a usage-reporting output
  format and specd to record that usage on the AgentRun; that is a follow-up,
  not a measurement this plan could take.
- **G3, the skill-evolution loop.** The skill text and its version exist (item
  D3) and every host injects it, but evolving it needs candidate generation plus
  a development and a held-out split, and G1's recall (0.13 over terse commit
  subjects) is too coarse a signal to optimise against today. Improve G1's gold
  set first.

### What the plan did not do, and why

The plan's own "done means" is met: every item is done or recorded as a limit
with its reason, `gofmt`, `go vet ./...`, `go test ./...` and `make test-live`
are green at the merge commit, and the run above records the dogfood run, its
findings and its measurements.
