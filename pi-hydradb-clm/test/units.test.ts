import assert from "node:assert/strict";
import { test } from "node:test";
import {
  edgeSelect,
  vertexCount,
  vertexSelect,
  vertexUpsert,
} from "../src/cypher.ts";
import { cypherLiteral, cypherString, stableNodeId } from "../src/ids.ts";
import {
  buildLiveContextDocument,
  estimateTokens,
  projectLiveContext,
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

test("touchedPaths reads file tools only", () => {
  assert.deepEqual(touchedPaths("read", { path: "src/a.ts" }), ["src/a.ts"]);
  assert.deepEqual(touchedPaths("edit", { file_path: "src/b.ts" }), ["src/b.ts"]);
  assert.deepEqual(touchedPaths("bash", { command: "ls" }), []);
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
