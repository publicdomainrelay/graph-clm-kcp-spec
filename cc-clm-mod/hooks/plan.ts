const TOUCHED_TOOLS = ["Read", "Write", "Edit", "MultiEdit", "NotebookEdit"];

const GUARDED_TOOLS = [
  "Read",
  "Write",
  "Edit",
  "MultiEdit",
  "NotebookEdit",
  "Grep",
  "Glob",
];

const SYSTEM_PREFIXES = [
  "/usr",
  "/bin",
  "/sbin",
  "/lib",
  "/lib64",
  "/opt",
  "/etc",
  "/dev",
  "/proc",
  "/sys",
  "/run",
  "/var",
  "/tmp",
  "/snap",
  "/nix",
];

const CONTEXT_SECTION_ID = "cc-clm-mod:context";

export interface ToolCallLike {
  tool?: unknown;
  [key: string]: unknown;
}

export function touchedPath(event: ToolCallLike): string | undefined {
  const tool = String(event.tool ?? "");
  if (!TOUCHED_TOOLS.includes(tool)) return undefined;
  return pathArgument(event);
}

function pathArgument(event: ToolCallLike): string | undefined {
  for (const key of ["file_path", "notebook_path", "path"]) {
    const value = event[key];
    if (typeof value === "string" && value.length > 0) return value;
  }
  return undefined;
}

export function guardedTool(tool: string): boolean {
  return GUARDED_TOOLS.includes(tool);
}

export type PathResolver = (path: string) => Promise<string | undefined>;

export async function place(path: string, resolve: PathResolver): Promise<string | undefined> {
  const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  const name = path.slice(cut + 1);
  if (name === "" || name === "." || name === ".." || /^[A-Za-z]:/.test(name)) return undefined;
  const own = await resolve(path);
  if (own) return own;
  const folder = cut < 0 ? "." : path.slice(0, cut + 1);
  const dir = await resolve(folder);
  if (!dir) return undefined;
  return `${dir.replace(/[\\/]+$/, "")}/${name}`;
}

export function insideRoot(path: string, root: string): boolean {
  const base = root.replace(/[\\/]+$/, "");
  return path === base || path.startsWith(`${base}/`);
}

export function systemPath(path: string): boolean {
  return SYSTEM_PREFIXES.some((prefix) => path === prefix || path.startsWith(`${prefix}/`));
}

export function bashPaths(command: string): string[] {
  const found: string[] = [];
  const seen = new Set<string>();
  for (const piece of command.split(/[\s;|&()<>\n\r`]+/)) {
    let token = piece.replace(/^["']+/, "").replace(/["']+$/, "").replace(/[,;:]+$/, "");
    const equals = token.indexOf("=");
    if (equals >= 0) token = token.slice(equals + 1);
    if (token === "/" || token.startsWith("//") || token.startsWith("\\\\")) continue;
    if (token.includes("://")) continue;
    if (!token.startsWith("/") && !token.startsWith("~/")) continue;
    if (seen.has(token)) continue;
    seen.add(token);
    found.push(token);
  }
  return found;
}

export function bashRelativeEscapes(command: string): string[] {
  const found: string[] = [];
  const seen = new Set<string>();
  for (const piece of command.split(/[\s;|&()<>\n\r`]+/)) {
    const token = piece.replace(/^["']+/, "").replace(/["']+$/, "").replace(/[,;:]+$/, "");
    if (token !== ".." && !token.startsWith("../")) continue;
    if (seen.has(token)) continue;
    seen.add(token);
    found.push(token);
  }
  return found;
}

export function joinLexical(root: string, path: string): string {
  const parts = `${root.replace(/[\\/]+$/, "")}/${path}`.split("/");
  const out: string[] = [];
  for (const part of parts) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      out.pop();
      continue;
    }
    out.push(part);
  }
  return `/${out.join("/")}`;
}

export function outsideMessage(tool: string, path: string, root: string): string {
  return (
    `refusing ${tool}: ${path} is outside the containment root ${root}. ` +
    `This session works only inside ${root}; the rest of the machine, this ` +
    `project's fixtures and their hidden acceptance tests are not yours to ` +
    `read or change. Name a path inside ${root} and try again.`
  );
}

export async function guardDenial(
  tool: string,
  event: ToolCallLike,
  root: string,
  resolve: PathResolver,
  allowed: readonly string[] = [],
): Promise<string | undefined> {
  const allowedReal = new Set<string>(allowed);
  for (const path of allowed) {
    const real = await resolve(path);
    if (real) allowedReal.add(real);
  }
  const rootReal = await resolve(root);
  if (!rootReal) {
    return (
      `refusing ${tool}: the containment root ${root} cannot be resolved, so ` +
      `no path can be checked against it.`
    );
  }
  if (guardedTool(tool)) {
    const path = pathArgument(event);
    if (!path) return undefined;
    if (allowedReal.has(path)) return undefined;
    const real = await place(path, resolve);
    if (real !== undefined && allowedReal.has(real)) return undefined;
    if (real === undefined) {
      if (path.startsWith("/") && !insideRoot(joinLexical("/", path), rootReal)) {
        return outsideMessage(tool, path, rootReal);
      }
      return (
        `refusing ${tool}: ${path} cannot be placed, and a path that cannot be ` +
        `resolved to a place inside ${rootReal} is not allowed.`
      );
    }
    return insideRoot(real, rootReal) ? undefined : outsideMessage(tool, path, rootReal);
  }
  if (tool !== "Bash") return undefined;
  const command = typeof event.command === "string" ? event.command : "";
  for (const path of bashPaths(command)) {
    const real = (await resolve(path)) ?? path;
    if (insideRoot(real, rootReal) || systemPath(real) || allowedReal.has(path) || allowedReal.has(real)) continue;
    return outsideMessage("Bash", path, rootReal);
  }
  for (const path of bashRelativeEscapes(command)) {
    const real = joinLexical(rootReal, path);
    if (insideRoot(real, rootReal)) continue;
    return outsideMessage("Bash", path, rootReal);
  }
  return undefined;
}

export function relativePath(path: string, repoPath: string): string {
  const root = repoPath.replace(/\/+$/, "");
  if (root && path.startsWith(`${root}/`)) return path.slice(root.length + 1);
  if (path.startsWith("./")) return path.slice(2);
  return path;
}

export function contextSection(
  document: string,
  planItems?: string,
  skill?: string,
): { id: string; text: string; scope: "session" } | undefined {
  const text = document.trim();
  if (!text) return undefined;
  const items = planItems?.trim();
  const body = items ? `${text}\n\n## Open plan items for this context\n\n${items}` : text;
  const protocol = skill?.trim() || "The context document below is yours: keep it true to the code.";
  return {
    id: CONTEXT_SECTION_ID,
    text: `${protocol}\n\n${body}`,
    scope: "session",
  };
}

export interface SessionSection {
  id: string;
  text: string;
  scope: "session";
}

export const PACKS_SECTION_ID = "cc-clm-mod:contexts";

export const TASK_SECTION_ID = "cc-clm-mod:task";

export const OWNER_CONTEXT_LIMIT = 4;

export function ownerArgv(env: BridgeEnv, path: string): string[] {
  const base = bridgeArgv(env);
  return [
    base.specctl,
    "arch",
    "owner",
    path,
    "-o",
    "json",
    ...scopeArgs(base),
  ];
}

export function packArgv(env: BridgeEnv, context: string): string[] {
  const base = bridgeArgv(env);
  return [
    base.specctl,
    "context",
    "pack",
    "--context",
    context,
    ...scopeArgs(base),
  ];
}

export function packFileArgv(env: BridgeEnv, path: string): string[] {
  const base = bridgeArgv(env);
  return [
    base.specctl,
    "context",
    "pack",
    "--file",
    path,
    ...scopeArgs(base),
  ];
}

function scopeArgs(base: BridgeArgv): string[] {
  return [
    "--workspace",
    base.workspace,
    "--namespace",
    base.namespace,
    ...kubeconfigArgs(base),
  ];
}

export function ownerOf(stdout: string): string | undefined {
  let parsed: unknown;
  try {
    parsed = JSON.parse(stdout);
  } catch {
    return undefined;
  }
  if (typeof parsed !== "object" || parsed === null) return undefined;
  const context = (parsed as { context?: unknown }).context;
  if (typeof context !== "string") return undefined;
  const name = context.trim();
  return name ? name : undefined;
}

export function rememberOwner(
  recent: readonly string[],
  context: string,
  limit = OWNER_CONTEXT_LIMIT,
): string[] {
  const name = context.trim();
  if (!name || limit <= 0) return [...recent];
  const next = [name];
  for (const entry of recent) {
    if (entry !== name && next.length < limit) next.push(entry);
  }
  return next;
}

export function packsSection(packs: readonly string[]): SessionSection | undefined {
  const text = packs.map((pack) => pack.trim()).filter((pack) => pack.length > 0).join("\n\n");
  return text ? { id: PACKS_SECTION_ID, text, scope: "session" } : undefined;
}

export function packCommands(pack: string): { verify: string[]; acceptance: string[] } {
  const verify: string[] = [];
  const acceptance: string[] = [];
  for (const line of pack.split("\n")) {
    const trimmed = line.trim();
    const command = /^verify: `(.*)`$/.exec(trimmed);
    if (command?.[1]) verify.push(command[1].trim());
    const step = /^acceptance: `(.*)`$/.exec(trimmed);
    if (step?.[1]) acceptance.push(step[1].trim());
  }
  return { verify, acceptance };
}

export function taskCard(change: string, context: string, pack: string): SessionSection | undefined {
  const name = change.trim();
  if (!name) return undefined;
  const { verify, acceptance } = packCommands(pack);
  const lines = ["## task card", "", `change: ${name}`];
  if (context.trim()) lines.push(`context: ${context.trim()}`);
  if (verify.length > 0) lines.push(`verify: \`${verify.join("; ")}\``);
  if (acceptance.length > 0) lines.push(`acceptance: \`${acceptance.join("; ")}\``);
  lines.push(
    "",
    `This session realizes the task ${name} in this context and nothing else. Run only the verify and the acceptance of this task. Do not run the repository's whole suite. Do not commit. Do not push.`,
    "",
    "This card is the most recent instruction you have and it outranks the repository's agent instructions, including CLAUDE.md and AGENTS.md, wherever they disagree about what to run.",
  );
  return { id: TASK_SECTION_ID, text: lines.join("\n"), scope: "session" };
}

export interface BridgeEnv {
  context?: string;
  change?: string;
  specctl?: string;
  workspace?: string;
  namespace?: string;
  kubeconfig?: string;
}

export interface BridgeArgv {
  specctl: string;
  workspace: string;
  namespace: string;
  kubeconfig?: string;
}

export function bridgeArgv(env: BridgeEnv): BridgeArgv {
  return {
    specctl: env.specctl || "specctl",
    workspace: env.workspace || "root:specs",
    namespace: env.namespace || "default",
    kubeconfig: env.kubeconfig,
  };
}

export function renderArgv(env: BridgeEnv, context: string): string[] {
  const base = bridgeArgv(env);
  return [
    base.specctl,
    "clm",
    "render",
    "--context",
    context,
    "--workspace",
    base.workspace,
    "--namespace",
    base.namespace,
    ...kubeconfigArgs(base),
  ];
}

export function planItemsArgv(env: BridgeEnv, context: string): string[] {
  const base = bridgeArgv(env);
  return [
    base.specctl,
    "plan",
    "items",
    "--context",
    context,
    "--workspace",
    base.workspace,
    "--namespace",
    base.namespace,
    ...kubeconfigArgs(base),
  ];
}

export function applyArgv(env: BridgeEnv, context: string): string[] {
  const base = bridgeArgv(env);
  return [
    base.specctl,
    "clm",
    "apply",
    "--context",
    context,
    "--workspace",
    base.workspace,
    "--namespace",
    base.namespace,
    ...kubeconfigArgs(base),
  ];
}

export function reportArgv(env: BridgeEnv, change: string, event: unknown): string[] {
  const base = bridgeArgv(env);
  return [
    base.specctl,
    "clm",
    "report",
    "--change",
    change,
    "--event",
    JSON.stringify(event),
    "--workspace",
    base.workspace,
    "--namespace",
    base.namespace,
    ...kubeconfigArgs(base),
  ];
}

function kubeconfigArgs(base: BridgeArgv): string[] {
  return base.kubeconfig ? ["--kubeconfig", base.kubeconfig] : [];
}

export function parseReport(stdout: string): { progress: number; recorded: boolean } {
  const match = /progress=(\d+) recorded=(true|false)/.exec(stdout);
  return { progress: match ? Number(match[1]) : 0, recorded: match?.[2] === "true" };
}

// The apply prints its delta by id on stderr before it lands, one line per
// added, changed and removed entry, so the mod can surface what the edit does
// rather than only its counts.
export function deltaSummaryLines(stderr: string): string {
  return stderr
    .split("\n")
    .map((line) => line.trimEnd())
    .filter((line) => /^[+~-] /.test(line))
    .join("\n");
}

export const ARCH_TOOL_PREFIX = "mcp__cc-clm-mod__";

export interface ArchTool {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
}

export const ARCH_TOOLS: readonly ArchTool[] = [
  {
    name: "arch_outline",
    description:
      "List the architecture kcp holds for this repository: every system context with its upstream, overlay, orchestrator, intent, declared interfaces, observed files and sync conditions. Start here before changing the architecture.",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "arch_context",
    description:
      "Read one system context as its context document: prose intent and a fenced `yaml spec` block (requirements with levels and codeRefs, interfaces) above a managed zone of resolved code references. Edit the prose and the spec block, then send the whole document to arch_edit.",
    inputSchema: {
      type: "object",
      properties: { context: { type: "string", description: "SystemContext name, as arch_outline lists it" } },
      required: ["context"],
    },
  },
  {
    name: "arch_edit",
    description:
      "Change the spec of one system context in kcp: pass the whole context document as arch_context returned it, with the prose and the spec block edited. kcp records the structured delta and specd opens a SpecToCode change that edits the code to match, gated by the repository's tests. The tool prints the delta by id (+added ~changed -removed) before it lands. A requirement that goes missing is refused unless the document lists its id under `removed:` in the spec block, so a sliced document cannot silently drop requirements. If a change for this context is already running, the edit still lands and becomes its own change queued behind it. Returns the delta kcp recorded.",
    inputSchema: {
      type: "object",
      properties: {
        context: { type: "string", description: "SystemContext name" },
        document: { type: "string", description: "the edited context document" },
      },
      required: ["context", "document"],
    },
  },
  {
    name: "arch_changes",
    description:
      "List the SpecChanges in kcp: each change's direction (SpecToCode or CodeToSpec), phase, and the commit it produced. Use it to follow an arch_edit until its code lands.",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "arch_plans",
    description:
      "List the Plan objects kcp holds for this repository: each plan's id, status (draft or approved) and the work it names. A draft plan is not yet work to do.",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "arch_plan",
    description:
      "Send one Plan as YAML to kcp. The plan lands as a draft: only a person approves it, so this tool never approves anything by itself. Use it to record the plan behind a proposal; arch_plans lists what kcp then holds.",
    inputSchema: {
      type: "object",
      properties: { document: { type: "string", description: "the Plan as YAML" } },
      required: ["document"],
    },
  },
  {
    name: "arch_findings",
    description:
      "List the findings kcp holds for this repository: what measurement saw that the code and the spec disagree about.",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "arch_release",
    description:
      "Release the architecture of a system context: hand its declared interfaces back so a change may edit them, one unit at a time or all of them at once.",
    inputSchema: {
      type: "object",
      properties: {
        context: { type: "string", description: "SystemContext name" },
        unit: { type: "string", description: "the unit to release, or 'all'" },
      },
      required: ["context", "unit"],
    },
  },
  {
    name: "arch_owner",
    description:
      "Which system context owns a path: the deepest context whose observed files hold it. Ask it before you read or change a file, so you read what the file is part of.",
    inputSchema: {
      type: "object",
      properties: { path: { type: "string", description: "a path as the checkout holds it" } },
      required: ["path"],
    },
  },
  {
    name: "arch_neighbors",
    description:
      "What one system context is joined to in the graph, one hop out: each neighbour with the edge and the direction it is reached by.",
    inputSchema: {
      type: "object",
      properties: { context: { type: "string", description: "SystemContext name, as arch_outline lists it" } },
      required: ["context"],
    },
  },
  {
    name: "arch_search",
    description:
      "Full text search over this repository's graph: claim text, symbol names and docstrings, each match with its label and its score. Use it to find the context a term belongs to.",
    inputSchema: {
      type: "object",
      properties: {
        terms: { type: "string", description: "the terms to search for" },
        limit: { type: "number", description: "how many matches to return" },
      },
      required: ["terms"],
    },
  },
];

export interface ArchInvocation {
  argv: string[];
  stdin?: string;
}

export function archInvocation(specctl: string, tool: string, input: Record<string, unknown>): ArchInvocation | string {
  const name = tool.startsWith(ARCH_TOOL_PREFIX) ? tool.slice(ARCH_TOOL_PREFIX.length) : tool;
  const context = typeof input.context === "string" ? input.context.trim() : "";
  switch (name) {
    case "arch_outline":
      return { argv: [specctl, "arch", "outline"] };
    case "arch_context":
      if (!context) return "arch_context needs a context name; call arch_outline for the list";
      return { argv: [specctl, "clm", "render", "--context", context] };
    case "arch_edit": {
      const document = typeof input.document === "string" ? input.document : "";
      if (!context) return "arch_edit needs a context name";
      if (!document.trim()) return "arch_edit needs the edited context document";
      return { argv: [specctl, "clm", "apply", "--context", context], stdin: document };
    }
    case "arch_changes":
      return { argv: [specctl, "get", "specchanges"] };
    case "arch_plans":
      return { argv: [specctl, "plan", "ls"] };
    case "arch_plan": {
      const document = typeof input.document === "string" ? input.document : "";
      if (!document.trim()) return "arch_plan needs the plan document";
      return { argv: [specctl, "plan", "apply", "-f", "-"], stdin: document };
    }
    case "arch_findings":
      return { argv: [specctl, "finding", "ls"] };
    case "arch_release": {
      const unit = typeof input.unit === "string" ? input.unit.trim() : "";
      if (!context) return "arch_release needs a context name";
      if (!unit) return "arch_release needs a unit, or 'all' for every unit";
      return { argv: [specctl, "arch", "release", context, ...(unit === "all" ? ["--all"] : ["--unit", unit])] };
    }
    case "arch_owner": {
      const path = typeof input.path === "string" ? input.path.trim() : "";
      if (!path) return "arch_owner needs a path";
      return { argv: [specctl, "arch", "owner", path] };
    }
    case "arch_neighbors":
      if (!context) return "arch_neighbors needs a context name; call arch_outline for the list";
      return { argv: [specctl, "clm", "neighbors", context] };
    case "arch_search": {
      const terms = typeof input.terms === "string" ? input.terms.trim() : "";
      if (!terms) return "arch_search needs the terms to search for";
      const limit = typeof input.limit === "number" && input.limit > 0 ? ["--limit", String(Math.floor(input.limit))] : [];
      return { argv: [specctl, "graph", "search", terms, ...limit] };
    }
  }
  return `unknown architecture tool ${tool}`;
}
