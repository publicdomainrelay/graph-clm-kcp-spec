// cc-clm-mod: the Claude Code host of the CLM loop. specd launches the model
// inside a SpecChange with this folder loaded, and the hooks below report into
// the same context file, graph and kcp state the controllers watch — so the
// alignment of code to spec is observed while the work happens, not guessed
// afterwards.
//
// The engine's rules shape this file. A mod has no Node and no sockets: it
// reaches the host only through `$`, and `$` may be followed only into a
// function declared in this same file, never across an import. So the pure
// decisions live in plan.ts, the shared logic comes from the vendored core, and
// everything that touches `$` is here.

import type { Register } from "claude-code";

import {
  ClmHost,
  type CommandResult,
  type FileStore,
  type ProgressRecord,
  type RunOptions,
  type Runner,
  type StateBridge,
} from "./vendor/core.js";
import {
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

/** The slice of the engine interface this mod uses. */
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
  ui: {
    status(text: string | undefined): void;
    log(text: string, options?: { to?: "debug" }): void;
  };
}

/** The state the hooks of one session share. */
interface Session {
  host?: ClmHost;
  repoPath: string;

  // The containment root the scope guard enforces. It is read once, lazily:
  // the tool.call hook may run before session.start, and a session without it
  // is an ordinary one whose paths are not the guard's business.
  root?: string;
  rootRead?: boolean;
}

/** Where a path lands, through the engine's own file system. */
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
  }
  return session.root;
}

// ---- the core's ports, over $ -------------------------------------------

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
    // A mod has no ancestor walk of its own, and the engine already reads the
    // CLAUDE.md stack for the model; the context document is the mod's part.
    async ancestors() {
      return [];
    },
  };
}

/**
 * The state bridge over `specctl clm`. The mod never computes a delta or writes
 * a graph row itself: the Go CLI does, so both hosts and the controller agree
 * on what changed.
 */
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

// ---- what a session does ------------------------------------------------

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
    // No controller put this session inside a change, so the mod has nothing to
    // report into and the session is an ordinary one.
    session.host = undefined;
    return;
  }
  const host = new ClmHost({
    bridge: modBridge($, env, session.repoPath),
    files: modFileStore($),
    repoPath: session.repoPath,
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
    return next(e);
  });

  // The context document is one more system section, added last: it is session
  // text, so it belongs after the shared half of the prompt.
  on("prompt.compose", async ($, e, next) => {
    const composed = await next(e);
    if (!session.host) return composed;
    const document = await session.host.section().catch(() => undefined);
    const section = document ? contextSection(document) : undefined;
    return section ? { sections: [...composed.sections, section] } : composed;
  });

  on("tool.call", async ($, e, next) => {
    // The scope guard runs before anything beneath: a path outside the root
    // must not be read, listed or written even once, so the call never reaches
    // the tool. The refusal is the model's, as an error result it can learn
    // from.
    const root = await scopeRoot($ as Engine, session);
    if (root) {
      const denial = await guardDenial(
        String(e.tool),
        e as unknown as Record<string, unknown>,
        root,
        statResolver($ as Engine),
      );
      if (denial) {
        $.ui.log(`clm: ${denial}`, { to: "debug" });
        return { deny: denial };
      }
    }

    // A tool call that touched a file is reported against the running change, so
    // `kubectl get specchange -w` shows the work while it happens.
    const result = await next(e);
    const path = touchedPath(e as unknown as Record<string, unknown>);
    if (!session.host || !path) return result;
    await session.host.touched(String(e.tool), [relativePath(path, session.repoPath)]);
    return result;
  });

  on("turn.complete", async ($, e, next) => {
    const result = await next(e);
    await settle($ as Engine, session);
    return result;
  });

  // A session ends on one short wall clock, so the apply is skipped when there
  // is no room for it: an exit that hangs is worse than a spec edit the next
  // session renders again from kcp.
  on("session.end", async ($, e, next) => {
    if (next.budget.remainingMs > 1_000) await settle($ as Engine, session);
    return next(e);
  });
};
