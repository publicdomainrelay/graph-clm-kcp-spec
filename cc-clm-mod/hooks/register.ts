import type { Register } from "claude-code";

import {
  ClmHost,
  docPathFromEnv,
  type CommandResult,
  type FileStore,
  type ProgressRecord,
  type RunOptions,
  type Runner,
  type StateBridge,
} from "./vendor/core.js";
import {
  ARCH_TOOL_PREFIX,
  ARCH_TOOLS,
  archInvocation,
  applyArgv,
  contextSection,
  guardDenial,
  parseReport,
  relativePath,
  renderArgv,
  reportArgv,
  touchedPath,
  type BridgeEnv,
  type PathResolver,
} from "./plan.js";

interface Engine {
  fs: {
    read(path: string): Promise<string>;
    write(path: string, text: string): Promise<void>;
    exists(path: string): Promise<boolean>;
    stat(path: string, options?: { resolve?: boolean }): Promise<{ realPath?: string }>;
  };
  process: {
    run(
      argv: readonly string[],
      init?: { cwd?: string; env?: Record<string, string>; stdin?: string; timeoutMs?: number },
    ): Promise<{ exitCode: number; stdout: string; stderr: string }>;
  };
  env: { get(name: string): Promise<string | undefined> };
  tool: {
    register(tool: { name: string; description: string; inputSchema?: Record<string, unknown> }): Promise<unknown>;
  };
  ui: {
    status(text: string | undefined): void;
    log(text: string, options?: { to?: "debug" }): void;
  };
}

interface Session {
  host?: ClmHost;
  repoPath: string;

  root?: string;
  rootRead?: boolean;
  doc?: string;
  archSpecctl?: string;
  archCwd?: string;
}

function statResolver($: Engine): PathResolver {
  return async (path) => {
    const stat = await $.fs.stat(path, { resolve: true }).catch(() => undefined);
    return stat?.realPath;
  };
}

async function scopeRoot($: Engine, session: Session): Promise<string | undefined> {
  if (!session.rootRead) {
    session.rootRead = true;
    const root = (await $.env.get("SPECD_CLM_ROOT"))?.trim();
    session.root = root ? root : undefined;
    const doc = (await $.env.get("SPECD_CLM_DOC"))?.trim();
    if (doc && !session.doc) session.doc = doc;
  }
  return session.root;
}

function modRunner($: Engine): Runner {
  return {
    async run(argv: readonly string[], options: RunOptions = {}): Promise<CommandResult> {
      const result = await $.process.run(argv, {
        cwd: options.cwd,
        env: options.env,
        stdin: options.stdin,
        timeoutMs: options.timeoutMs,
      });
      return { exitCode: result.exitCode, stdout: result.stdout, stderr: result.stderr };
    },
  };
}

function modFileStore($: Engine): FileStore {
  return {
    read: (path) => $.fs.read(path),
    write: (path, text) => $.fs.write(path, text),
    exists: (path) => $.fs.exists(path),
    async ancestors() {
      return [];
    },
  };
}

function modBridge($: Engine, env: BridgeEnv, cwd: string): StateBridge {
  const runner = modRunner($);
  const call = async (argv: readonly string[], stdin?: string) => {
    const result = await runner.run(argv, { cwd, stdin, timeoutMs: 120_000 });
    if (result.exitCode !== 0) {
      throw new Error(`${argv[0]} ${argv.slice(1).join(" ")} exited ${result.exitCode}: ${result.stderr.trim()}`);
    }
    return result;
  };

  return {
    async render(context: string) {
      return (await call(renderArgv(env, context))).stdout;
    },
    async apply(context: string, modelZone: string) {
      const result = await call(applyArgv(env, context), modelZone);
      let delta: unknown = {};
      try {
        delta = JSON.parse(result.stdout);
      } catch {
        delta = {};
      }
      return {
        delta,
        applied: result.stderr.includes("applied"),
        folded: /folded into (\S+)/.exec(result.stderr)?.[1],
      };
    },
    async report(change: string, event: ProgressRecord) {
      const result = await call(reportArgv(env, change, event));
      return parseReport(result.stdout);
    },
  };
}

async function readEnv($: Engine): Promise<BridgeEnv> {
  return {
    context: await $.env.get("SPECD_CLM_CONTEXT"),
    change: await $.env.get("SPECD_CLM_CHANGE"),
    specctl: await $.env.get("SPECD_SPECCTL"),
    workspace: await $.env.get("SPECD_WORKSPACE"),
    namespace: await $.env.get("SPECD_NAMESPACE"),
    kubeconfig: (await $.env.get("KUBECONFIG")) ?? (await $.env.get("SPECD_KUBECONFIG")),
  };
}

async function start($: Engine, session: Session, cwd: string): Promise<void> {
  const repo = await $.env.get("SPECD_CLM_REPO");
  session.repoPath = repo ?? cwd;
  const env = await readEnv($);
  const context = env.context ?? "";
  if (!context) {
    session.host = undefined;
    return;
  }
  session.doc = docPathFromEnv(
    {
      SPECD_CLM_DOC: await $.env.get("SPECD_CLM_DOC"),
      SPECD_CLM_DOC_DIR: await $.env.get("SPECD_CLM_DOC_DIR"),
      SPECD_CLM_REPOSITORY: await $.env.get("SPECD_CLM_REPOSITORY"),
      SPECD_STATE_DIR: await $.env.get("SPECD_STATE_DIR"),
      XDG_STATE_HOME: await $.env.get("XDG_STATE_HOME"),
    },
    await $.env.get("HOME"),
    session.repoPath,
    context,
  );
  const host = new ClmHost({
    bridge: modBridge($, env, session.repoPath),
    files: modFileStore($),
    repoPath: session.repoPath,
    docPath: session.doc,
    context,
    change: env.change,
  });
  session.host = host;
  const rendered = await host.start();
  if (!rendered) return;
  $.ui.status(`clm: ${context}${env.change ? ` (${env.change})` : ""}`);
  if (rendered.changed) $.ui.log(`clm: rendered ${rendered.path}`);
}

async function settle($: Engine, session: Session): Promise<void> {
  if (!session.host) return;
  try {
    const applied = await session.host.finish("turn");
    if (applied?.applied) {
      $.ui.log(`clm: applied the spec edit${applied.folded ? ` into ${applied.folded}` : ""}`);
    } else if (applied?.error) {
      $.ui.log(`clm: the spec edit was not applied: ${applied.error}`);
    }
  } catch (error) {
    $.ui.log(`clm: ${describe(error)}`, { to: "debug" });
  }
}

async function offerArchTools($: Engine, session: Session, cwd: string): Promise<void> {
  const specctl = (await $.env.get("SPECD_SPECCTL"))?.trim() || "specctl";
  const found = await $.process.run([specctl, "env", "--repo", cwd, "-o", "json"], { cwd, timeoutMs: 10_000 }).catch(() => undefined);
  if (!found || found.exitCode !== 0) return;
  for (const tool of ARCH_TOOLS) {
    await $.tool.register({ name: tool.name, description: tool.description, inputSchema: tool.inputSchema });
  }
  session.archSpecctl = specctl;
  session.archCwd = cwd;
  $.ui.status("clm: arch tools on (specctl up session)");
}

async function runArchTool($: Engine, session: Session, tool: string, input: Record<string, unknown>): Promise<string> {
  const invocation = archInvocation(session.archSpecctl ?? "specctl", tool, input);
  if (typeof invocation === "string") return invocation;
  const ran = await $.process
    .run(invocation.argv, { cwd: session.archCwd, stdin: invocation.stdin, timeoutMs: 60_000 })
    .catch((error: unknown) => ({ exitCode: 1, stdout: "", stderr: describe(error) }));
  const text = `${ran.stdout}${ran.stderr ? `\n${ran.stderr}` : ""}`.trim();
  return ran.exitCode === 0 ? text || "(no output)" : `failed (exit ${ran.exitCode}): ${text}`;
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export const register: Register = (on) => {
  const session: Session = { repoPath: "." };

  on("session.start", async ($, e, next) => {
    try {
      await start($ as Engine, session, e.cwd);
    } catch (error) {
      $.ui.log(`clm: ${describe(error)}`, { to: "debug" });
    }
    const started = await next(e);
    if (!session.host) {
      try {
        await offerArchTools($ as Engine, session, e.cwd);
      } catch (error) {
        $.ui.log(`clm: ${describe(error)}`, { to: "debug" });
      }
    }
    return started;
  });

  on("prompt.compose", async ($, e, next) => {
    const composed = await next(e);
    if (!session.host) return composed;
    const document = await session.host.section().catch(() => undefined);
    const section = document ? contextSection(document) : undefined;
    return section ? { sections: [...composed.sections, section] } : composed;
  });

  on("tool.call", async ($, e, next) => {
    if (session.archSpecctl && String(e.tool).startsWith(ARCH_TOOL_PREFIX)) {
      return { result: await runArchTool($ as Engine, session, String(e.tool), e as unknown as Record<string, unknown>) };
    }
    const root = await scopeRoot($ as Engine, session);
    if (root) {
      const denial = await guardDenial(
        String(e.tool),
        e as unknown as Record<string, unknown>,
        root,
        statResolver($ as Engine),
        session.doc ? [session.doc] : [],
      );
      if (denial) {
        $.ui.log(`clm: ${denial}`, { to: "debug" });
        return { deny: denial };
      }
    }

    const result = await next(e);
    const path = touchedPath(e as unknown as Record<string, unknown>);
    if (!session.host || !path || path === session.doc) return result;
    await session.host.touched(String(e.tool), [relativePath(path, session.repoPath)]);
    return result;
  });

  on("turn.complete", async ($, e, next) => {
    const result = await next(e);
    await settle($ as Engine, session);
    return result;
  });

  on("session.end", async ($, e, next) => {
    if (next.budget.remainingMs > 1_000) await settle($ as Engine, session);
    return next(e);
  });
};
