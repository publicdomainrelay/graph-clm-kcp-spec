import { parse, stringify } from "yaml";

import { canonicalInterfaces, canonicalRequirements, canonicalSet, canonicalSpec } from "./canonical.ts";
import type { Interface, Requirement, SystemContextSpec } from "./types.ts";

export const MANAGED_BEGIN = "<!-- SPECD_MANAGED_BEGIN -->";
export const MANAGED_END = "<!-- SPECD_MANAGED_END -->";
export const DEFAULT_MANAGED_BUDGET = 1500;

export const HEADER_LINE = "# Context: ";
export const SPEC_HEADING = "## spec";
export const SPEC_FENCE = "```yaml spec";
export const SPEC_FENCE_CLOSE = "```";
export const EMPTY_INTENT = "_(empty: write what this context is for)_";
export const NOTICE =
  "_Write the prose above and the fields in the spec block. " +
  "`codeRefs` and the resolved references below are maintained by the tool; " +
  "an edit there is lost._";

export interface CodeRefRecord {
  codegraphId: string;
  kind: string;
  name: string;
  filePath: string;
}

export interface ResolvedRef {
  codegraphId: string;
  kind: string;
  name: string;
  filePath: string;
}

export type Env = Readonly<Record<string, string | undefined>>;

export function contextDocPath(docDir: string, repository: string, context: string): string {
  return `${docDir.replace(/\/+$/, "")}/${repository}/${context}.md`;
}

export function docDirFromEnv(env: Env, home: string | undefined): string {
  if (env.SPECD_CLM_DOC_DIR) return env.SPECD_CLM_DOC_DIR;
  if (env.SPECD_STATE_DIR) return `${env.SPECD_STATE_DIR.replace(/\/+$/, "")}/clm`;
  if (env.XDG_STATE_HOME) return `${env.XDG_STATE_HOME.replace(/\/+$/, "")}/specd/clm`;
  if (home) return `${home.replace(/\/+$/, "")}/.local/state/specd/clm`;
  return "/tmp/specd/clm";
}

export function docPathFromEnv(env: Env, home: string | undefined, repoPath: string, context: string): string {
  if (env.SPECD_CLM_DOC) return env.SPECD_CLM_DOC;
  const repository = env.SPECD_CLM_REPOSITORY || repoPath.replace(/\/+$/, "").split("/").pop() || "repository";
  return contextDocPath(docDirFromEnv(env, home), repository, context);
}

export function estimateTokens(text: string): number {
  return Math.ceil(text.length / 4);
}

export function selectWithinBudget<T>(items: T[], budgetTokens: number, render: (item: T) => string): T[] {
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

interface SpecBlock {
  upstream?: string;
  overlay?: string[];
  orchestrator?: string;
  dependsOn?: string[];
  introduces?: string[];
  requirements?: Requirement[];
  interfaces?: Interface[];
}

export function declared(inSpec: SystemContextSpec): SystemContextSpec {
  const out: SystemContextSpec = {
    intent: inSpec.intent ?? "",
    upstream: inSpec.upstream ?? "",
    requirements: canonicalRequirements(inSpec.requirements ?? []),
    interfaces: canonicalInterfaces(inSpec.interfaces ?? []),
  };
  if (inSpec.overlay?.length) out.overlay = canonicalSet(inSpec.overlay);
  if (inSpec.orchestrator) out.orchestrator = inSpec.orchestrator;
  if (inSpec.dependsOn?.length) out.dependsOn = canonicalSet(inSpec.dependsOn);
  if (inSpec.introduces?.length) out.introduces = canonicalSet(inSpec.introduces);
  return out;
}

export function mergeDeclared(base: SystemContextSpec, declaredSpec: SystemContextSpec): SystemContextSpec {
  const out: SystemContextSpec = { ...base };
  out.intent = declaredSpec.intent ?? "";
  out.upstream = declaredSpec.upstream ?? "";
  out.overlay = declaredSpec.overlay;
  out.orchestrator = declaredSpec.orchestrator;
  out.dependsOn = declaredSpec.dependsOn;
  out.introduces = declaredSpec.introduces;
  out.requirements = declaredSpec.requirements;
  out.interfaces = declaredSpec.interfaces;
  return canonicalSpec(out);
}

export function renderModelZone(context: string, repository: string, inSpec: SystemContextSpec): string {
  const spec = declared(inSpec);
  const lines = [HEADER_LINE + context, ""];
  if (repository) lines.push(`Repository: \`${repository}\``, "");
  lines.push(spec.intent && spec.intent.trim() ? spec.intent.trim() : EMPTY_INTENT, "");
  lines.push(NOTICE, "");
  lines.push(SPEC_HEADING, "", SPEC_FENCE);
  lines.push(renderBlock(spec));
  lines.push(SPEC_FENCE_CLOSE, "");
  return lines.join("\n");
}

export function parseModelZone(text: string): SystemContextSpec {
  const open = text.indexOf(SPEC_FENCE);
  if (open === -1) throw new Error(`the model zone opens no ${SPEC_FENCE} block`);
  const rest = text.slice(open + SPEC_FENCE.length);
  const close = rest.indexOf(SPEC_FENCE_CLOSE);
  if (close === -1) throw new Error(`the ${SPEC_FENCE} block is not closed`);
  const block = parseBlock(rest.slice(0, close));
  return declared({ ...block, intent: parseIntent(text.slice(0, open)) });
}

export function splitContextDoc(text: string): { model: string; managed: string } {
  const begin = text.indexOf(MANAGED_BEGIN);
  const end = text.indexOf(MANAGED_END);
  if (begin === -1 || end === -1 || end < begin) return { model: text.trim(), managed: "" };
  const model = `${text.slice(0, begin)}${text.slice(end + MANAGED_END.length)}`.trim();
  return { model, managed: text.slice(begin, end + MANAGED_END.length) };
}

export function renderManagedZone(refs: ResolvedRef[], budgetTokens = DEFAULT_MANAGED_BUDGET): string {
  const lines = [MANAGED_BEGIN, "## Resolved code references"];
  if (refs.length === 0) {
    lines.push("", "_None yet._");
  } else {
    lines.push("");
    const ordered = [...refs].sort((left, right) => (left.codegraphId < right.codegraphId ? -1 : 1));
    const shown = selectWithinBudget(
      ordered,
      budgetTokens,
      (ref) => `- \`${ref.codegraphId}\` ${ref.kind} ${ref.name} (${ref.filePath})`,
    );
    for (const ref of shown) {
      lines.push(`- \`${ref.codegraphId}\` ${ref.kind} ${ref.name} (${ref.filePath})`);
    }
    const hidden = ordered.length - shown.length;
    if (hidden > 0) {
      lines.push("", `_${hidden} more reference(s) indexed but not listed here to stay inside the ${budgetTokens}-token budget._`);
    }
  }
  lines.push(MANAGED_END);
  return lines.join("\n");
}

export function composeContextDoc(model: string, refs: ResolvedRef[], budgetTokens = DEFAULT_MANAGED_BUDGET): string {
  const head = model.trim().length > 0 ? model.trim() : `# Context\n\n${EMPTY_INTENT}`;
  return `${head}\n\n${renderManagedZone(refs, budgetTokens)}\n`;
}

export function fileRef(codeRef: string): ResolvedRef | undefined {
  const prefix = "file:";
  if (!codeRef.startsWith(prefix)) return undefined;
  const filePath = codeRef.slice(prefix.length);
  if (!filePath) return undefined;
  const cut = filePath.lastIndexOf("/");
  return { codegraphId: codeRef, kind: "file", name: cut === -1 ? filePath : filePath.slice(cut + 1), filePath };
}

function renderBlock(spec: SystemContextSpec): string {
  return stringify({
    upstream: spec.upstream || undefined,
    orchestrator: spec.orchestrator || undefined,
    overlay: spec.overlay?.length ? spec.overlay : undefined,
    dependsOn: spec.dependsOn?.length ? spec.dependsOn : undefined,
    introduces: spec.introduces?.length ? spec.introduces : undefined,
    requirements: spec.requirements?.length ? spec.requirements : undefined,
    interfaces: spec.interfaces?.length ? spec.interfaces : undefined,
  });
}

function parseBlock(body: string): SystemContextSpec {
  const parsed = parse(body);
  if (parsed === null || parsed === undefined) return {};
  if (typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("the spec block is a mapping");
  }
  const block = parsed as SpecBlock;
  const out: SystemContextSpec = {};
  if (block.upstream) out.upstream = String(block.upstream);
  if (block.orchestrator) out.orchestrator = String(block.orchestrator);
  if (Array.isArray(block.overlay)) out.overlay = block.overlay.map(String);
  if (Array.isArray(block.dependsOn)) out.dependsOn = block.dependsOn.map(String);
  if (Array.isArray(block.introduces)) out.introduces = block.introduces.map(String);
  if (Array.isArray(block.requirements)) out.requirements = block.requirements;
  if (Array.isArray(block.interfaces)) out.interfaces = block.interfaces;
  return out;
}

function parseIntent(prose: string): string {
  const kept: string[] = [];
  for (const line of prose.split("\n")) {
    const trimmed = line.trim();
    if (trimmed.startsWith(HEADER_LINE)) continue;
    if (trimmed.startsWith("Repository: `")) continue;
    if (trimmed === NOTICE) continue;
    if (trimmed === SPEC_HEADING) continue;
    kept.push(line);
  }
  const intent = kept.join("\n").trim();
  return intent === EMPTY_INTENT ? "" : intent;
}
