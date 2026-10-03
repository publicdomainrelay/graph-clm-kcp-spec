---
name: codebase-survey-eval
description: Run and grade a codebase-survey evaluation of the pi-hydradb-clm extension against a fresh graph backend. Use when measuring what the agent learns from a repository, validating that the context file and context graph still work end to end, or reproducing a recorded run in docs/.
---

# Codebase survey evaluation

Point pi at a repository with this extension loaded, let it build a live context
file and a context graph, then inspect both. This is the end-to-end evaluation
behind `docs/codebase-survey-run.md` and `docs/codebase-survey-run-2.md`.

The run is not deterministic — it drives a real model against a real database.
Grade it by inspecting artefacts, not by asserting an exact output.

## What you need

- A graph backend. Either one works; ArcadeDB is easier to start fresh because
  its data directory is just a working directory.
- `DEEPSEEK_API_KEY`, or the `deepseek-claude` launcher script on `PATH` that
  carries one.
- A CodeGraph index in the target repository (`.codegraph/codegraph.db`). Build
  it with `codegraph init` from the repository root. Without it the run still
  works, but every code reference lands in the managed zone's "not in the
  CodeGraph index" list instead of resolving to ids. Both are worth measuring.

## 1. Start a fresh backend

A fresh database is the point: the graph must contain only what this run
produced. Reusing one silently mixes runs and makes the counts meaningless.

```bash
# ArcadeDB, fresh data directory, Bolt on 7688
FRESH=$(mktemp -d /tmp/arcadedb-eval-XXXXXX)
cd "$FRESH"
ARCADEDB_HOME=/tmp/arcadedb/arcadedb-26.9.1 ARCADEDB_JMX=" " \
ARCADEDB_OPTS_MEMORY="-Xms256M -Xmx2G" \
JAVA_OPTS="-Darcadedb.bolt.port=7688 -Darcadedb.bolt.host=127.0.0.1 \
  -Darcadedb.server.rootPassword=clm-arcadedb-root \
  -Darcadedb.server.defaultDatabases=clm[root] \
  -Darcadedb.server.plugins=Bolt:com.arcadedb.bolt.BoltProtocolPlugin" \
  /tmp/arcadedb/arcadedb-26.9.1/bin/server.sh > "$FRESH/server.log" 2>&1 &
sleep 30 && grep -i 'bolt plugin started' "$FRESH/server.log"
```

Wait for `Neo4j-Bolt plugin started` and `Creating default database 'clm'`.
Bolt being absent from the `-base` distribution is documented in the README —
if the plugin line is missing, you have the wrong tarball.

Confirm it is empty before starting:

```bash
HYDRA_BACKEND=arcadedb HYDRA_BOLT_URL=bolt://127.0.0.1:7688 HYDRA_USER=root \
HYDRA_TOKEN=clm-arcadedb-root HYDRA_DATABASE=clm \
  npx tsx scripts/graph-dump.ts      # every label should report (0)
```

## 2. Run the survey

Run from the repository under test, so the extension's `read`/`grep` tools and
its CodeGraph lookup both resolve against it.

```bash
cd /path/to/repo
export DEEPSEEK_API_KEY="$(sed -n 's/^export ANTHROPIC_API_KEY="\(.*\)"/\1/p' "$(command -v deepseek-claude)")"
export HYDRA_BACKEND=arcadedb HYDRA_BOLT_URL=bolt://127.0.0.1:7688 \
       HYDRA_USER=root HYDRA_TOKEN=clm-arcadedb-root HYDRA_DATABASE=clm \
       HYDRA_CLM_SESSION=eval-$(date +%s)

npx --yes @earendil-works/pi-coding-agent@1.0.0 \
  -ne -e /path/to/pi-hydradb-clm \
  --provider deepseek --model deepseek-flash --thinking low --no-session \
  -p "Survey this repository and build a complete understanding of its codebase. \
Your live context file is your working memory: read it first, then keep it updated \
as you learn using your ordinary read/write/edit tools. Write code references as \
backticked repository-relative paths or symbol names so they can be resolved to \
CodeGraph ids. Also store each significant concept with the hydradb_remember tool, \
passing the relevant code_refs. Store at least 8 concepts. Ignore node_modules and \
generated artifacts. Finish with a concise summary of the architecture."
```

`-ne` disables extension discovery but keeps built-in tools; `-e` loads this one.
`--thinking low` keeps it cheap. Expect 3-8 minutes and a few hundred thousand
input tokens. Output is buffered until the end.

The live context file is at `${TMPDIR:-/tmp}/pi-hydradb-clm/$HYDRA_CLM_SESSION/live-context.md`
unless `HYDRA_CLM_CONTEXT_PATH` overrides it. Read it during the run to watch it
fill; it is rewritten every turn.

## 3. Inspect the two halves

**The context file** — the agent's own understanding. Read the model zone and
the managed zone separately:

```bash
CF="${TMPDIR:-/tmp}/pi-hydradb-clm/$HYDRA_CLM_SESSION/live-context.md"
awk '/HYDRA_CLM_MANAGED_BEGIN/{exit} {print}' "$CF"   # what the model wrote
wc -c "$CF"
```

**The context graph** — what it attached that to:

```bash
HYDRA_BACKEND=arcadedb HYDRA_BOLT_URL=bolt://127.0.0.1:7688 HYDRA_USER=root \
HYDRA_TOKEN=clm-arcadedb-root HYDRA_DATABASE=clm \
HYDRA_DUMP_SESSION=$HYDRA_CLM_SESSION HYDRA_DUMP_OUT=/tmp/eval-dump.json \
  npx tsx scripts/graph-dump.ts
```

`HYDRA_DUMP_SESSION` scopes every label to one session, which matters whenever
the database holds more than one run.

## 4. Grade it

There is no pass mark; these are the questions worth answering, and the
signatures of the defects found so far.

| Check | Healthy | Failure signature |
| --- | --- | --- |
| File vs index size | managed zone smaller than or comparable to the model zone | managed zone several times the model zone — the budget cap is not applied |
| File content | names real symbols, records invariants ("`resetLayout` refuses to delete a non-layout dir"), not just file names | a list of filenames with no claims about behaviour |
| Reference resolution | most backticked refs became `PiCodeRef` with real ids | a long "not in the CodeGraph index" list on a repo that is indexed |
| Unresolved list contents | only real symbols and paths, e.g. modules added after the index was built | CLI flags, whole commands, ASCII diagrams, absolute filesystem paths — the reference filter is off |
| Reference accuracy | a concept's refs live in the files the concept describes | refs pointing into `test/` for a concept about `src/` — bare-name ambiguity |
| Duplicate edges | `REFERENCES` edges ≈ distinct (concept, ref) pairs | the same ref repeated under one concept |
| Turn summaries | readable prose plus `called: read, grep` | exactly-400-character JSON fragments |
| File nodes | one node per distinct path | 2× nodes vs distinct paths — absolute/relative split |
| Concept ↔ code | every concept has refs | concepts with no `REFERENCES` edges |

A useful sanity check on the graph itself: `MATCH (n:PiCodeRef) RETURN
n.kind, count(*)` should show a mix of `file`, `function`, `import`, and
`constant`. All-`file` means symbol resolution is failing and only paths work.

## 5. Compare against the recorded runs

`docs/codebase-survey-run.md` (context-as-graph design) and
`docs/codebase-survey-run-2.md` (context-file design) hold the earlier numbers
and the defects each exposed. When a defect is fixed, re-run and confirm the
failure signature is gone from the table above.

## 6. Clean up

```bash
pkill -f "[A]rcadeDBServer"
rm -rf "$FRESH" "${TMPDIR:-/tmp}/pi-hydradb-clm/$HYDRA_CLM_SESSION"
```

The `[A]` bracket keeps the pattern from matching the `pkill` command itself,
which otherwise kills your own shell.

## Shorter loops while developing

The survey run is the slow end-to-end check. For most changes these are enough:

```bash
npx tsx --test test/units.test.ts                      # pure logic, no I/O
npm test                                               # units + conformance + integration
HYDRA_BACKEND=arcadedb ... npx tsx scripts/cypher-probe.ts   # engine conformance
```

The integration tests drive real pi sessions against a real backend and a real
model, so they catch wiring breakage the unit tests cannot. Reach for the full
survey run when the question is "what does the agent actually learn", not
"does this function work".
