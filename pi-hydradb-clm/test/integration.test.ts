import assert from "node:assert/strict";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { MANAGED_BEGIN } from "../src/context-doc.ts";
import type { GraphClient } from "../src/graph.ts";
import { nodeKey, stableNodeId } from "../src/ids.ts";
import { CODE_REF_PROPS, EDGES, FILE_PROPS, LABELS, MEMORY_PROPS } from "../src/schema.ts";
import { linkSpecifies, specCodeRefId } from "../src/spec-graph.ts";
import { ensureGraph, makeScratch, makeSession, openGraph, uniqueSessionKey } from "./harness.ts";

const TIMEOUT = { timeout: 240_000 };
const EXPECTED_TOOLS = [
  "hydradb_remember",
  "hydradb_recall",
  "hydradb_neighbors",
  "hydradb_forget",
  "hydradb_context",
  "codegraph_resolve",
];

let graph: GraphClient;

before(async () => {
  await ensureGraph();
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

test("a remembered concept specifies the requirement its code answers to", TIMEOUT, async () => {
  const suffix = Math.floor(Math.random() * 1_000_000);
  const requirementId = stableNodeId(`requirement:calc/r.${suffix}`);
  const codegraphId = `function:synthetic${suffix}`;
  const codeRefId = specCodeRefId(codegraphId);
  const memoryId = stableNodeId(`pimemory:synthetic${suffix}`);
  await graph.upsertVertices(
    "SpecRequirement",
    [
      {
        id: requirementId,
        context: "calc",
        reqId: `r.${suffix}`,
        level: "MUST",
        text: "synthetic",
      },
    ],
    ["context", "reqId", "level", "text"],
  );
  await graph.upsertVertices(
    "CodeRef",
    [
      {
        id: codeRefId,
        codegraphId,
        kind: "function",
        name: `synthetic${suffix}`,
        filePath: "calc/calc.go",
      },
    ],
    ["codegraphId", "kind", "name", "filePath"],
  );
  await graph.upsertEdges("REFERENCES", "SpecRequirement", "CodeRef", [
    { src: requirementId, dst: codeRefId },
  ]);
  await graph.upsertVertices(
    LABELS.memory,
    [
      {
        id: memoryId,
        session: 0,
        kind: "decision",
        title: `synthetic ${suffix}`,
        body: "why the code matters",
        created: 0,
      },
    ],
    [...MEMORY_PROPS],
  );

  try {
    assert.equal(await linkSpecifies(graph, memoryId, [codegraphId]), 1);
    await linkSpecifies(graph, memoryId, [codegraphId]);
    const rows = await graph.selectNeighbors(
      EDGES.specifies,
      LABELS.memory,
      "SpecRequirement",
      ["id", "reqId"],
      memoryId,
    );
    assert.equal(rows.length, 1);
    assert.equal(rows[0]?.reqId, `r.${suffix}`);
    assert.equal(await linkSpecifies(graph, memoryId, [`function:absent${suffix}`]), 0);
  } finally {
    await graph.deleteVertices([memoryId, requirementId, codeRefId]);
  }
});

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
    assert.ok(
      payloads.includes("Context file:"),
      "the live context file was not injected into the request",
    );
    assert.ok(
      payloads.includes(MANAGED_BEGIN),
      "the injected document is missing its managed reference section",
    );
    assert.ok(
      payloads.includes("live-context.md"),
      "the injected document does not name the context file",
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

test("backticked references in the context file become CodeGraph ids in the graph", TIMEOUT, async () => {
  const sessionKey = uniqueSessionKey("coderef");
  process.env.HYDRA_CLM_SESSION = sessionKey;
  const sessionId = sessionIdFor(sessionKey);
  const scratch = makeScratch();
  const contextFile = join(scratch, "live-context.md");
  writeFileSync(
    contextFile,
    [
      "# Live context",
      "",
      "The Bolt client lives in `pi-hydradb-clm/src/graph.ts`.",
      "The retry logic is in `GraphClient::connect`.",
    ].join("\n"),
  );
  process.env.HYDRA_CLM_CONTEXT_PATH = contextFile;
  const harness = await makeSession(scratch);
  try {
    await harness.session.prompt("Reply with the single word: ok");
    const rows = await graph.selectVertices(
      LABELS.codeRef,
      ["id", ...CODE_REF_PROPS],
      { session: sessionId },
    );
    const ids = rows.map((row) => String(row.codegraph_id));
    assert.ok(
      ids.includes("file:pi-hydradb-clm/src/graph.ts"),
      `file reference not resolved to a CodeGraph id: ${JSON.stringify(ids)}`,
    );
    assert.ok(
      ids.some((id) => id.startsWith("method:") || id.startsWith("class:")),
      `symbol reference not resolved to a CodeGraph id: ${JSON.stringify(ids)}`,
    );

    const regenerated = readFileSync(contextFile, "utf8");
    assert.ok(
      regenerated.includes("file:pi-hydradb-clm/src/graph.ts"),
      "managed section did not record the resolved id",
    );
    assert.ok(
      regenerated.startsWith("# Live context"),
      "the model-owned part of the context file was moved or lost",
    );
  } finally {
    delete process.env.HYDRA_CLM_CONTEXT_PATH;
    harness.dispose();
  }
});

test("remembering a concept attaches it to code through CodeGraph ids", TIMEOUT, async () => {
  const sessionKey = uniqueSessionKey("attach");
  process.env.HYDRA_CLM_SESSION = sessionKey;
  const sessionId = sessionIdFor(sessionKey);
  const harness = await makeSession(makeScratch());
  try {
    await harness.session.prompt(
      "Call hydradb_remember exactly once with kind=\"decision\", title=\"bolt client\", " +
        'body="the graph client retries connectivity three times", ' +
        'code_refs=["pi-hydradb-clm/src/graph.ts"]. Then reply done.',
    );
    const memories = await graph.selectVertices(LABELS.memory, ["id"], { session: sessionId });
    assert.equal(memories.length, 1, "expected one concept node");
    const refs = await graph.selectNeighbors(
      "REFERENCES",
      LABELS.memory,
      LABELS.codeRef,
      ["codegraph_id"],
      Number(memories[0]!.id),
    );
    assert.ok(
      refs.some((row) => row.codegraph_id === "file:pi-hydradb-clm/src/graph.ts"),
      `concept is not attached to code: ${JSON.stringify(refs)}`,
    );
  } finally {
    harness.dispose();
  }
});
