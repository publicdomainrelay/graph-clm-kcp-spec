// The pi host's door into the shared state bridge. When the controller starts
// pi inside a change it sets SPECD_CLM_CONTEXT and SPECD_CLM_CHANGE; the
// extension then renders the same context document the Claude Code mod renders,
// and reports what the tools touched into the same kcp record. Nothing here
// talks to kcp or Bolt directly: `specctl clm` does, in Go.

import { ClmHost, contextFromEnv, changeFromEnv } from "../../clm/core/mod.ts";
import { nodeFileStore, specctlBridge } from "../../clm/adapters-node/mod.ts";

export interface PiHostOptions {
  repoPath?: string;
  env?: NodeJS.ProcessEnv;
}

/**
 * The host the controller put this process inside, or undefined when pi is
 * running on its own: an unset SPECD_CLM_CONTEXT means there is no change to
 * report into and the extension keeps its own session-scoped context.
 */
export function clmHostFromEnv(options: PiHostOptions = {}): ClmHost | undefined {
  const env = options.env ?? process.env;
  const context = contextFromEnv(env);
  if (!context) return undefined;
  return new ClmHost({
    bridge: specctlBridge({ env: env as Record<string, string> }),
    files: nodeFileStore(),
    repoPath: options.repoPath ?? env.SPECD_CLM_REPO ?? process.cwd(),
    context,
    change: changeFromEnv(env),
  });
}
