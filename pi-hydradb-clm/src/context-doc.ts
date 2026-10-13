export {
  docPathFromEnv,
  DEFAULT_MANAGED_BUDGET,
  EMPTY_INTENT,
  MANAGED_BEGIN,
  MANAGED_END,
  NOTICE,
  estimateTokens,
  extractReferences,
  isCodegraphId,
  isReferenceCandidate,
  selectWithinBudget,
  splitContextDoc as splitContextFile,
  type ResolvedRef,
} from "../../clm/core/mod.ts";

export {
  CONTEXT_SKILL as MEMORY_PROTOCOL,
  CONTEXT_SKILL_VERSION,
  contextSkill,
  REPOSITORY_SKILL_PATH,
  repositorySkill,
} from "../../clm/core/skill.ts";

import {
  MANAGED_BEGIN,
  MANAGED_END,
  selectWithinBudget,
  type CodeRefRecord as CoreCodeRefRecord,
} from "../../clm/core/mod.ts";

export const CONTEXT_FILE_NAME = "live-context.md";

export interface CodeRefRecord extends CoreCodeRefRecord {
  id: number;
}

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

// The protocol the pi host injects is the shared context-management skill of
// clm/core, re-exported as MEMORY_PROTOCOL so the host and the mod inject one
// versioned text. A repository override replaces it; see contextSkill.

export function renderManagedZone(
  references: CodeRefRecord[],
  sessionKey: string,
  revision: number,
  budgetTokens = 1500,
  unresolved: string[] = [],
): string {
  const lines = [
    MANAGED_BEGIN,
    `## Indexed code references (session ${sessionKey}, revision ${revision})`,
  ];
  if (references.length === 0) {
    lines.push("", "_None yet. Write backticked file paths or symbol names in your notes_");
    lines.push("_and they will be resolved to CodeGraph ids here._");
  } else {
    lines.push("");
    const shown = selectWithinBudget(
      references,
      budgetTokens,
      (ref) => `- \`${ref.codegraphId}\` (${ref.kind} ${ref.name}) in ${ref.filePath}`,
    );
    for (const ref of shown) {
      lines.push(`- \`${ref.codegraphId}\` ${ref.kind} ${ref.name} (${ref.filePath})`);
    }
    const hidden = references.length - shown.length;
    if (hidden > 0) {
      lines.push(
        "",
        `_${hidden} more reference(s) indexed but not listed here to stay inside the_`,
        `_${budgetTokens}-token budget. Use \`hydradb_neighbors\` on a concept node for its full set._`,
      );
    }
  }
  if (unresolved.length > 0) {
    lines.push("", `### Not in the CodeGraph index (${unresolved.length})`);
    for (const reference of unresolved.slice(0, 10)) lines.push(`- ${reference}`);
    if (unresolved.length > 10) {
      lines.push(`- ... and ${unresolved.length - 10} more`);
    }
    lines.push("", "_Run `codegraph sync` to index them, or quote a different path._");
  }
  lines.push(MANAGED_END);
  return lines.join("\n");
}

export function composeContextFile(
  model: string,
  references: CodeRefRecord[],
  sessionKey: string,
  revision: number,
  budgetTokens = 1500,
  unresolved: string[] = [],
): string {
  const head =
    model.trim().length > 0 ? model.trim() : "# Live context\n\n_(empty: write your notes here)_";
  return `${head}\n\n${renderManagedZone(references, sessionKey, revision, budgetTokens, unresolved)}\n`;
}

export function summarizeTurn(message: unknown, toolResults: unknown[] = []): string {
  const parts: string[] = [];
  const content = (message as { content?: unknown[] } | null)?.content;
  if (Array.isArray(content)) {
    const texts: string[] = [];
    const thinking: string[] = [];
    const tools: string[] = [];
    for (const block of content) {
      const b = block as { type?: string; text?: string; thinking?: string; name?: string };
      if (b.type === "text" && b.text) texts.push(b.text);
      else if (b.type === "thinking" && b.thinking) thinking.push(b.thinking);
      else if (b.type === "toolCall" && b.name) tools.push(b.name);
    }
    if (texts.length > 0) parts.push(texts.join(" "));
    else if (thinking.length > 0) parts.push(thinking.join(" "));
    if (tools.length > 0) parts.push(`called: ${[...new Set(tools)].join(", ")}`);
  }
  for (const result of toolResults) {
    const r = result as { toolName?: string; isError?: boolean; content?: unknown[] };
    if (!r?.isError) continue;
    const text = Array.isArray(r.content)
      ? r.content
          .map((c) => (c as { text?: string }).text ?? "")
          .join(" ")
          .replace(/\s+/g, " ")
          .trim()
      : "";
    parts.push(`ERROR from ${r.toolName ?? "tool"}: ${text.slice(0, 200)}`);
  }
  const summary = parts.join(" | ").replace(/\s+/g, " ").trim();
  return summary.length > 0 ? summary.slice(0, 600) : "(no text)";
}

