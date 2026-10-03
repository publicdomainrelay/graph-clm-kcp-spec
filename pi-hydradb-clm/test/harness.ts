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
import { HydraGraph } from "../src/graph.ts";

export const EXTENSION_PATH = resolve(import.meta.dirname, "..", "index.ts");
export const DEEPSEEK_CLAUDE = join(
  process.env.HOME ?? "/root",
  ".local",
  "bin",
  "deepseek-claude",
);

export interface HydraHandle {
  boltUrl: string;
  tokenFile: string;
  spawned: ChildProcess | null;
  stop(): void;
}

let handle: HydraHandle | null = null;

function deepseekKey(): string {
  if (process.env.DEEPSEEK_API_KEY) return process.env.DEEPSEEK_API_KEY;
  const script = readFileSync(DEEPSEEK_CLAUDE, "utf8");
  const match = script.match(/ANTHROPIC_API_KEY="([^"]+)"/);
  if (!match?.[1]) throw new Error(`no API key found in ${DEEPSEEK_CLAUDE}`);
  return match[1];
}

async function reachable(boltUrl: string, token: string): Promise<boolean> {
  try {
    const graph = await HydraGraph.connect({ boltUrl, user: "neo4j", token });
    await graph.close();
    return true;
  } catch {
    return false;
  }
}

export async function ensureHydra(): Promise<HydraHandle> {
  if (handle) return handle;

  const boltUrl = process.env.HYDRA_BOLT_URL ?? "bolt://127.0.0.1:7687";
  let tokenFile = process.env.HYDRA_TOKEN_FILE ?? "";
  let token = process.env.HYDRA_TOKEN ?? "";

  if (!token && tokenFile) token = readFileSync(tokenFile, "utf8").trim();

  if (token && (await reachable(boltUrl, token))) {
    handle = { boltUrl, tokenFile, spawned: null, stop() {} };
    return handle;
  }

  const nodeBin = process.env.HYDRA_NODE_BIN ?? "/tmp/hydradb-bin/graph-node";
  const libDir = process.env.HYDRA_LIB_PATH ?? join(dirname(nodeBin), "lib");
  const root = mkdtempSync(join(tmpdir(), "hydradb-clm-"));
  const dataPath = join(root, "data");
  const cachePath = join(root, "cache");
  const slatePath = join(root, "slate");
  for (const path of [dataPath, cachePath, slatePath]) mkdirSync(path, { recursive: true });

  tokenFile = join(root, "token");
  token = Buffer.from(crypto.getRandomValues(new Uint8Array(36))).toString("base64");
  writeFileSync(tokenFile, `${token}\n`, { mode: 0o600 });

  const child = spawn(nodeBin, [], {
    env: {
      ...process.env,
      LD_LIBRARY_PATH: libDir,
      GRAPH_ALLOW_PLAINTEXT: "true",
      GRAPH_AUTH_TOKEN_FILE: tokenFile,
      GRAPH_DATA_PATH: dataPath,
      GRAPH_DATA_CACHE_DIR: cachePath,
      GRAPH_HTTP_ADDR: "127.0.0.1:7474",
      GRAPH_BOLT_ADDR: "127.0.0.1:7687",
      GRAPH_ADMIN_ADDR: "127.0.0.1:9090",
      GRAPH_LOG_FORMAT: "text",
      CLOUD_PROVIDER: "local",
      LOCAL_PATH: slatePath,
    },
    stdio: "ignore",
  });

  for (let attempt = 0; attempt < 60; attempt++) {
    if (await reachable(boltUrl, token)) {
      process.env.HYDRA_BOLT_URL = boltUrl;
      process.env.HYDRA_TOKEN_FILE = tokenFile;
      process.env.HYDRA_TOKEN = token;
      handle = {
        boltUrl,
        tokenFile,
        spawned: child,
        stop() {
          child.kill("SIGKILL");
        },
      };
      return handle;
    }
    await new Promise((done) => setTimeout(done, 500));
  }

  child.kill("SIGKILL");
  throw new Error(`hydradb did not become reachable at ${boltUrl}`);
}

export async function openGraph(): Promise<HydraGraph> {
  await ensureHydra();
  return await HydraGraph.connect({
    boltUrl: process.env.HYDRA_BOLT_URL ?? "bolt://127.0.0.1:7687",
    user: process.env.HYDRA_USER ?? "neo4j",
    token: process.env.HYDRA_TOKEN ?? readFileSync(process.env.HYDRA_TOKEN_FILE ?? "", "utf8").trim(),
  });
}

export interface HarnessSession {
  session: Awaited<ReturnType<typeof createAgentSession>>["session"];
  captured: string[];
  dispose(): void;
}

export async function makeSession(scratch: string): Promise<HarnessSession> {
  await ensureHydra();
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
