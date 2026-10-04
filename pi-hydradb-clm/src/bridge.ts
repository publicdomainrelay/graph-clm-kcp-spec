import { homedir } from "node:os";

import { ClmHost, contextFromEnv, changeFromEnv, docPathFromEnv } from "../../clm/core/mod.ts";
import { nodeFileStore, specctlBridge } from "../../clm/adapters-node/mod.ts";

export interface PiHostOptions {
  repoPath?: string;
  env?: NodeJS.ProcessEnv;
}

export function clmHostFromEnv(options: PiHostOptions = {}): ClmHost | undefined {
  const env = options.env ?? process.env;
  const context = contextFromEnv(env);
  if (!context) return undefined;
  const repoPath = options.repoPath ?? env.SPECD_CLM_REPO ?? process.cwd();
  return new ClmHost({
    bridge: specctlBridge({ env: env as Record<string, string> }),
    files: nodeFileStore(),
    repoPath,
    docPath: docPathFromEnv(env, homedir(), repoPath, context),
    context,
    change: changeFromEnv(env),
  });
}
