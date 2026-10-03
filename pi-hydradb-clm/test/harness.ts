import { spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import {
  createAgentSession,
  DefaultResourceLoader,
  getAgentDir,
  ModelRuntime,
  SessionManager,
} from "@earendil-works/pi-coding-agent";
import { BACKEND_DEFAULTS, parseBackend, type GraphBackend } from "../src/backend.ts";
import { GraphClient } from "../src/graph.ts";

export const EXTENSION_PATH = resolve(import.meta.dirname, "..", "index.ts");
export const DEEPSEEK_CLAUDE = join(
  process.env.HOME ?? "/root",
  ".local",
  "bin",
  "deepseek-claude",
);

export interface GraphHandle {
  backend: GraphBackend;
  boltUrl: string;
  user: string;
  token: string;
  database?: string;
  spawned: ChildProcess | null;
  stop(): void;
}

let handle: GraphHandle | null = null;

function deepseekKey(): string {
  if (process.env.DEEPSEEK_API_KEY) return process.env.DEEPSEEK_API_KEY;
  const script = readFileSync(DEEPSEEK_CLAUDE, "utf8");
  const match = script.match(/ANTHROPIC_API_KEY="([^"]+)"/);
  if (!match?.[1]) throw new Error(`no API key found in ${DEEPSEEK_CLAUDE}`);
  return match[1];
}

async function reachable(boltUrl: string, user: string, token: string, database?: string): Promise<boolean> {
  try {
    const graph = await GraphClient.connect({ boltUrl, user, token, database });
    await graph.run(`MATCH (n:ClmProbe) RETURN count(*) AS total`).catch(() => {});
    await graph.close();
    return true;
  } catch {
    return false;
  }
}

function portOf(boltUrl: string): number {
  const match = boltUrl.match(/:(\d+)/);
  return match?.[1] ? Number(match[1]) : 7687;
}

function hydradbProcess(boltUrl: string, httpPort: number, adminPort: number) {
  const nodeBin = process.env.HYDRA_NODE_BIN ?? "/tmp/hydradb-bin/graph-node";
  const libDir = process.env.HYDRA_LIB_PATH ?? join(dirname(nodeBin), "lib");
  const root = mkdtempSync(join(tmpdir(), "hydradb-clm-"));
  const dataPath = join(root, "data");
  const cachePath = join(root, "cache");
  const slatePath = join(root, "slate");
  for (const path of [dataPath, cachePath, slatePath]) mkdirSync(path, { recursive: true });

  const tokenFile = join(root, "token");
  const token = Buffer.from(crypto.getRandomValues(new Uint8Array(36))).toString("base64");
  writeFileSync(tokenFile, `${token}\n`, { mode: 0o600 });

  const host = boltUrl.replace(/^bolt:\/\//, "").replace(/:\d+$/, "");
  const child = spawn(nodeBin, [], {
    env: {
      ...process.env,
      LD_LIBRARY_PATH: libDir,
      GRAPH_ALLOW_PLAINTEXT: "true",
      GRAPH_AUTH_TOKEN_FILE: tokenFile,
      GRAPH_DATA_PATH: dataPath,
      GRAPH_DATA_CACHE_DIR: cachePath,
      GRAPH_HTTP_ADDR: `${host}:${httpPort}`,
      GRAPH_BOLT_ADDR: `${host}:${portOf(boltUrl)}`,
      GRAPH_ADMIN_ADDR: `${host}:${adminPort}`,
      GRAPH_LOG_FORMAT: "text",
      CLOUD_PROVIDER: "local",
      LOCAL_PATH: slatePath,
    },
    stdio: "ignore",
  });

  return { child, token, tokenFile };
}

function arcadedbProcess(boltUrl: string, httpPort: number) {
  const home = process.env.HYDRA_ARCADEDB_HOME ?? "/tmp/arcadedb/arcadedb-26.9.1";
  const root = mkdtempSync(join(tmpdir(), "arcadedb-clm-"));
  const token = process.env.HYDRA_ARCADEDB_PASSWORD ?? "clm-arcadedb-root";
  const database = process.env.HYDRA_DATABASE ?? BACKEND_DEFAULTS.arcadedb.database ?? "clm";

  const child = spawn(join(home, "bin", "server.sh"), [], {
    cwd: root,
    env: {
      ...process.env,
      ARCADEDB_HOME: home,
      ARCADEDB_JMX: " ",
      ARCADEDB_OPTS_MEMORY: "-Xms256M -Xmx1G",
      JAVA_OPTS: [
        `-Darcadedb.bolt.port=${portOf(boltUrl)}`,
        `-Darcadedb.bolt.host=${boltUrl.replace(/^bolt:\/\//, "").replace(/:\d+$/, "")}`,
        `-Darcadedb.server.httpPort=${httpPort}`,
        `-Darcadedb.server.rootPassword=${token}`,
        `-Darcadedb.server.defaultDatabases=${database}[root]`,
        "-Darcadedb.server.plugins=Bolt:com.arcadedb.bolt.BoltProtocolPlugin",
      ].join(" "),
    },
    stdio: "ignore",
  });

  return { child, token, database };
}

export async function ensureGraph(): Promise<GraphHandle> {
  if (handle) return handle;
  const backend = parseBackend(process.env.HYDRA_BACKEND);
  const defaults = BACKEND_DEFAULTS[backend];

  const boltUrl = process.env.HYDRA_BOLT_URL ?? defaults.boltUrl;
  const user = process.env.HYDRA_USER ?? defaults.user;
  const database = process.env.HYDRA_DATABASE ?? defaults.database;
  const envToken =
    process.env[defaults.tokenEnv] ??
    (process.env.HYDRA_TOKEN_FILE && defaults.tokenFile
      ? readFileSync(process.env.HYDRA_TOKEN_FILE, "utf8").trim()
      : undefined);

  if (envToken && (await reachable(boltUrl, user, envToken, database))) {
    handle = { backend, boltUrl, user, token: envToken, database, spawned: null, stop() {} };
    return handle;
  }

  const boltPort = portOf(boltUrl);
  const httpPort = boltPort === 7688 ? 2491 : 7475;
  const started =
    backend === "hydradb"
      ? hydradbProcess(boltUrl, httpPort, boltPort === 7687 ? 9090 : 9091)
      : arcadedbProcess(boltUrl, httpPort);

  const token = started.token;
  const spawnedDatabase = backend === "arcadedb" ? (started as { database?: string }).database : undefined;
  const resolvedDatabase = database ?? spawnedDatabase;

  for (let attempt = 0; attempt < 120; attempt++) {
    if (await reachable(boltUrl, user, token, resolvedDatabase)) {
      process.env.HYDRA_BACKEND = backend;
      process.env.HYDRA_BOLT_URL = boltUrl;
      process.env.HYDRA_USER = user;
      process.env.HYDRA_TOKEN = token;
      if (resolvedDatabase) process.env.HYDRA_DATABASE = resolvedDatabase;
      handle = {
        backend,
        boltUrl,
        user,
        token,
        database: resolvedDatabase,
        spawned: started.child,
        stop() {
          started.child.kill("SIGKILL");
        },
      };
      return handle;
    }
    await new Promise((done) => setTimeout(done, 500));
  }

  started.child.kill("SIGKILL");
  throw new Error(`${backend} did not become reachable at ${boltUrl}`);
}

export async function openGraph(): Promise<GraphClient> {
  const target = await ensureGraph();
  return await GraphClient.connect({
    boltUrl: target.boltUrl,
    user: target.user,
    token: target.token,
    database: target.database,
  });
}

export interface HarnessSession {
  session: Awaited<ReturnType<typeof createAgentSession>>["session"];
  captured: string[];
  dispose(): void;
}

export async function makeSession(scratch: string): Promise<HarnessSession> {
  await ensureGraph();
  process.env.PI_CODING_AGENT_DIR = join(scratch, "agent-dir");
  mkdirSync(process.env.PI_CODING_AGENT_DIR, { recursive: true });

  const runtime = await ModelRuntime.create();
  await runtime.setRuntimeApiKey("deepseek", deepseekKey());
  const model = runtime.getModel(
    process.env.HYDRA_PI_PROVIDER ?? "deepseek",
    process.env.HYDRA_PI_MODEL ?? "deepseek-flash",
  );
  if (!model) throw new Error("deepseek model not available in pi model registry");

  const captured: string[] = [];
  const resourceLoader = new DefaultResourceLoader({
    cwd: scratch,
    agentDir: getAgentDir(),
    additionalExtensionPaths: [EXTENSION_PATH],
    extensionFactories: [
      (pi) => {
        pi.on("before_provider_request", (event) => {
          captured.push(JSON.stringify(event.payload));
        });
      },
    ],
  });
  await resourceLoader.reload();

  const { session } = await createAgentSession({
    cwd: scratch,
    model,
    thinkingLevel: "low",
    modelRuntime: runtime,
    resourceLoader,
    sessionManager: SessionManager.inMemory(),
  });
  await session.bindExtensions({});

  return {
    session,
    captured,
    dispose: () => session.dispose(),
  };
}

export function makeScratch(): string {
  return mkdtempSync(join(tmpdir(), "pi-hydradb-clm-"));
}

export function uniqueSessionKey(label: string): string {
  const random = Math.random().toString(36).slice(2, 10);
  return `${label}-${Date.now().toString(36)}-${random}`;
}
