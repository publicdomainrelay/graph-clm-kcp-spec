// What the mod decides, with no host in it: how a tool call becomes the files
// it touched, how the context file becomes a system section, and how the
// environment becomes the state bridge's argument vector. A hook that has to
// ask the engine anything lives in register.ts; everything a test can decide
// without an engine lives here.

const TOUCHED_TOOLS = ["Read", "Write", "Edit", "MultiEdit", "NotebookEdit"];

const CONTEXT_SECTION_ID = "cc-clm-mod:context";

export interface ToolCallLike {
  tool?: unknown;
  [key: string]: unknown;
}

/** The file a tool call names, whichever of the file tools it is. */
export function touchedPath(event: ToolCallLike): string | undefined {
  const tool = String(event.tool ?? "");
  if (!TOUCHED_TOOLS.includes(tool)) return undefined;
  for (const key of ["file_path", "notebook_path", "path"]) {
    const value = event[key];
    if (typeof value === "string" && value.length > 0) return value;
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
