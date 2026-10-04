import { isAbsolute, normalize, resolve } from "node:path";

export {
  MAX_NODE_ID,
  cypherLiteral,
  cypherString,
  stable as stableNodeId,
} from "../../clm/core/mod.ts";

export function canonicalPath(path: string, cwd: string): string {
  return isAbsolute(path) ? normalize(path) : resolve(cwd, path);
}

export function nodeKey(sessionKey: string, kind: string, localKey: string): string {
  return `${sessionKey}\u0000${kind}\u0000${localKey}`;
}
