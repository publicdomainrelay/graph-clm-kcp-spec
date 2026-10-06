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

export function contextSection(document: string): { id: string; text: string; scope: "session" } | undefined {
  const text = document.trim();
  if (!text) return undefined;
  return {
    id: CONTEXT_SECTION_ID,
    text: `The context document below is yours: keep it true to the code.\n\n${text}`,
    scope: "session",
  };
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
];

/**
 * The org root tools: read only views of a git superproject whose submodules
 * are the repositories of one system. They need no kcp, only `specctl org`.
 */
export const ORG_TOOLS: readonly ArchTool[] = [
  {
    name: "org_members",
    description:
      "In an org root (a git superproject), list its submodules: the commit each pins, whether the checkout is at the pin, dirty or unpublished, and where each member's own spec and policy branches are. Start here when the work may cross repositories; outside an org root it says so.",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "org_status",
    description:
      "In an org root, list what must be fixed before the root can be trusted or pushed: an unpublished pin (push the member first), a dirty or unbumped member, a member with no spec. Read it before committing a pointer.",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "org_outline",
    description:
      "In an org root, outline one member's architecture, read in place from that member's open-architecture branch at the commit the root pins. Without a member it outlines the root's own architecture and lists the members.",
    inputSchema: {
      type: "object",
      properties: { member: { type: "string", description: "member repository name, as org_members lists it" } },
    },
  },
  {
    name: "org_history",
    description:
      "In an org root, list the root commits that moved a submodule pointer, with the member commits each one brought in and the member spec each pin resolves to. Pass a member to follow one repository.",
    inputSchema: {
      type: "object",
      properties: { member: { type: "string", description: "only the moves of this member" } },
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
    case "org_members":
      return { argv: [specctl, "org", "ls"] };
    case "org_status":
      return { argv: [specctl, "org", "status"] };
    case "org_outline": {
      const member = typeof input.member === "string" ? input.member.trim() : "";
      return { argv: member ? [specctl, "org", "outline", "--member", member] : [specctl, "org", "outline"] };
    }
    case "org_history": {
      const member = typeof input.member === "string" ? input.member.trim() : "";
      return { argv: member ? [specctl, "org", "history", "--member", member] : [specctl, "org", "history"] };
    }
  }
  return `unknown architecture tool ${tool}`;
}
