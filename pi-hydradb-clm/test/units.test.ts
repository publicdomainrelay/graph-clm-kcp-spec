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
  buildLiveContextDocument,
  composeContextFile,
  estimateTokens,
  extractReferences,
  isCodegraphId,
  projectLiveContext,
  splitContextFile,
  type MemoryRecord,
} from "../src/context-doc.ts";
import { touchedPaths } from "../src/extension.ts";

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

test("live context document has the CLM header and sections", () => {
  const document = buildLiveContextDocument({
    sessionKey: "s1",
    sessionId: 11,
    revision: 2,
    budgetTokens: 500,
    memories: [{ id: 1, kind: "decision", title: "use bolt", body: "port 7687", created: 0 }],
    files: [{ id: 2, path: "src/a.ts", touches: 3 }],
    turns: [{ id: 3, turnIndex: 0, summary: "started" }],
  });
  assert.match(document, /^\[\[HYDRA_CLM version=1 session=s1 id=11 revision=2 budget=500\]\]/);
  assert.match(document, /## Remembered/);
  assert.match(document, /\[decision\] use bolt \(id=1\)/);
  assert.match(document, /## Touched files/);
  assert.match(document, /## Recent turns/);
});

test("projection drops entries past the token budget", () => {
  const memories: MemoryRecord[] = Array.from({ length: 50 }, (_, index) => ({
    id: index,
    kind: "finding",
    title: `finding ${index}`,
    body: "x".repeat(400),
    created: index,
  }));
  const projected = projectLiveContext({
    sessionKey: "s1",
    sessionId: 1,
    revision: 1,
    budgetTokens: 300,
    memories,
    files: [],
    turns: [],
  });
  assert.ok(estimateTokens(projected) < 2000);
  assert.ok(projected.includes("hydradb_remember"));
  assert.match(projected, /## Remembered/);
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
