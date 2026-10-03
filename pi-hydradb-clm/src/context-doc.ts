export const LIVE_CONTEXT_VERSION = 1;

export interface MemoryRecord {
  id: number;
  kind: string;
  title: string;
  body: string;
  created: number;
}

export interface FileRecord {
  id: number;
  path: string;
  touches: number;
}

export interface TurnRecord {
  id: number;
  turnIndex: number;
  summary: string;
}

export interface LiveContextInput {
  sessionKey: string;
  sessionId: number;
  revision: number;
  budgetTokens: number;
  memories: MemoryRecord[];
  files: FileRecord[];
  turns: TurnRecord[];
}

export const MEMORY_PROTOCOL = [
  "# HydraDB live context",
  "",
  "Your conversation context is mirrored into a HydraDB graph. The graph is durable:",
  "it outlives this context window and is shared across turns of this session.",
  "",
  "- `hydradb_remember` stores a durable fact, decision, or finding as a graph node.",
  "- `hydradb_recall` reads back stored nodes, newest first, filtered by substring.",
  "- `hydradb_neighbors` walks one edge from a node you already have an id for.",
  "- `hydradb_forget` removes a node and its edges.",
  "- `hydradb_context` re-renders the block below on demand.",
  "",
  "Write to the graph when a fact must survive compaction. Read from it before",
  "re-deriving something you may already know.",
].join("\n");

export function estimateTokens(text: string): number {
  return Math.ceil(text.length / 4);
}

export function selectWithinBudget<T>(
  items: T[],
  budgetTokens: number,
  render: (item: T) => string,
): T[] {
  const selected: T[] = [];
  let spent = 0;
  for (const item of items) {
    const cost = estimateTokens(render(item));
    if (spent + cost > budgetTokens) break;
    selected.push(item);
    spent += cost;
  }
  return selected;
}

function renderMemory(memory: MemoryRecord): string {
  return `- [${memory.kind}] ${memory.title} (id=${memory.id})\n  ${memory.body}`;
}

function renderFile(file: FileRecord): string {
  return `- ${file.path} (id=${file.id}, touches=${file.touches})`;
}

function renderTurn(turn: TurnRecord): string {
  return `- turn ${turn.turnIndex}: ${turn.summary} (id=${turn.id})`;
}

export function buildLiveContextDocument(input: LiveContextInput): string {
  const lines: string[] = [];
  lines.push(
    `[[HYDRA_CLM version=${LIVE_CONTEXT_VERSION} session=${input.sessionKey} ` +
      `id=${input.sessionId} revision=${input.revision} budget=${input.budgetTokens}]]`,
  );

  lines.push("", "## Remembered");
  if (input.memories.length === 0) lines.push("- (none)");
  else for (const memory of input.memories) lines.push(renderMemory(memory));

  lines.push("", "## Touched files");
  if (input.files.length === 0) lines.push("- (none)");
  else for (const file of input.files) lines.push(renderFile(file));

  lines.push("", "## Recent turns");
  if (input.turns.length === 0) lines.push("- (none)");
  else for (const turn of input.turns) lines.push(renderTurn(turn));

  return lines.join("\n");
}

export function projectLiveContext(input: LiveContextInput): string {
  const header = estimateTokens(
    `[[HYDRA_CLM version=${LIVE_CONTEXT_VERSION} session=${input.sessionKey}]]`,
  );
  let remaining = Math.max(0, input.budgetTokens - header);

  const memories = selectWithinBudget(input.memories, Math.floor(remaining * 0.6), renderMemory);
  remaining -= memories.reduce((sum, item) => sum + estimateTokens(renderMemory(item)), 0);

  const files = selectWithinBudget(input.files, Math.floor(remaining * 0.5), renderFile);
  remaining -= files.reduce((sum, item) => sum + estimateTokens(renderFile(item)), 0);

  const turns = selectWithinBudget(input.turns, remaining, renderTurn);

  const document = buildLiveContextDocument({ ...input, memories, files, turns });
  return `${MEMORY_PROTOCOL}\n\n${document}`;
}
