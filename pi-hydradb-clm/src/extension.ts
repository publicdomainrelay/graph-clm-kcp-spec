import { readFileSync } from "node:fs";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { HydraGraph } from "./graph.ts";
import { nodeKey, stableNodeId } from "./ids.ts";
import {
  MEMORY_PROTOCOL,
  projectLiveContext,
  type FileRecord,
  type MemoryRecord,
  type TurnRecord,
} from "./context-doc.ts";
import {
  EDGES,
  FILE_PROPS,
  LABELS,
  MEMORY_PROPS,
  SESSION_PROPS,
  TURN_PROPS,
} from "./schema.ts";

export interface HydraClmOptions {
  boltUrl: string;
  user: string;
  token?: string;
  tokenFile: string;
  database?: string;
  budgetTokens: number;
  sessionKey: string;
  autoIndex: boolean;
  enabled: boolean;
}

export const DEFAULT_TOKEN_FILE = "/var/run/secrets/slatedb-graph/auth-token";

export function optionsFromEnv(env: NodeJS.ProcessEnv = process.env): HydraClmOptions {
  const enabled = env.HYDRA_CLM_ENABLED !== "0" && env.HYDRA_CLM_ENABLED !== "false";
  const budgetTokens = Number(env.HYDRA_CLM_BUDGET ?? 2000);
  return {
    boltUrl: env.HYDRA_BOLT_URL ?? "bolt://127.0.0.1:7687",
    user: env.HYDRA_USER ?? "neo4j",
    token: env.HYDRA_TOKEN,
    tokenFile: env.HYDRA_TOKEN_FILE ?? DEFAULT_TOKEN_FILE,
    database: env.HYDRA_DATABASE,
    budgetTokens: Number.isFinite(budgetTokens) && budgetTokens > 0 ? budgetTokens : 2000,
    sessionKey: env.HYDRA_CLM_SESSION ?? `pi-${process.pid}`,
    autoIndex: env.HYDRA_CLM_AUTO_INDEX !== "0",
    enabled,
  };
}

export function resolveToken(options: HydraClmOptions): string {
  if (options.token) return options.token.trim();
  return readFileSync(options.tokenFile, "utf8").trim();
}

const FILE_TOOLS = new Set(["read", "write", "edit"]);

export function touchedPaths(toolName: string, input: Record<string, unknown>): string[] {
  if (!FILE_TOOLS.has(toolName)) return [];
  const path = input.path ?? input.file_path ?? input.filePath;
  return typeof path === "string" && path.length > 0 ? [path] : [];
}

export function memoryNodeId(sessionKey: string, kind: string, title: string, body: string): number {
  return stableNodeId(nodeKey(sessionKey, "memory", `${kind}\u0001${title}\u0001${body}`));
}

function text(content: string) {
  return { content: [{ type: "text" as const, text: content }], details: {} };
}

export function hydraClmExtension(pi: ExtensionAPI, options: HydraClmOptions): void {
  let graph: HydraGraph | null = null;
  let sessionId = 0;
  let revision = 0;
  const filesTouched = new Map<string, number>();
  const recentTurns: TurnRecord[] = [];

  const sessionNodeId = () => stableNodeId(nodeKey(options.sessionKey, "session", options.sessionKey));
  const fileNodeId = (path: string) => stableNodeId(nodeKey(options.sessionKey, "file", path));
  const turnNodeId = (turnIndex: number) =>
    stableNodeId(nodeKey(options.sessionKey, "turn", String(turnIndex)));

  async function connect(): Promise<HydraGraph | null> {
    if (graph) return graph;
    try {
      graph = await HydraGraph.connect({
        boltUrl: options.boltUrl,
        user: options.user,
        token: resolveToken(options),
        database: options.database,
      });
      return graph;
    } catch {
      graph = null;
      return null;
    }
  }

  async function ensureSession(client: HydraGraph): Promise<number> {
    if (sessionId !== 0) return sessionId;
    sessionId = sessionNodeId();
    await client.upsertVertices(
      LABELS.session,
      [
        {
          id: sessionId,
          key: options.sessionKey,
          started: Date.now(),
          cwd: process.cwd(),
          revision,
        },
      ],
      [...SESSION_PROPS],
    );
    return sessionId;
  }

  async function loadMemories(client: HydraGraph): Promise<MemoryRecord[]> {
    const rows = await client.selectVertices(
      LABELS.memory,
      ["id", ...MEMORY_PROPS],
      { session: sessionId },
    );
    return rows
      .map((row) => ({
        id: Number(row.id ?? 0),
        kind: String(row.kind ?? "note"),
        title: String(row.title ?? ""),
        body: String(row.body ?? ""),
        created: Number(row.created ?? 0),
      }))
      .sort((a, b) => b.created - a.created);
  }

  async function loadFiles(client: HydraGraph): Promise<FileRecord[]> {
    const rows = await client.selectVertices(LABELS.file, ["id", ...FILE_PROPS], {
      session: sessionId,
    });
    return rows
      .map((row) => ({
        id: Number(row.id ?? 0),
        path: String(row.path ?? ""),
        touches: Number(row.touches ?? 0),
      }))
      .sort((a, b) => b.touches - a.touches);
  }

  async function renderContext(): Promise<string | null> {
    const client = graph ?? (await connect());
    if (!client) return null;
    await ensureSession(client);
    const [memories, files] = await Promise.all([loadMemories(client), loadFiles(client)]);
    return projectLiveContext({
      sessionKey: options.sessionKey,
      sessionId,
      revision,
      budgetTokens: options.budgetTokens,
      memories,
      files,
      turns: recentTurns.slice(-8).reverse(),
    });
  }

  pi.on("session_start", async () => {
    if (!options.enabled) return;
    const client = await connect();
    if (!client) return;
    await ensureSession(client);
  });

  pi.on("tool_result", async (event) => {
    if (!options.enabled || !options.autoIndex) return;
    const client = graph;
    if (!client) return;
    await ensureSession(client);
    for (const path of touchedPaths(event.toolName, event.input)) {
      const id = fileNodeId(path);
      const touches = (filesTouched.get(path) ?? 0) + 1;
      filesTouched.set(path, touches);
      await client.upsertVertices(
        LABELS.file,
        [{ id, session: sessionId, path, touches }],
        [...FILE_PROPS],
      );
      await client.upsertEdges(EDGES.touched, LABELS.session, LABELS.file, [
        { src: sessionId, dst: id },
      ]);
    }
  });

  pi.on("turn_end", async (event) => {
    if (!options.enabled) return;
    const client = graph;
    if (!client) return;
    await ensureSession(client);
    const summary = JSON.stringify(event.message).slice(0, 400);
    const id = turnNodeId(event.turnIndex);
    recentTurns.push({ id, turnIndex: event.turnIndex, summary });
    await client.upsertVertices(
      LABELS.turn,
      [{ id, session: sessionId, turnIndex: event.turnIndex, summary, created: Date.now() }],
      [...TURN_PROPS],
    );
    await client.upsertEdges(EDGES.occurred, LABELS.session, LABELS.turn, [
      { src: sessionId, dst: id },
    ]);
  });

  pi.on("context", async (event) => {
    if (!options.enabled) return;
    const document = await renderContext();
    if (!document) return;
    revision += 1;
    return {
      messages: [
        ...event.messages,
        { role: "system" as const, content: document, timestamp: Date.now() },
      ],
    };
  });

  pi.on("session_shutdown", async () => {
    if (!graph) return;
    await graph.close();
    graph = null;
  });

  pi.registerTool({
    name: "hydradb_remember",
    label: "HydraDB Remember",
    description:
      "Store a durable fact, decision, or finding in the HydraDB context graph so it survives compaction. " +
      "Optionally link it to file paths it concerns.",
    promptSnippet: "hydradb_remember: persist a fact into the graph-backed live context",
    parameters: Type.Object({
      kind: Type.String({ description: "Short category, e.g. decision, finding, todo" }),
      title: Type.String({ description: "One-line summary" }),
      body: Type.String({ description: "Full detail to persist verbatim" }),
      files: Type.Optional(
        Type.Array(Type.String(), { description: "File paths this fact concerns" }),
      ),
    }),
    async execute(_toolCallId, params) {
      const client = graph ?? (await connect());
      if (!client) return text("hydradb unavailable: cannot reach the graph node");
      await ensureSession(client);
      const id = memoryNodeId(options.sessionKey, params.kind, params.title, params.body);
      await client.upsertVertices(
        LABELS.memory,
        [
          {
            id,
            session: sessionId,
            kind: params.kind,
            title: params.title,
            body: params.body,
            created: Date.now(),
          },
        ],
        [...MEMORY_PROPS],
      );
      await client.upsertEdges(EDGES.remembered, LABELS.session, LABELS.memory, [
        { src: sessionId, dst: id },
      ]);
      for (const path of params.files ?? []) {
        const fileId = fileNodeId(path);
        await client.upsertVertices(
          LABELS.file,
          [{ id: fileId, session: sessionId, path, touches: filesTouched.get(path) ?? 0 }],
          [...FILE_PROPS],
        );
        await client.upsertEdges(EDGES.mentions ?? "MENTIONS", LABELS.memory, LABELS.file, [
          { src: id, dst: fileId },
        ]);
      }
      return text(`remembered ${params.kind} "${params.title}" as node ${id}`);
    },
  });

  pi.registerTool({
    name: "hydradb_recall",
    label: "HydraDB Recall",
    description: "Read back durable facts stored in the HydraDB context graph, newest first.",
    promptSnippet: "hydradb_recall: read durable facts from the graph-backed live context",
    parameters: Type.Object({
      query: Type.Optional(Type.String({ description: "Case-insensitive substring filter" })),
      limit: Type.Optional(Type.Number({ description: "Maximum nodes to return" })),
    }),
    async execute(_toolCallId, params) {
      const client = graph ?? (await connect());
      if (!client) return text("hydradb unavailable: cannot reach the graph node");
      await ensureSession(client);
      const limit = params.limit ?? 20;
      const needle = (params.query ?? "").toLowerCase();
      const memories = (await loadMemories(client)).filter(
        (memory) =>
          needle.length === 0 ||
          `${memory.kind} ${memory.title} ${memory.body}`.toLowerCase().includes(needle),
      );
      if (memories.length === 0) return text("no matching memories");
      const lines = memories
        .slice(0, limit)
        .map((memory) => `[${memory.kind}] ${memory.title} (id=${memory.id})\n${memory.body}`);
      return text(lines.join("\n\n"));
    },
  });

  pi.registerTool({
    name: "hydradb_neighbors",
    label: "HydraDB Neighbors",
    description: "Walk one edge from a memory node to the files it mentions.",
    promptSnippet: "hydradb_neighbors: expand one hop from a graph node",
    parameters: Type.Object({
      id: Type.Number({ description: "Node id returned by hydradb_remember or hydradb_recall" }),
    }),
    async execute(_toolCallId, params) {
      const client = graph ?? (await connect());
      if (!client) return text("hydradb unavailable: cannot reach the graph node");
      await ensureSession(client);
      const rows = await client.selectNeighbors(
        "MENTIONS",
        LABELS.memory,
        LABELS.file,
        ["path"],
        Math.trunc(params.id),
      );
      if (rows.length === 0) return text(`node ${params.id} has no mentioned files`);
      return text(rows.map((row) => String(row.path)).join("\n"));
    },
  });

  pi.registerTool({
    name: "hydradb_forget",
    label: "HydraDB Forget",
    description: "Delete a memory node and its edges from the HydraDB context graph.",
    promptSnippet: "hydradb_forget: delete a node from the graph-backed live context",
    parameters: Type.Object({
      id: Type.Number({ description: "Node id to delete" }),
    }),
    async execute(_toolCallId, params) {
      const client = graph ?? (await connect());
      if (!client) return text("hydradb unavailable: cannot reach the graph node");
      await ensureSession(client);
      await client.deleteVertices([Math.trunc(params.id)]);
      return text(`forgot node ${params.id}`);
    },
  });

  pi.registerTool({
    name: "hydradb_context",
    label: "HydraDB Live Context",
    description: "Render the current graph-backed live context document.",
    promptSnippet: "hydradb_context: render the live context document",
    parameters: Type.Object({}),
    async execute() {
      const document = await renderContext();
      return text(document ?? `${MEMORY_PROTOCOL}\n\n(hydradb unavailable)`);
    },
  });

  pi.registerCommand("hydradb", {
    description: "Show HydraDB live-context status",
    handler: async (_args, ctx) => {
      const client = graph;
      if (!client) {
        ctx.ui.notify("HydraDB: not connected", "warning");
        return;
      }
      await ensureSession(client);
      const memories = await loadMemories(client);
      const files = await loadFiles(client);
      ctx.ui.notify(
        `HydraDB session ${sessionId}: ${memories.length} memories, ${files.length} files, revision ${revision}`,
        "info",
      );
    },
  });
}
