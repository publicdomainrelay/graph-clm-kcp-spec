export const MAX_NODE_ID = 0x1f_ffff_ffff_ffff;

export function stable(value: string): number {
  let hash = 0xcbf29ce484222325n;
  const prime = 0x100000001b3n;
  for (let index = 0; index < value.length; index++) {
    hash ^= BigInt(value.charCodeAt(index));
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
  return String(Math.trunc(value));
}

export function repoId(name: string): number {
  return stable(`repo:${name}`);
}

export function contextId(name: string): number {
  return stable(`context:${name}`);
}

export function requirementId(context: string, id: string): number {
  return stable(`requirement:${context}/${id}`);
}

export function interfaceId(context: string, name: string): number {
  return stable(`interface:${context}/${name}`);
}

export function codeRefId(codegraphId: string): number {
  return stable(`coderef:${codegraphId}`);
}

export function fileCodeRefId(path: string): number {
  return codeRefId(`file:${path}`);
}

export function changeId(name: string): number {
  return stable(`change:${name}`);
}

export function progressId(change: string, index: number): number {
  return stable(`progress:${change}:${index}`);
}

export function piMemoryId(title: string): number {
  return stable(`pimemory:${title}`);
}
