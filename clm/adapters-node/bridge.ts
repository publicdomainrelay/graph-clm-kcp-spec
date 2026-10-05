import type { ProgressRecord, StateBridge } from "../core/mod.ts";
import { nodeRunner } from "./runner.ts";
import type { Runner } from "../core/mod.ts";

export interface BridgeOptions {
  specctl?: string;
  runner?: Runner;
  env?: Record<string, string>;
  cwd?: string;
  timeoutMs?: number;
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
        queued: /queued behind (\S+)/.exec(result.stderr)?.[1],
        summary: deltaSummaryLines(result.stderr),
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

// The apply prints its delta by id on stderr before it lands, one line per
// added, changed and removed entry; the host relays those lines so a model or
// operator sees what the edit does rather than only its counts.
export function deltaSummaryLines(stderr: string): string {
  return stderr
    .split("\n")
    .map((line) => line.trimEnd())
    .filter((line) => /^[+~-] /.test(line))
    .join("\n");
}
