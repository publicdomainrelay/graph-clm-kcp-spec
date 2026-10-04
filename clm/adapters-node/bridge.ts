// The StateBridge over the Go CLI. kcp access, the delta authority and the
// graph writes have one implementation, so a host reaches them the same way a
// shell does: `specctl clm render|apply|report`. A host never needs a kube
// client or a Bolt driver of its own.

import type { ProgressRecord, StateBridge } from "../core/mod.ts";
import { nodeRunner } from "./runner.ts";
import type { Runner } from "../core/mod.ts";

export interface BridgeOptions {
  /** The specctl binary; SPECD_SPECCTL when unset, `specctl` when that is too. */
  specctl?: string;
  runner?: Runner;
  /**
   * The environment every call inherits. The workspace kubeconfig and the bolt
   * endpoint come from the process environment when this is left out, so a host
   * started by the controller needs nothing passed.
   */
  env?: Record<string, string>;
  cwd?: string;
  timeoutMs?: number;
  /**
   * The logical cluster the specification lives in. A controller that owns more
   * than one workspace sets SPECD_WORKSPACE and SPECD_NAMESPACE; without them
   * every call lands on specctl's own default workspace, where the context the
   * host is working on does not exist, and a failure to apply reads as a model
   * that changed nothing. The mod passes the same two names.
   */
  workspace?: string;
  namespace?: string;
  kubeconfig?: string;
}

function scopeArgv(workspace?: string, namespace?: string, kubeconfig?: string): string[] {
  const argv: string[] = [];
  if (workspace) argv.push("--workspace", workspace);
  if (namespace) argv.push("--namespace", namespace);
  if (kubeconfig) argv.push("--kubeconfig", kubeconfig);
  return argv;
}

export function specctlBridge(options: BridgeOptions = {}): StateBridge {
  const specctl = options.specctl ?? process.env.SPECD_SPECCTL ?? "specctl";
  const runner = options.runner ?? nodeRunner();
  const env = { ...process.env, ...options.env } as Record<string, string>;
  const cwd = options.cwd;
  const timeoutMs = options.timeoutMs ?? 120_000;
  const scope = scopeArgv(
    options.workspace ?? env.SPECD_WORKSPACE,
    options.namespace ?? env.SPECD_NAMESPACE,
    options.kubeconfig ?? env.KUBECONFIG ?? env.SPECD_KUBECONFIG,
  );

  const call = async (argv: readonly string[], stdin?: string) => {
    const full = [...argv, ...scope];
    const result = await runner.run([specctl, ...full], { cwd, env, stdin, timeoutMs });
    if (result.exitCode !== 0) {
      throw new Error(`${specctl} ${full.join(" ")} exited ${result.exitCode}: ${result.stderr.trim()}`);
    }
    return result;
  };

  return {
    async render(context) {
      const result = await call(["clm", "render", "--context", context]);
      return result.stdout;
    },
    async apply(context, modelZone) {
      const result = await call(["clm", "apply", "--context", context], modelZone);
      const delta = parseJSON(result.stdout);
      return {
        delta,
        applied: result.stderr.includes("applied"),
        folded: /folded into (\S+)/.exec(result.stderr)?.[1],
      };
    },
    async report(change, event: ProgressRecord) {
      const result = await call(["clm", "report", "--change", change, "--event", JSON.stringify(event)]);
      const match = /progress=(\d+) recorded=(true|false)/.exec(result.stdout);
      return { progress: match ? Number(match[1]) : 0, recorded: match?.[2] === "true" };
    },
  };
}

function parseJSON(text: string): unknown {
  const trimmed = text.trim();
  if (!trimmed) return {};
  try {
    return JSON.parse(trimmed);
  } catch {
    return {};
  }
}
