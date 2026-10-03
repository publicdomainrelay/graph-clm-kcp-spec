import { readFileSync } from "node:fs";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { type GraphBackend } from "./backend.ts";
import { CodegraphResolver, type CodegraphNode } from "./codegraph.ts";
import { contextFilePath, ensureContextFile, readContextFile, writeContextFile } from "./context-file.ts";
import {
  MEMORY_PROTOCOL,
  estimateTokens,
  extractReferences,
  splitContextFile,
  summarizeTurn,
  type CodeRefRecord,
} from "./context-doc.ts";
import { GraphClient } from "./graph.ts";
import { canonicalPath, nodeKey, stableNodeId } from "./ids.ts";
import {
  CODE_REF_PROPS,
  EDGES,
  FILE_PROPS,
  LABELS,
  MEMORY_PROPS,
  SESSION_PROPS,
  TURN_PROPS,
} from "./schema.ts";
import { resolvePartialTarget } from "./target.ts";

export interface HydraClmOptions {
  backend: GraphBackend;
  boltUrl: string;
  user: string;
  token?: string;
  tokenFile?: string;
  database?: string;
  budgetTokens: number;
  sessionKey: string;
  contextPath?: string;
  autoIndex: boolean;
  enabled: boolean;
}

export function optionsFromEnv(env: NodeJS.ProcessEnv = process.env): HydraClmOptions {
  const enabled = env.HYDRA_CLM_ENABLED !== "0" && env.HYDRA_CLM_ENABLED !== "false";
  const budgetTokens = Number(env.HYDRA_CLM_BUDGET ?? 2000);
  const target = resolvePartialTarget(env);
  return {
    backend: target.backend,
    boltUrl: target.boltUrl,
    user: target.user,
    token: target.token,
    tokenFile: target.tokenFile,
    database: target.database,
    budgetTokens: Number.isFinite(budgetTokens) && budgetTokens > 0 ? budgetTokens : 2000,
    sessionKey: env.HYDRA_CLM_SESSION ?? `pi-${process.pid}`,
    contextPath: env.HYDRA_CLM_CONTEXT_PATH,
    autoIndex: env.HYDRA_CLM_AUTO_INDEX !== "0",
    enabled,
  };
}

export function resolveToken(options: HydraClmOptions): string {
  if (options.token) return options.token.trim();
  if (options.tokenFile) return readFileSync(options.tokenFile, "utf8").trim();
  throw new Error(`${options.backend} backend needs HYDRA_TOKEN or HYDRA_TOKEN_FILE`);
}

const FILE_TOOLS = new Set(["read", "write", "edit"]);

export function touchedPaths(
  toolName: string,
  input: Record<string, unknown>,
  cwd: string = process.cwd(),
): string[] {
  if (!FILE_TOOLS.has(toolName)) return [];
  const path = input.path ?? input.file_path ?? input.filePath;
  if (typeof path !== "string" || path.length === 0) return [];
  return [canonicalPath(path, cwd)];
}

export function memoryNodeId(sessionKey: string, kind: string, title: string, body: string): number {
  return stableNodeId(nodeKey(sessionKey, "memory", `${kind}\u0001${title}\u0001${body}`));
}

function text(content: string) {
  return { content: [{ type: "text" as const, text: content }], details: {} };
}

export function hydraClmExtension(pi: ExtensionAPI, options: HydraClmOptions): void {
  let graph: GraphClient | null = null;
  let sessionId = 0;
  let revision = 0;
  const filesTouched = new Map<string, number>();
  const codeRefs = new Map<string, CodeRefRecord>();
  const unresolvedRefs = new Set<string>();
  const resolver = CodegraphResolver.open(process.cwd());
  const filePath = options.contextPath ?? contextFilePath(options.sessionKey);

  const sessionNodeId = () =>
    stableNodeId(nodeKey(options.sessionKey, "session", options.sessionKey));
  const fileNodeId = (path: string) => stableNodeId(nodeKey(options.sessionKey, "file", path));
  const turnNodeId = (turnIndex: number) =>
    stableNodeId(nodeKey(options.sessionKey, "turn", String(turnIndex)));
  const codeRefNodeId = (codegraphId: string) =>
    stableNodeId(nodeKey(options.sessionKey, "coderef", codegraphId));

  async function connect(): Promise<GraphClient | null> {
    if (graph) return graph;
    try {
      graph = await GraphClient.connect({
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

  async function ensureSession(client: GraphClient): Promise<number> {
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

  const linkedEdges = new Set<string>();

  async function linkOnce(client: GraphClient, src: number, codegraphId: string): Promise<boolean> {
    const key = `${src}\u0000${codegraphId}`;
    if (linkedEdges.has(key)) return false;
    linkedEdges.add(key);
    await client.upsertEdges(EDGES.references, LABELS.memory, LABELS.codeRef, [
      { src, dst: codeRefNodeId(codegraphId) },
    ]);
    return true;
  }

  async function storeCodeRef(client: GraphClient, node: CodegraphNode): Promise<string> {
    const id = codeRefNodeId(node.id);
    const record: CodeRefRecord = {
      id,
      codegraphId: node.id,
      kind: node.kind,
      name: node.qualifiedName || node.name,
      filePath: node.filePath,
    };
    codeRefs.set(node.id, record);
    await client.upsertVertices(
      LABELS.codeRef,
      [
        {
          id,
          session: sessionId,
          codegraph_id: node.id,
          kind: node.kind,
          name: record.name,
          file_path: node.filePath,
        },
      ],
      [...CODE_REF_PROPS],
    );
    return node.id;
  }

  async function syncReferencesFromFile(client: GraphClient, model: string): Promise<void> {
    const references = extractReferences(model);
    for (const reference of references) {
      const nodes = await resolver.resolve(reference);
      if (nodes.length === 0) {
        if (!resolver.ambiguous.has(reference)) unresolvedRefs.add(reference);
        continue;
      }
      unresolvedRefs.delete(reference);
      for (const node of nodes) await storeCodeRef(client, node);
    }
  }

  const contextBudget = Math.max(400, Math.floor(options.budgetTokens * 0.6));

  function refreshContextFile(model: string): void {
    writeContextFile(
      filePath,
      model,
      [...codeRefs.values()],
      options.sessionKey,
      revision,
      contextBudget,
      [...unresolvedRefs].sort(),
    );
  }

  pi.on("session_start", async () => {
    if (!options.enabled) return;
    ensureContextFile(filePath, options.sessionKey, revision, contextBudget);
    const client = await connect();
    if (client) await ensureSession(client);
    pi.appendEntry?.("pi-hydradb-clm", { contextPath: filePath });
  });

  pi.on("tool_result", async (event) => {
    if (!options.enabled || !options.autoIndex) return;
    const client = graph;
    if (!client) return;
    await ensureSession(client);
    for (const path of touchedPaths(event.toolName, event.input)) {
      if (canonicalPath(path, process.cwd()) === canonicalPath(filePath, process.cwd())) continue;
      const touches = (filesTouched.get(path) ?? 0) + 1;
      filesTouched.set(path, touches);
      const id = fileNodeId(path);
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
    revision += 1;
    const summary = summarizeTurn(event.message, event.toolResults);
    const id = turnNodeId(event.turnIndex);
    await client.upsertVertices(
      LABELS.turn,
      [{ id, session: sessionId, turnIndex: event.turnIndex, summary, created: Date.now() }],
      [...TURN_PROPS],
    );
    await client.upsertEdges(EDGES.occurred, LABELS.session, LABELS.turn, [
      { src: sessionId, dst: id },
    ]);
    refreshContextFile(readContextFile(filePath).model);
  });

  pi.on("context", async (event) => {
    if (!options.enabled) return;
    const client = await connect();
    if (client) {
      await ensureSession(client);
      const { model } = readContextFile(filePath);
      await syncReferencesFromFile(client, model);
    }
    ensureContextFile(filePath, options.sessionKey, revision, contextBudget);
    refreshContextFile(readContextFile(filePath).model);
    const document = readFileSync(filePath, "utf8");
    const split = splitContextFile(document);
    const header = [
      MEMORY_PROTOCOL,
      "",
      `Context file: ${filePath}`,
      `Size: ${estimateTokens(document)} tokens ` +
        `(your notes ${estimateTokens(split.model)}, index ${estimateTokens(split.managed)})`,
      "",
      document,
    ].join("\n");
    return {
      messages: [
        ...event.messages,
        { role: "system" as const, content: header, timestamp: Date.now() },
      ],
    };
  });

  pi.on("session_shutdown", async () => {
    resolver.close();
    if (!graph) return;
    await graph.close();
    graph = null;
  });

  pi.registerTool({
    name: "hydradb_remember",
    label: "HydraDB Remember",
    description:
      "Store a durable concept in the context graph so it survives compaction, and attach the code it " +
      "concerns. Code references are resolved through CodeGraph and stored as CodeGraph ids, so the " +
      "concept records why the code matters alongside what it is.",
    promptSnippet: "hydradb_remember: persist a concept and the code it concerns",
    parameters: Type.Object({
      kind: Type.String({ description: "Short category, e.g. decision, finding, todo" }),
      title: Type.String({ description: "One-line summary" }),
      body: Type.String({ description: "Full detail to persist verbatim" }),
      code_refs: Type.Optional(
        Type.Array(Type.String(), {
          description:
            "Code this concept concerns, as repository-relative paths or CodeGraph ids",
        }),
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

      let linked = 0;
      let unresolved: string[] = [];
      try {
        await client.upsertEdges(EDGES.remembered, LABELS.session, LABELS.memory, [
          { src: sessionId, dst: id },
        ]);
        for (const reference of params.code_refs ?? []) {
          const nodes = await resolver.resolve(reference);
          if (nodes.length === 0) {
            if (!resolver.ambiguous.has(reference)) unresolvedRefs.add(reference);
            unresolved.push(reference);
            continue;
          }
          unresolvedRefs.delete(reference);
          for (const node of nodes) {
            await storeCodeRef(client, node);
            if (await linkOnce(client, id, node.id)) linked += 1;
          }
        }
      } catch (error) {
        return text(
          `remembered ${params.kind} "${params.title}" as node ${id}, but linking stopped ` +
            `after ${linked} code reference(s): ${error instanceof Error ? error.message : String(error)}. ` +
            "The concept is stored and will appear in the live context; do not re-store it.",
        );
      }
      refreshContextFile(readContextFile(filePath).model);
      const ambiguous = (params.code_refs ?? []).filter((ref) => resolver.ambiguous.has(ref));
      const notes: string[] = [];
      if (unresolved.length > 0) notes.push(`unresolved: ${unresolved.join(", ")}`);
      if (ambiguous.length > 0) {
        notes.push(
          `ambiguous, quote a path instead: ${ambiguous
            .map((ref) => {
              const files = [...new Set((resolver.ambiguous.get(ref) ?? []).map((n) => n.filePath))];
              return `${ref} (${files.slice(0, 3).join(", ")})`;
            })
            .join("; ")}`,
        );
      }
      const unresolvedNote = notes.length > 0 ? `; ${notes.join("; ")}` : "";
      return text(
        `remembered ${params.kind} "${params.title}" as node ${id}, ` +
          `linked to ${linked} code reference(s)${unresolvedNote}`,
      );
    },
  });

  pi.registerTool({
    name: "hydradb_recall",
    label: "HydraDB Recall",
    description: "Read back durable concepts stored in the context graph, newest first.",
    promptSnippet: "hydradb_recall: read durable concepts from the context graph",
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
      const rows = await client.selectVertices(
        LABELS.memory,
        ["id", ...MEMORY_PROPS],
        { session: sessionId },
      );
      const memories = rows
        .map((row) => ({
          id: Number(row.id ?? 0),
          kind: String(row.kind ?? "note"),
          title: String(row.title ?? ""),
          body: String(row.body ?? ""),
          created: Number(row.created ?? 0),
        }))
        .filter(
          (memory) =>
            needle.length === 0 ||
            `${memory.kind} ${memory.title} ${memory.body}`.toLowerCase().includes(needle),
        )
        .sort((a, b) => b.created - a.created);
      if (memories.length === 0) return text("no matching concepts");
      const lines = memories
        .slice(0, limit)
        .map((memory) => `[${memory.kind}] ${memory.title} (id=${memory.id})\n${memory.body}`);
      return text(lines.join("\n\n"));
    },
  });

  pi.registerTool({
    name: "hydradb_neighbors",
    label: "HydraDB Neighbors",
    description: "Walk from a concept node to the code references attached to it.",
    promptSnippet: "hydradb_neighbors: expand one hop from a concept to its code",
    parameters: Type.Object({
      id: Type.Number({ description: "Concept node id from hydradb_remember or hydradb_recall" }),
    }),
    async execute(_toolCallId, params) {
      const client = graph ?? (await connect());
      if (!client) return text("hydradb unavailable: cannot reach the graph node");
      await ensureSession(client);
      const rows = await client.selectNeighbors(
        EDGES.references,
        LABELS.memory,
        LABELS.codeRef,
        ["codegraph_id", "kind", "file_path"],
        Math.trunc(params.id),
      );
      if (rows.length === 0) return text(`node ${params.id} references no code`);
      return text(
        rows
          .map((row) => `${row.codegraph_id} (${row.kind}) ${row.file_path}`)
          .join("\n"),
      );
    },
  });

  pi.registerTool({
    name: "hydradb_forget",
    label: "HydraDB Forget",
    description: "Delete a concept node and its edges from the context graph.",
    promptSnippet: "hydradb_forget: delete a node from the context graph",
    parameters: Type.Object({
      id: Type.Number({ description: "Node id to delete" }),
    }),
    async execute(_toolCallId, params) {
      const client = graph ?? (await connect());
      if (!client) return text("hydradb unavailable: cannot reach the graph node");
      await ensureSession(client);
      await client.deleteVertices([Math.trunc(params.id)]);
      refreshContextFile(readContextFile(filePath).model);
      return text(`forgot node ${params.id}`);
    },
  });

  pi.registerTool({
    name: "hydradb_context",
    label: "HydraDB Live Context",
    description: "Print the live context file path and its current contents.",
    promptSnippet: "hydradb_context: show the live context file",
    parameters: Type.Object({}),
    async execute() {
      let contents = "";
      try {
        contents = readFileSync(filePath, "utf8");
      } catch {
        contents = "(missing)";
      }
      return text(`${filePath}\n\n${contents}`);
    },
  });

  pi.registerTool({
    name: "codegraph_resolve",
    label: "CodeGraph Resolve",
    description:
      "Resolve a repository-relative path, symbol name, or CodeGraph id to the CodeGraph ids " +
      "that index this repository.",
    promptSnippet: "codegraph_resolve: turn a path or symbol into CodeGraph ids",
    parameters: Type.Object({
      reference: Type.String({ description: "Path, symbol name, or CodeGraph id" }),
    }),
    async execute(_toolCallId, params) {
      if (!resolver.available) {
        return text(
          "no CodeGraph index found; run `codegraph init` in this repository to create .codegraph/",
        );
      }
      const nodes = await resolver.resolve(params.reference);
      if (nodes.length === 0) return text(`no CodeGraph node matches ${params.reference}`);
      return text(
        nodes
          .map(
            (node) =>
              `${node.id}  ${node.kind} ${node.qualifiedName}  ${node.filePath}`,
          )
          .join("\n"),
      );
    },
  });

  pi.registerCommand("hydradb", {
    description: "Show live context status",
    handler: async (_args, ctx) => {
      const client = graph;
      if (!client) {
        ctx.ui.notify("HydraDB: not connected", "warning");
        return;
      }
      await ensureSession(client);
      const memories = await client.countVertices(LABELS.memory, { session: sessionId });
      ctx.ui.notify(
        `HydraDB session ${sessionId}: ${memories} concepts, ${codeRefs.size} code refs, ` +
          `revision ${revision}. Context file: ${filePath}`,
        "info",
      );
    },
  });
}
