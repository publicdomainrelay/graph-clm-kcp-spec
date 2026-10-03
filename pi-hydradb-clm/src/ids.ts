import { isAbsolute, normalize, resolve } from "node:path";

// The hash and the Cypher literals are shared with every other CLM host; the
// path helpers are pi's own, because only a Node host has node:path.
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
