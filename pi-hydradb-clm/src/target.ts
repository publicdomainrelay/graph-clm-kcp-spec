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

export function envValue(env: NodeJS.ProcessEnv, ...names: string[]): string | undefined {
  for (const name of names) {
    const value = env[name];
    if (value !== undefined && value !== "") return value;
  }
  return undefined;
}

export function resolvePartialTarget(env: NodeJS.ProcessEnv = process.env): PartialTarget {
  const backend = parseBackend(envValue(env, "GRAPH_BACKEND", "HYDRA_BACKEND"));
  const defaults = BACKEND_DEFAULTS[backend];
  return {
    backend,
    boltUrl: envValue(env, "GRAPH_BOLT_URL", "HYDRA_BOLT_URL") ?? defaults.boltUrl,
    user: envValue(env, "GRAPH_USER", "HYDRA_USER") ?? defaults.user,
    token: envValue(env, defaults.tokenEnv, "HYDRA_TOKEN") ?? defaults.token,
    tokenFile: envValue(env, "GRAPH_TOKEN_FILE", "HYDRA_TOKEN_FILE") ?? defaults.tokenFile,
    database: envValue(env, "GRAPH_DATABASE", "HYDRA_DATABASE") ?? defaults.database,
  };
}

export function resolveGraphTarget(env: NodeJS.ProcessEnv = process.env): GraphTarget {
  const partial = resolvePartialTarget(env);
  if (partial.token) return { ...partial, token: partial.token.trim() };
  if (partial.tokenFile) {
    return { ...partial, token: readFileSync(partial.tokenFile, "utf8").trim() };
  }
  throw new Error(
    `${partial.backend} backend needs GRAPH_TOKEN or GRAPH_TOKEN_FILE (HYDRA_TOKEN, HYDRA_TOKEN_FILE) to authenticate`,
  );
}
