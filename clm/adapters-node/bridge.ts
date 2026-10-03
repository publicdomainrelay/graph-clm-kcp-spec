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
}

export function specctlBridge(options: BridgeOptions = {}): StateBridge {
  const specctl = options.specctl ?? process.env.SPECD_SPECCTL ?? "specctl";
  const runner = options.runner ?? nodeRunner();
  const env = { ...process.env, ...options.env } as Record<string, string>;
  const cwd = options.cwd;
  const timeoutMs = options.timeoutMs ?? 120_000;

  const call = async (argv: readonly string[], stdin?: string) => {
    const result = await runner.run([specctl, ...argv], { cwd, env, stdin, timeoutMs });
    if (result.exitCode !== 0) {
      throw new Error(`${specctl} ${argv.join(" ")} exited ${result.exitCode}: ${result.stderr.trim()}`);
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
