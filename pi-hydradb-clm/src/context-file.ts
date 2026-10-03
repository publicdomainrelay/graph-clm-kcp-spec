import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  CONTEXT_FILE_NAME,
  composeContextFile,
  splitContextFile,
  type CodeRefRecord,
} from "./context-doc.ts";

export function contextFilePath(sessionKey: string, env: NodeJS.ProcessEnv = process.env): string {
  const override = env.HYDRA_CLM_CONTEXT_PATH;
  if (override) return override;
  const dir = join(tmpdir(), "pi-hydradb-clm", sessionKey);
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  return join(dir, CONTEXT_FILE_NAME);
}

export function readContextFile(path: string): { model: string; managed: string } {
  try {
    return splitContextFile(readFileSync(path, "utf8"));
  } catch {
    return { model: "", managed: "" };
  }
}

export function writeContextFile(
  path: string,
  model: string,
  references: CodeRefRecord[],
  sessionKey: string,
  revision: number,
  budgetTokens?: number,
  unresolved: string[] = [],
): void {
  writeFileSync(
    path,
    composeContextFile(model, references, sessionKey, revision, budgetTokens, unresolved),
    { mode: 0o600 },
  );
}

export function ensureContextFile(
  path: string,
  sessionKey: string,
  revision: number,
  budgetTokens?: number,
): void {
  if (existsSync(path)) return;
  writeContextFile(path, "", [], sessionKey, revision, budgetTokens);
}
