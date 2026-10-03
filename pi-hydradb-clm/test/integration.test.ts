import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { after, before, test } from "node:test";
import type { HydraGraph } from "../src/graph.ts";
import { nodeKey, stableNodeId } from "../src/ids.ts";
import { FILE_PROPS, LABELS, MEMORY_PROPS } from "../src/schema.ts";
import { ensureHydra, makeScratch, makeSession, openGraph, uniqueSessionKey } from "./harness.ts";

const TIMEOUT = { timeout: 240_000 };
const EXPECTED_TOOLS = [
  "hydradb_remember",
  "hydradb_recall",
  "hydradb_neighbors",
  "hydradb_forget",
  "hydradb_context",
];

let graph: HydraGraph;

before(async () => {
  await ensureHydra();
  graph = await openGraph();
});

after(async () => {
  await graph?.close();
});

const sessionIdFor = (key: string) => stableNodeId(nodeKey(key, "session", key));

async function memoriesOf(sessionId: number) {
  return await graph.selectVertices(LABELS.memory, ["id", ...MEMORY_PROPS], {
    session: sessionId,
  });
}

async function filesOf(sessionId: number) {
  return await graph.selectVertices(LABELS.file, ["id", ...FILE_PROPS], { session: sessionId });
}

test("hydradb graph round-trips a vertex and an edge", TIMEOUT, async () => {
  const sessionId = Math.floor(Math.random() * 1_000_000);
  const a = Math.floor(Math.random() * 1_000_000);
  const b = a + 1;
  await graph.upsertVertices(
    "ClmProbe",
    [
      { id: a, session: sessionId, name: "alpha" },
      { id: b, session: sessionId, name: "beta" },
    ],
    ["session", "name"],
  );
  await graph.upsertEdges("CLM_LINK", "ClmProbe", "ClmProbe", [{ src: a, dst: b }]);
  const rows = await graph.selectVertices("ClmProbe", ["id", "name"], { session: sessionId });
  assert.equal(rows.length, 2);
  const neighbors = await graph.selectNeighbors("CLM_LINK", "ClmProbe", "ClmProbe", ["name"], a);
  assert.deepEqual(neighbors.map((row) => row.name), ["beta"]);
  await graph.deleteVertices([a, b]);
  assert.equal(await graph.countVertices("ClmProbe", { session: sessionId }), 0);
});

test("extension registers the hydradb tools and command", TIMEOUT, async () => {
  process.env.HYDRA_CLM_SESSION = uniqueSessionKey("registry");
  const harness = await makeSession(makeScratch());
  try {
    const tools = harness.session.getActiveToolNames();
    for (const name of EXPECTED_TOOLS) assert.ok(tools.includes(name), `missing ${name}`);
  } finally {
    harness.dispose();
  }
});

test("model stores a fact that lands in the hydradb graph", TIMEOUT, async () => {
  const sessionKey = uniqueSessionKey("remember");
  process.env.HYDRA_CLM_SESSION = sessionKey;
  const sessionId = sessionIdFor(sessionKey);
  const scratch = makeScratch();
  const harness = await makeSession(scratch);
  try {
    await harness.session.prompt(
      "Call the hydradb_remember tool exactly once with " +
        'kind="decision", title="deploy target", body="production runs on port 8080". ' +
        "Then reply with only the node id.",
    );
    const stored = await memoriesOf(sessionId);
    assert.ok(stored.length >= 1, "no memory node was written to hydradb");
    const titles = stored.map((row) => row.title);
    assert.ok(titles.includes("deploy target"), `unexpected titles: ${JSON.stringify(titles)}`);
  } finally {
    harness.dispose();
  }
});

test("a later session recalls the fact from the graph", TIMEOUT, async () => {
  const sessionKey = uniqueSessionKey("recall");
  process.env.HYDRA_CLM_SESSION = sessionKey;
  const sessionId = sessionIdFor(sessionKey);
  const first = await makeSession(makeScratch());
  try {
    await first.session.prompt(
      "Call the hydradb_remember tool exactly once with " +
        'kind="finding", title="cache key", body="the cache key is tundra-77". ' +
        "Then reply done.",
    );
  } finally {
    first.dispose();
  }
  assert.ok((await memoriesOf(sessionId)).length >= 1, "seed memory missing");

  const second = await makeSession(makeScratch());
  try {
    await second.session.prompt(
      "Use the hydradb_recall tool with query \"cache key\" and report the body verbatim.",
    );
    const answer = second.session.getLastAssistantText() ?? "";
    assert.ok(
      answer.includes("tundra-77"),
      `recall did not surface the stored fact: ${answer.slice(0, 300)}`,
    );
  } finally {
    second.dispose();
  }
});

test("reading a file auto-indexes it into the graph", TIMEOUT, async () => {
  const sessionKey = uniqueSessionKey("index");
  process.env.HYDRA_CLM_SESSION = sessionKey;
  const sessionId = sessionIdFor(sessionKey);
  const scratch = makeScratch();
  writeFileSync(join(scratch, "notes.txt"), "the relay is the registry\n");
  const harness = await makeSession(scratch);
  try {
    await harness.session.prompt(
      "Use the read tool to read the file notes.txt in the working directory. Then say done.",
    );
    const files = await filesOf(sessionId);
    assert.ok(
      files.some((row) => String(row.path).endsWith("notes.txt")),
      `notes.txt was not indexed: ${JSON.stringify(files)}`,
    );
  } finally {
    harness.dispose();
  }
});

test("the live context document is injected into the provider request", TIMEOUT, async () => {
  const sessionKey = uniqueSessionKey("context");
  process.env.HYDRA_CLM_SESSION = sessionKey;
  const harness = await makeSession(makeScratch());
  try {
    await harness.session.prompt(
      "Call hydradb_remember once with kind=\"note\", title=\"context probe\", " +
        'body="the context probe body". Then reply done.',
    );
    await harness.session.prompt("Reply with the single word: ok");
    const payloads = harness.captured.join("\n");
    const header = `[[HYDRA_CLM version=1 session=${sessionKey}`;
    assert.ok(
      payloads.includes(header),
      "live context document was not injected into the request",
    );
    assert.ok(payloads.includes("context probe"), "remembered fact missing from injection");
    assert.ok(
      payloads.includes("## Touched files"),
      "live context document is missing its sections",
    );
  } finally {
    harness.dispose();
  }
});

test("forgetting removes the node from the graph", TIMEOUT, async () => {
  const sessionKey = uniqueSessionKey("forget");
  process.env.HYDRA_CLM_SESSION = sessionKey;
  const sessionId = sessionIdFor(sessionKey);
  const harness = await makeSession(makeScratch());
  try {
    await harness.session.prompt(
      "Call hydradb_remember once with kind=\"todo\", title=\"throwaway\", " +
        'body="delete me". Reply with only the node id number.',
    );
    const stored = await memoriesOf(sessionId);
    assert.equal(stored.length, 1, "expected exactly one memory node");
    const nodeId = Number(stored[0]!.id);

    await harness.session.prompt(
      `Call the hydradb_forget tool with id ${nodeId}. Then reply done.`,
    );
    assert.equal(await graph.countVertices(LABELS.memory, { session: sessionId }), 0);
  } finally {
    harness.dispose();
  }
});
