import { isAbsolute, normalize, resolve } from "node:path";

export const MAX_NODE_ID = 0x1f_ffff_ffff_ffff;

export function canonicalPath(path: string, cwd: string): string {
  return isAbsolute(path) ? normalize(path) : resolve(cwd, path);
}

export function stableNodeId(key: string): number {
  let hash = 0xcbf29ce484222325n;
  const prime = 0x100000001b3n;
  for (let index = 0; index < key.length; index++) {
    hash ^= BigInt(key.charCodeAt(index));
    hash = (hash * prime) & 0xffffffffffffffffn;
  }
  return Number(hash & BigInt(MAX_NODE_ID));
}

export function cypherString(value: string): string {
  const escaped = value
    .replace(/\\/g, "\\\\")
    .replace(/'/g, "\\'")
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r");
  return `'${escaped}'`;
}

export function cypherLiteral(value: string | number | boolean): string {
  if (typeof value === "string") return cypherString(value);
  if (typeof value === "boolean") return String(value);
  if (!Number.isFinite(value)) throw new Error(`non-finite numeric literal: ${value}`);
  return String(value);
}

export function nodeKey(sessionKey: string, kind: string, localKey: string): string {
  return `${sessionKey}\u0000${kind}\u0000${localKey}`;
}
