export type GraphBackend = "hydradb" | "arcadedb";

export const DEFAULT_BACKEND: GraphBackend = "arcadedb";

export interface BackendDefaults {
  boltUrl: string;
  user: string;
  database?: string;
  tokenEnv: string;
  token?: string;
  tokenFile?: string;
}

export const BACKEND_DEFAULTS: Record<GraphBackend, BackendDefaults> = {
  hydradb: {
    boltUrl: "bolt://127.0.0.1:7687",
    user: "neo4j",
    tokenEnv: "GRAPH_TOKEN",
    tokenFile: "/var/run/secrets/slatedb-graph/auth-token",
  },
  arcadedb: {
    boltUrl: "bolt://127.0.0.1:7688",
    user: "root",
    database: "clm",
    tokenEnv: "GRAPH_TOKEN",
    token: "clm-arcadedb-root",
  },
};

export function parseBackend(value: string | undefined): GraphBackend {
  if (value === undefined || value.trim() === "") return DEFAULT_BACKEND;
  const normalized = value.trim().toLowerCase();
  if (normalized === "hydradb" || normalized === "arcadedb") return normalized;
  throw new Error(`GRAPH_BACKEND (HYDRA_BACKEND) must be hydradb or arcadedb, got ${value}`);
}
