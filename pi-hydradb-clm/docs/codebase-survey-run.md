# Codebase survey run: pi + pi-hydradb-clm + ArcadeDB

A recorded run of the extension doing the thing it exists for: point a coding
agent at a repository, have it build durable graph memory, then read the graph.

## Setup

- Fresh ArcadeDB 26.9.1, empty `databases/` directory, database `clm` created at
  boot. No prior nodes in the graph.
- `HYDRA_BACKEND=arcadedb`, Bolt `127.0.0.1:7688`, session key `codebase-survey-1`.
- pi 1.0.0, `--provider deepseek --model deepseek-flash --thinking low`, headless
  (`-p`), extensions disabled except this one (`-ne -e pi-hydradb-clm`).
- Repository surveyed: this repo (`hydradb/`), 12 turns, ~4 minutes.

Prompt:

> Survey this repository and build a complete understanding of its codebase.
> Ignore node_modules and generated artifacts. Read the key sources: the Go
> program under cmd/, the shell script under scripts/, AGENTS.md, and the
> pi-hydradb-clm TypeScript extension. As you learn each significant thing —
> each component, each file's role, each design decision, and how the pieces
> relate — store it with the hydradb_remember tool, and link each memory to the
> files it concerns via the files argument. Store at least 8 memories. Finish
> with a concise summary of the architecture.

## What it learned

Session-scoped counts for `codebase-survey-1`:

| | count |
| --- | --- |
| `PiSession` | 1 |
| `PiMemory` | 13 |
| `PiFile` | 41 (23 distinct paths — see defect 1) |
| `PiTurn` | 10 |
| `-[:REMEMBERED]->` | 10 |
| `-[:TOUCHED]->` | 21 |
| `-[:OCCURRED]->` | 10 |
| `-[:MENTIONS]->` | 20 |

Memories stored, all `kind="finding"`:

- Go `main.go`: OCI image payload extractor/packer
- `backend.ts` + `target.ts`: dual backend config resolution
- HydraDB operational facts learned by probing
- Cypher conformance probe: HydraDB 26/39, ArcadeDB 38/39
- `context-doc.ts`: CLM live-context document + budget projection
- `extension.ts`: pi wiring, hooks, tools, env options
- Tests: units, integration (live graph+LLM), conformance harness
- `graph.ts`: Bolt client over neo4j-driver
- `pi-hydradb-clm` is a pi extension (CLM-style graph memory)
- Repo `hydradb`: Go binary extractor + pi TS extension
- `cypher.ts`: query builders for the portable subset
- Shell script: fetch layout, extract, install graph binaries
- `schema.ts` + `ids.ts`: graph model and stable node ids

It also auto-indexed every file the `read` tool touched (21 `TOUCHED` edges) and
recorded each turn.

## Quality of what it learned

The memories are accurate and specific — they name real functions, real env
vars, real design decisions, including the ones that only exist because of
earlier debugging (`neo4j-driver ~5.26.0` pinned, "trust the image tag `0.2.0`,
not the binary's self-reported `0.1.0`"). The model cross-referenced the
conformance results it read out of `docs/cypher-conformance.md`.

Its closing summary correctly describes both halves of the repo (the Go OCI
extractor and the TypeScript extension), the module split, the data flow, and
the key design decisions. Memory bodies run 300-700 characters: dense, not
padded.

## Defects this run exposed

### 1. One file became two nodes (fixed)

41 `PiFile` nodes covered only 23 distinct paths; 18 paths had a duplicate. The
split is exact: 21 nodes hold absolute paths with `touches>0` (written by the
`tool_result` auto-indexer, which receives absolute paths from the `read` tool),
and 20 hold repo-relative paths with `touches=0` (written by `hydradb_remember`
from the model's `files` argument, which the model gave as
`pi-hydradb-clm/src/graph.ts`).

Node ids hash the path string, so the two spellings of one file never merge.
That halves the value of the file graph and splits `MENTIONS` edges across two
targets. Fixed by canonicalizing to an absolute path in both paths
(`canonicalPath` in `src/ids.ts`), with a unit test asserting both spellings
produce one node id.

### 2. A failed link leaves an orphan memory (mitigated)

Three of the 13 memories have neither a `REMEMBERED` nor a `MENTIONS` edge:

- `backend.ts` + `target.ts`: dual backend config resolution
- HydraDB operational facts learned by probing
- `cypher.ts`: query builders for the portable subset

`hydradb_remember` writes the memory vertex, then the session edge, then one
edge per file, as separate Bolt statements. A failure after the vertex commits
leaves a node with no edges. Replaying the exact write for the third orphan
against the same server succeeded, so the original failures were transient, not
semantic. The model saw the errors, retried some of them, and mis-counted how
many had failed — its own turn notes list four failures when the graph shows
three.

Mitigated by making the link phase best-effort: the tool now reports
`remembered ... linked to N file(s)`, or on failure says the memory is stored and
must not be re-stored. The graph stays usable either way, because the context
document selects memories by their `session` property, not by edge.

### 3. Turn summaries are truncated JSON (open)

`PiTurn.summary` stores `JSON.stringify(event.message).slice(0, 400)`. All ten
stored summaries are exactly 400 characters, so every one is cut off mid-JSON
and none is readable as a summary. They do not carry tool results either, which
is why the error text for defect 2 is not recoverable from the graph. Storing a
useful summary — or nothing — would be better than 400 bytes of broken JSON per
turn.

## Also observed

**ArcadeDB: `WHERE` after `OPTIONAL MATCH` does not filter.** The idiomatic
anti-join `MATCH (m:PiMemory) OPTIONAL MATCH (s:PiSession)-[:REMEMBERED]->(m)
WHERE s.id IS NULL RETURN count(*)` returns 13 on ArcadeDB (one row per memory);
Neo4j semantics give 3. This was found while inspecting, and is not currently
covered by the probe, whose cases assert only that a query parses and, where
`minRows` is set, that it returns rows.

**Correction:** an earlier version of this report credited the `.codegraph/`
directory and the `.gitignore` line to the agent. The operator ran `codegraph`,
not the agent. `codegraph init` created `.codegraph/codegraph.db` (908 KB) and
appended `.codegraph/` to the repo `.gitignore`.

**This database was mutated during inspection.** The replay in defect 2 added a
`REMEMBERED` and a `MENTIONS` edge to the `cypher.ts` orphan, and the fixture
nodes it created were deleted afterwards. The counts above were taken
session-scoped, after that cleanup, and the orphan count of three reflects the
state before the replay.
