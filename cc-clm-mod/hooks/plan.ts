// What the mod decides, with no host in it: how a tool call becomes the files
// it touched, how the context file becomes a system section, and how the
// environment becomes the state bridge's argument vector. A hook that has to
// ask the engine anything lives in register.ts; everything a test can decide
// without an engine lives here.

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

// The absolute prefixes a Bash command may name without leaving the session.
// They are the system's own: the tools the verify command needs, the device
// files a shell redirects to, and /tmp, where a build keeps its scratch. A
// path under the containment root is allowed whatever it is; a path under
// none of these and not under the root is refused.
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

/** The file a tool call names, whichever of the file tools it is. */
export function touchedPath(event: ToolCallLike): string | undefined {
  const tool = String(event.tool ?? "");
  if (!TOUCHED_TOOLS.includes(tool)) return undefined;
  return pathArgument(event);
}

/** Every path-bearing argument key a tool call may carry. */
function pathArgument(event: ToolCallLike): string | undefined {
  for (const key of ["file_path", "notebook_path", "path"]) {
    const value = event[key];
    if (typeof value === "string" && value.length > 0) return value;
  }
  return undefined;
}

/** Whether the scope guard checks this tool's path arguments. */
export function guardedTool(tool: string): boolean {
  return GUARDED_TOOLS.includes(tool);
}

/** Where a path lands, or undefined when no spelling of it can be placed. */
export type PathResolver = (path: string) => Promise<string | undefined>;

/**
 * The absolute path a spelling lands on. The path itself is tried first, then
 * its folder and the name kept, so a Write to a file that is not there yet is
 * placed as reliably as a Read of one that is. The folder is kept with its
 * separator so a drive or a share root stays that root.
 */
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

/** Whether an absolute path is the root itself or below it. */
export function insideRoot(path: string, root: string): boolean {
  const base = root.replace(/[\\/]+$/, "");
  return path === base || path.startsWith(`${base}/`);
}

/** Whether an absolute path is one the system owns. */
export function systemPath(path: string): boolean {
  return SYSTEM_PREFIXES.some((prefix) => path === prefix || path.startsWith(`${prefix}/`));
}

/**
 * The absolute-looking paths a shell command names, best effort. Word splitting
 * is on whitespace and the shell operators, quotes are trimmed, and a value is
 * read out of `--flag=value`; a token that is a URL, a bare `/`, or a network
 * or device spelling (`//host`, `\\host`) is left alone. A `..`-relative token
 * is not returned here: it is a relative escape and the caller resolves it
 * against the root.
 */
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

/** The `..`-relative tokens a shell command names, best effort. */
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

/** The one lexical join used on a `..`-relative token, with no file system. */
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

/** The refusal text the model receives, so a deny teaches where the line is. */
export function outsideMessage(tool: string, path: string, root: string): string {
  return (
    `refusing ${tool}: ${path} is outside the containment root ${root}. ` +
    `This session works only inside ${root}; the rest of the machine, this ` +
    `project's fixtures and their hidden acceptance tests are not yours to ` +
    `read or change. Name a path inside ${root} and try again.`
  );
}

/**
 * The scope guard's decision, with the file system behind `resolve`. An
 * undefined answer allows the call; a string is the reason the model reads.
 * The decision is pure given the resolver, so the whole allow/deny matrix is
 * a unit test.
 */
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
      // An absolute spelling that leads nowhere is still judged by where it
      // says it is, so a probe of a file the session may not touch is refused
      // with the same reason as one that is there.
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

/** The path as the managed tree sees it, so two spellings are one file. */
export function relativePath(path: string, repoPath: string): string {
  const root = repoPath.replace(/\/+$/, "");
  if (root && path.startsWith(`${root}/`)) return path.slice(root.length + 1);
  if (path.startsWith("./")) return path.slice(2);
  return path;
}

/** The system section the context file is injected as. */
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

/**
 * The shared flags of every `specctl clm` call. The workspace and the namespace
 * are named explicitly, because the mod runs inside a workspace the controller
 * chose, not inside `root:specs`.
 */
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

/** The parse of `specctl clm report`'s one line of output. */
export function parseReport(stdout: string): { progress: number; recorded: boolean } {
  const match = /progress=(\d+) recorded=(true|false)/.exec(stdout);
  return { progress: match ? Number(match[1]) : 0, recorded: match?.[2] === "true" };
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
      "Change the spec of one system context in kcp: pass the whole context document as arch_context returned it, with the prose and the spec block edited. kcp records the structured delta and specd opens a SpecToCode change that edits the code to match, gated by the repository's tests. Returns the delta kcp recorded.",
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
  }
  return `unknown architecture tool ${tool}`;
}
