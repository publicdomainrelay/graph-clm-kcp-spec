import assert from "node:assert/strict";
import { test } from "node:test";
import {
  edgeSelect,
  vertexCount,
  vertexSelect,
  vertexUpsert,
} from "../src/cypher.ts";
import { canonicalPath, cypherLiteral, cypherString, nodeKey, stableNodeId } from "../src/ids.ts";
import {
  MANAGED_BEGIN,
  MANAGED_END,
  composeContextFile,
  estimateTokens,
  extractReferences,
  isCodegraphId,
  isReferenceCandidate,
  splitContextFile,
  summarizeTurn,
} from "../src/context-doc.ts";
import { touchedPaths } from "../src/extension.ts";
import { parseBackend } from "../src/backend.ts";
import { resolvePartialTarget } from "../src/target.ts";

test("stableNodeId is deterministic and non-negative", () => {
  const a = stableNodeId("session:alpha");
  const b = stableNodeId("session:alpha");
  const c = stableNodeId("session:beta");
  assert.equal(a, b);
  assert.notEqual(a, c);
  assert.ok(a >= 0 && Number.isSafeInteger(a));
});

test("cypher literals escape quotes and newlines", () => {
  assert.equal(cypherString("it's"), "'it\\'s'");
  assert.equal(cypherString("a\nb"), "'a\\nb'");
  assert.equal(cypherLiteral(42), "42");
  assert.equal(cypherLiteral(true), "true");
});

test("vertex upsert uses UNWIND batch with single SET clause", () => {
  const query = vertexUpsert("PiMemory", ["session", "title"]);
  assert.equal(
    query,
    "UNWIND $rows AS row MERGE (n {id: row.id}) SET n:PiMemory, n.session = row.session, n.title = row.title",
  );
});

test("vertex select projects only properties, never the node", () => {
  const query = vertexSelect("PiMemory", ["id", "title"], { session: 7 }, 5);
  assert.equal(
    query,
    "MATCH (n:PiMemory {session: 7}) RETURN n.id AS id, n.title AS title LIMIT 5",
  );
  assert.equal(vertexCount("PiMemory", { session: 7 }), "MATCH (n:PiMemory {session: 7}) RETURN count(*) AS total");
});

test("edge select uses a single relationship type and one hop", () => {
  const query = edgeSelect("MENTIONS", "PiMemory", "PiFile", ["path"], 123);
  assert.equal(query, "MATCH (a:PiMemory {id: 123})-[:MENTIONS]->(b:PiFile) RETURN b.path AS path");
});

test("touchedPaths reads file tools and canonicalizes to absolute paths", () => {
  assert.deepEqual(touchedPaths("read", { path: "src/a.ts" }, "/repo"), ["/repo/src/a.ts"]);
  assert.deepEqual(touchedPaths("read", { path: "/repo/src/a.ts" }, "/repo"), ["/repo/src/a.ts"]);
  assert.deepEqual(touchedPaths("edit", { file_path: "src/b.ts" }, "/repo"), ["/repo/src/b.ts"]);
  assert.deepEqual(touchedPaths("bash", { command: "ls" }, "/repo"), []);
});

test("absolute and relative references to one file collapse to one node", () => {
  const absolute = canonicalPath("/repo/src/a.ts", "/repo");
  const relative = canonicalPath("src/a.ts", "/repo");
  assert.equal(absolute, relative);
  assert.equal(
    stableNodeId(nodeKey("session", "file", absolute)),
    stableNodeId(nodeKey("session", "file", relative)),
  );
});

test("codegraph ids are recognized by kind prefix", () => {
  assert.ok(isCodegraphId("file:pi-hydradb-clm/src/graph.ts"));
  assert.ok(isCodegraphId("function:6afd43a75d2f6c730cb2b29c8f5dcb5b"));
  assert.ok(isCodegraphId("method:0d564a833fc434af175a7ce3af3d5beb"));
  assert.ok(!isCodegraphId("src/graph.ts"));
  assert.ok(!isCodegraphId("GraphClient"));
});

test("extractReferences pulls backticked paths, symbols and codegraph ids", () => {
  const text = [
    "The Bolt client lives in `pi-hydradb-clm/src/graph.ts`.",
    "It is implemented by `GraphClient::connect`.",
    "Already resolved: function:6afd43a75d2f6c730cb2b29c8f5dcb5b",
    "Duplicate `pi-hydradb-clm/src/graph.ts` is collapsed.",
  ].join("\n");
  assert.deepEqual(extractReferences(text), [
    "pi-hydradb-clm/src/graph.ts",
    "GraphClient::connect",
    "function:6afd43a75d2f6c730cb2b29c8f5dcb5b",
  ]);
});

test("context file splits and recomposes around the managed zone", () => {
  const model = "# Live context\n\nBolt client is `pi-hydradb-clm/src/graph.ts`.";
  const refs = [
    {
      id: 1,
      codegraphId: "file:pi-hydradb-clm/src/graph.ts",
      kind: "file",
      name: "graph.ts",
      filePath: "pi-hydradb-clm/src/graph.ts",
    },
  ];
  const composed = composeContextFile(model, refs, "s1", 3);
  assert.ok(composed.startsWith(model));
  assert.ok(composed.includes(MANAGED_BEGIN) && composed.includes(MANAGED_END));
  assert.ok(composed.includes("`file:pi-hydradb-clm/src/graph.ts`"));

  const split = splitContextFile(composed);
  assert.equal(split.model, model);
  assert.ok(split.managed.startsWith(MANAGED_BEGIN));

  const regenerated = composeContextFile(split.model, [], "s1", 4);
  assert.ok(!regenerated.includes("file:pi-hydradb-clm/src/graph.ts"));
  assert.ok(regenerated.includes("_None yet."));
});

test("estimateTokens is characters over four", () => {
  assert.equal(estimateTokens("abcd"), 1);
  assert.equal(estimateTokens("a".repeat(401)), 101);
});

test("summarizeTurn keeps prose and tool names instead of a JSON slice", () => {
  const message = {
    role: "assistant",
    content: [
      { type: "thinking", thinking: "I should read the file first." },
      { type: "text", text: "Reading the Bolt client." },
      { type: "toolCall", name: "read", arguments: { path: "src/graph.ts" } },
      { type: "toolCall", name: "grep", arguments: { pattern: "connect" } },
    ],
  };
  const summary = summarizeTurn(message);
  assert.ok(summary.includes("Reading the Bolt client."));
  assert.ok(summary.includes("called: read, grep"));
  assert.ok(!summary.includes("{"), "summary must not be raw JSON");
  assert.ok(summary.length < 600);
});

test("summarizeTurn surfaces tool errors", () => {
  const summary = summarizeTurn(
    { role: "assistant", content: [{ type: "text", text: "retrying" }] },
    [
      { toolName: "hydradb_remember", isError: true, content: [{ type: "text", text: "connection reset" }] },
      { toolName: "read", isError: false, content: [{ type: "text", text: "ok" }] },
    ],
  );
  assert.ok(summary.includes("ERROR from hydradb_remember: connection reset"));
  assert.ok(!summary.includes("ERROR from read"));
});

test("summarizeTurn falls back when there is nothing to say", () => {
  assert.equal(summarizeTurn({ role: "assistant", content: [] }), "(no text)");
  assert.equal(summarizeTurn(null), "(no text)");
});

test("only plausible code references survive extraction", () => {
  assert.ok(isReferenceCandidate("pi-hydradb-clm/src/graph.ts"));
  assert.ok(isReferenceCandidate("GraphClient::connect"));
  assert.ok(isReferenceCandidate("file:cmd/hydradb-bins/main.go"));
  assert.ok(isReferenceCandidate("oras.land/oras-go/v2"));
  assert.ok(!isReferenceCandidate("codegraph sync"), "prose phrase");
  assert.ok(!isReferenceCandidate("--test-force-exit"), "command-line flag");
  assert.ok(!isReferenceCandidate("/home/user/src/repo"), "absolute path, not repo-relative");
  assert.ok(!isReferenceCandidate(".codegraph/codegraph.db"), "leading dot");
  assert.ok(!isReferenceCandidate(""), "empty");
});

test("extraction drops prose and flags from backticks", () => {
  const text = "Run `codegraph sync`, pass `--test-force-exit`, and read `pi-hydradb-clm/src/graph.ts`.";
  assert.deepEqual(extractReferences(text), ["pi-hydradb-clm/src/graph.ts"]);
});

test("arcadedb is the default graph backend", () => {
  assert.equal(parseBackend(undefined), "arcadedb");
  assert.equal(parseBackend(""), "arcadedb");
  assert.equal(parseBackend("HydraDB"), "hydradb");
  assert.throws(() => parseBackend("sqlite"));
});

test("the default target is arcadedb on 7688 with the local password", () => {
  const target = resolvePartialTarget({});
  assert.equal(target.backend, "arcadedb");
  assert.equal(target.boltUrl, "bolt://127.0.0.1:7688");
  assert.equal(target.user, "root");
  assert.equal(target.database, "clm");
  assert.equal(target.token, "clm-arcadedb-root");
});

test("GRAPH_* and the HYDRA_* aliases both select the target", () => {
  const preferred = resolvePartialTarget({
    GRAPH_BACKEND: "hydradb",
    GRAPH_BOLT_URL: "bolt://graph:7687",
    GRAPH_USER: "alice",
    GRAPH_TOKEN: "graph-token",
  });
  assert.deepEqual(preferred, {
    backend: "hydradb",
    boltUrl: "bolt://graph:7687",
    user: "alice",
    token: "graph-token",
    tokenFile: "/var/run/secrets/slatedb-graph/auth-token",
    database: undefined,
  });
  const alias = resolvePartialTarget({
    HYDRA_BACKEND: "hydradb",
    HYDRA_BOLT_URL: "bolt://alias:7687",
    HYDRA_USER: "bob",
    HYDRA_TOKEN: "alias-token",
  });
  assert.equal(alias.backend, "hydradb");
  assert.equal(alias.boltUrl, "bolt://alias:7687");
  assert.equal(alias.user, "bob");
  assert.equal(alias.token, "alias-token");
  const both = resolvePartialTarget({ HYDRA_USER: "alias", GRAPH_USER: "preferred" });
  assert.equal(both.user, "preferred");
});
