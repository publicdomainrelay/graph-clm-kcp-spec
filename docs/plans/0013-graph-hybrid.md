# PLAN 0013 - the graph hybrid (structure + claims + search + CLM working memory + swarm)

Status: active. Owner: coordination agent. Executor: a Claude Code agent driving specd, which drives deepseek-claude (the 0011 dogfood pattern).

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
