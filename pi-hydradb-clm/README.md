# pi-hydradb-clm

A proof-of-concept [pi](https://pi.dev) coding-agent extension that gives the
agent a **durable, graph-backed live context** in
[HydraDB](https://github.com/hydra-db/hydradb), following the
[Context Language Models](https://github.com/facebookresearch/context-language-models)
(CLM) idea: the model does not just consume a context window, it *edits* one.

CLM's reference harness mirrors the model-visible conversation into a file in a
sandbox that the model rewrites with ordinary tools. This PoC keeps the same
contract but changes the medium: the mirror is a **HydraDB graph**, and the
model edits it through typed tools (`hydradb_remember`, `hydradb_recall`,
`hydradb_neighbors`, `hydradb_forget`). The graph is durable, so a fact stored
in one session is readable in the next one — which a temp file is not.

## What it does

| Hook | Behaviour |
| --- | --- |
| `session_start` | Upserts a `PiSession` node for the session key. |
| `tool_result` | Auto-indexes every path touched by `read`/`write`/`edit` as a `PiFile` node plus a `TOUCHED` edge. |
| `turn_end` | Records a `PiTurn` node plus an `OCCURRED` edge. |
| `context` | Renders the graph into a `[[HYDRA_CLM ...]]` document and appends it to the request as a system message. |
| `session_shutdown` | Closes the Bolt connection. |

Tools registered: `hydradb_remember`, `hydradb_recall`, `hydradb_neighbors`,
`hydradb_forget`, `hydradb_context`. Command: `/hydradb`.

Graph shape:

```
(PiSession {key, started, cwd, revision})
   -[:REMEMBERED]-> (PiMemory {kind, title, body, created, session})
   -[:TOUCHED]->    (PiFile   {path, touches, session})
   -[:OCCURRED]->   (PiTurn   {turnIndex, summary, created, session})
(PiMemory) -[:MENTIONS]-> (PiFile)
```

Every node carries an integer `id`, derived from a stable FNV-1a hash of a
content key, so re-remembering the same fact merges instead of duplicating.

## Layout

```
index.ts            pi extension entrypoint (pi.extensions)
src/ids.ts          pure: stable node ids, Cypher literal escaping
src/cypher.ts       pure: query builders for the HydraDB-compatible subset
src/context-doc.ts  pure: CLM live-context document + token-budget projection
src/schema.ts       pure: labels, edge types, property lists
src/graph.ts        I/O: Bolt client over neo4j-driver
src/extension.ts    pi wiring: tools, hooks, env options
test/units.test.ts  pure-module tests, no I/O
test/integration.test.ts  live HydraDB + live DeepSeek
```

## Running

```bash
npm install
npm test              # units + integration
npx tsx --test test/units.test.ts        # fast, no I/O
```

`npm test` passes `--test-force-exit`: `session.dispose()` does not emit
`session_shutdown`, so the extension's Bolt pool holds the event loop open after
the assertions finish. Closing the driver on `dispose()` instead would need a
hook pi does not currently offer.

Integration tests need a HydraDB node. Point them at one:

```bash
export HYDRA_BOLT_URL=bolt://127.0.0.1:7687
export HYDRA_TOKEN_FILE=/path/to/auth-token
```

If none is reachable, the harness starts one from `HYDRA_NODE_BIN`
(default `/tmp/hydradb-bin/graph-node`) with `HYDRA_LIB_PATH` on
`LD_LIBRARY_PATH`.

The backing LLM is DeepSeek. The key is read from `DEEPSEEK_API_KEY`, or from
the `deepseek-claude` launcher script if that env var is unset.

```bash
export HYDRA_PI_MODEL=deepseek-flash      # default; deepseek-v4-flash also resolves
```

## Install into pi

```bash
pi install ./pi-hydradb-clm
# or, for one run:
pi -e ./pi-hydradb-clm
```

## Configuration

| Env var | Default | Meaning |
| --- | --- | --- |
| `HYDRA_BOLT_URL` | `bolt://127.0.0.1:7687` | Bolt endpoint |
| `HYDRA_USER` | `neo4j` | Bolt principal (HydraDB accepts any) |
| `HYDRA_TOKEN` / `HYDRA_TOKEN_FILE` | `/var/run/secrets/slatedb-graph/auth-token` | Bearer token, min 32 non-placeholder chars |
| `HYDRA_CLM_SESSION` | `pi-<pid>` | Session key; the graph partition for one agent session |
| `HYDRA_CLM_BUDGET` | `2000` | Token budget for the injected live-context document |
| `HYDRA_CLM_AUTO_INDEX` | `1` | Index touched files |
| `HYDRA_CLM_ENABLED` | `1` | Master switch |

## Where this sits in the HydraDB ecosystem

HydraDB positions itself as agentic memory / a knowledge graph for LLMs
(GraphRAG). The `hydra-db` org already ships integrations for other harnesses:

| Repo | Harness | What it does |
| --- | --- | --- |
| `hydradb-mcp` | any MCP client | MCP server for store/recall/search, stdio or HTTP |
| `hydradb-claude-code` | Claude Code | plugin with workspace sync, prompt-time recall, memory capture, `/hydradb:*` skills |
| `openclaw-hydradb` | OpenClaw | auto conversation capture, recall, injection before every turn |
| `hydradb-crewai-plugin` | CrewAI 1.0-1.10 | `Storage` implementation via `ExternalMemory` |
| `hydradb-cli` | shell | agent-friendly CLI |
| `graphify` | Claude Code, Cursor, Codex, Gemini CLI | turns a codebase into a queryable knowledge graph |

Nothing targets **pi**, and none of them take the CLM "the model edits its own
context document" route — they inject recalled memory, they do not hand the
model a document to rewrite. This PoC is the pi integration and the CLM variant.

The database itself is AGPL-3.0; the integration repos are Apache-2.0.

## HydraDB notes (learned by probing `0.1.0`)

HydraDB is a Rust graph database: Bolt on `7687`, a health endpoint on `7474`
(`/healthz` only), Prometheus metrics on `9090` (`/metrics`, `/readyz`), a
slateDB object-store LSM underneath, and GraphBLAS kernels for matrix work.
It requires either TLS materials or `GRAPH_ALLOW_PLAINTEXT=true`, an auth token
file, and a storage backend (`CLOUD_PROVIDER=local` + `LOCAL_PATH`, or
`memory`).

The published README advertises a broader OpenCypher subset than the shipped
binaries accept. Against the `0.2.0` image (binaries self-report `0.1.0`), the
engine rejected multi-hop paths, variable-length relationships, bare
`MATCH (n)`, and `RETURN n`. Treat the list below as what this PoC was tested
against, not as the documented surface.

What works:

```cypher
MATCH (n:Label {prop: 'literal'}) RETURN n.other AS other
MATCH (n:Label) RETURN count(*) AS total
MATCH (a:Label {id: 1})-[:TYPE]->(b:Label) RETURN b.prop AS prop

UNWIND $rows AS row MERGE (n {id: row.id}) SET n:Label, n.p = row.p
UNWIND $rows AS row MATCH (a:Label {id: row.src}), (b:Label {id: row.dst}) CREATE (a)-[:TYPE]->(b)
UNWIND $rows AS row MATCH (n {id: row.id}) DETACH DELETE n
```

What it rejects: bare `MATCH (n)`, `RETURN n` (only `<binding>.<property>` or
`count(*)`), multi-hop or variable-length patterns, relationship patterns with
more than one type, `CREATE` followed by another clause, non-integer node ids,
and `DELETE` of a vertex with edges (needs `DETACH`). Write batches must be
`UNWIND`ed parameters, and integer fields must be Bolt integers, not JS floats.

`src/cypher.ts` encodes exactly this subset, which is why the extension has a
query-builder module instead of hand-written Cypher.

### Client compatibility

Use `neo4j-driver@~5.26.0`. Driver 6.x advertises the Bolt manifest handshake
(version `255.1`) first; HydraDB selects it but does not reliably complete the
v2 negotiation, so `verifyConnectivity()` fails with

```
RangeError: The value of "offset" is out of range. It must be >= 0 and <= 4. Received 5
  at ChannelBuffer.getVarInt ... at handshakeNegotiationV2
```

Measured against HydraDB 0.1.0: driver 6.2.0 failed 9 of 12 connections;
driver 5.26.0 (which does not offer `255.1`) succeeded 12 of 12.
