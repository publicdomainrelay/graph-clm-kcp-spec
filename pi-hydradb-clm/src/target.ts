import { readFileSync } from "node:fs";
import { BACKEND_DEFAULTS, parseBackend, type GraphBackend } from "./backend.ts";

export interface GraphTarget {
  backend: GraphBackend;
  boltUrl: string;
  user: string;
  token: string;
  database?: string;
}

export interface PartialTarget {
  backend: GraphBackend;
  boltUrl: string;
  user: string;
  token?: string;
  tokenFile?: string;
  database?: string;
}

export function resolvePartialTarget(env: NodeJS.ProcessEnv = process.env): PartialTarget {
  const backend = parseBackend(env.HYDRA_BACKEND);
  const defaults = BACKEND_DEFAULTS[backend];
  return {
    backend,
    boltUrl: env.HYDRA_BOLT_URL ?? defaults.boltUrl,
    user: env.HYDRA_USER ?? defaults.user,
    token: env[defaults.tokenEnv],
    tokenFile: env.HYDRA_TOKEN_FILE ?? defaults.tokenFile,
    database: env.HYDRA_DATABASE ?? defaults.database,
  };
}

export function resolveGraphTarget(env: NodeJS.ProcessEnv = process.env): GraphTarget {
  const partial = resolvePartialTarget(env);
  if (partial.token) return { ...partial, token: partial.token.trim() };
  if (partial.tokenFile) {
    return { ...partial, token: readFileSync(partial.tokenFile, "utf8").trim() };
  }
  throw new Error(
    `${partial.backend} backend needs HYDRA_TOKEN or HYDRA_TOKEN_FILE to authenticate`,
  );
}
