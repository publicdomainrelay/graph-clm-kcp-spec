import { cypherLiteral, changeId, codeRefId, progressId } from "./ids.ts";
import type { CodeRefRecord } from "./context-doc.ts";
import type { ProgressRecord, SpecChange } from "./types.ts";

export type Literal = string | number | boolean;

export interface VertexRow {
  id: number;
  [property: string]: Literal;
}

export interface EdgeRow {
  src: number;
  dst: number;
}

export interface VertexSet {
  label: string;
  properties: string[];
  rows: VertexRow[];
}

export interface EdgeSet {
  type: string;
  fromLabel: string;
  toLabel: string;
  rows: EdgeRow[];
}

export const LABELS = {
  repo: "SpecRepo",
  context: "SpecContext",
  requirement: "SpecRequirement",
  declaredInterface: "SpecInterface",
  codeRef: "CodeRef",
  change: "SpecChange",
  progress: "SpecProgress",
  piMemory: "PiMemory",
} as const;

export const EDGES = {
  hasContext: "HAS_CONTEXT",
  requires: "REQUIRES",
  declares: "DECLARES",
  references: "REFERENCES",
  upstream: "UPSTREAM",
  overlay: "OVERLAY",
  orchestrator: "ORCHESTRATOR",
  dependsOn: "DEPENDS_ON",
  introduces: "INTRODUCES",
  touched: "TOUCHED",
  occurred: "OCCURRED",
  specifies: "SPECIFIES",
} as const;

export const VERTEX_PROPERTIES: Record<string, string[]> = {
  [LABELS.repo]: ["name", "path"],
  [LABELS.context]: ["name", "repo", "intent", "specHash"],
  [LABELS.requirement]: ["context", "reqId", "level", "text"],
  [LABELS.declaredInterface]: ["context", "name", "kind", "signature"],
  [LABELS.codeRef]: ["codegraphId", "kind", "name", "filePath"],
  [LABELS.change]: ["name", "context", "direction", "phase"],
  [LABELS.progress]: ["change", "turn", "tool", "note", "at"],
  [LABELS.piMemory]: ["title", "body", "session"],
};

export function vertexUpsert(label: string, properties: readonly string[]): string {
  const assignments = properties.map((property) => `n.${property} = row.${property}`);
  return `UNWIND $rows AS row MERGE (n {id: row.id}) SET ${[`n:${label}`, ...assignments].join(", ")}`;
}

export function edgeCreate(type: string, fromLabel: string, toLabel: string): string {
  return `UNWIND $rows AS row MATCH (a:${fromLabel} {id: row.src}), (b:${toLabel} {id: row.dst}) CREATE (a)-[:${type}]->(b)`;
}

export function vertexDelete(): string {
  return "UNWIND $rows AS row MATCH (n {id: row.id}) DETACH DELETE n";
}

export function vertexIds(label: string): string {
  return `MATCH (n:${label}) RETURN n.id AS id`;
}

export function vertexSelect(label: string, properties: readonly string[], filter: Record<string, Literal> = {}): string {
  return `MATCH (n:${label}${filterClause(filter)}) RETURN ${projection("n", properties)}`;
}

export function edgeSelectOut(
  type: string,
  fromLabel: string,
  toLabel: string,
  fromId: number,
  properties: readonly string[],
): string {
  return `MATCH (a:${fromLabel} {id: ${cypherLiteral(fromId)}})-[:${type}]->(b:${toLabel}) RETURN ${projection("b", properties)}`;
}

export function edgeSelectIn(
  type: string,
  fromLabel: string,
  toLabel: string,
  toId: number,
  properties: readonly string[],
): string {
  return `MATCH (a:${fromLabel})-[:${type}]->(b:${toLabel} {id: ${cypherLiteral(toId)}}) RETURN ${projection("a", properties)}`;
}

export function fileCodeRefRow(path: string): VertexRow {
  const cut = path.lastIndexOf("/");
  return {
    id: codeRefId(`file:${path}`),
    codegraphId: `file:${path}`,
    kind: "file",
    name: cut === -1 ? path : path.slice(cut + 1),
    filePath: path,
  };
}

export function codeRefRow(ref: CodeRefRecord): VertexRow {
  return {
    id: codeRefId(ref.codegraphId),
    codegraphId: ref.codegraphId,
    kind: ref.kind,
    name: ref.name,
    filePath: ref.filePath,
  };
}

export function changeRow(change: SpecChange): VertexRow {
  return {
    id: changeId(change.metadata.name),
    name: change.metadata.name,
    context: change.spec.systemContext,
    direction: change.spec.direction,
    phase: change.status?.phase ?? "",
  };
}

export function progressRow(change: string, index: number, record: ProgressRecord): VertexRow {
  return {
    id: progressId(change, index),
    change,
    turn: record.turn ?? 0,
    tool: record.tool ?? "",
    note: record.note ?? "",
    at: record.at ?? "",
  };
}

export function liveRows(change: SpecChange, record: ProgressRecord, index: number): { vertices: VertexSet[]; edges: EdgeSet[] } {
  const vertices: VertexSet[] = [
    { label: LABELS.change, properties: VERTEX_PROPERTIES[LABELS.change] as string[], rows: [changeRow(change)] },
  ];
  const edges: EdgeSet[] = [
    { type: EDGES.touched, fromLabel: LABELS.change, toLabel: LABELS.codeRef, rows: [] },
    { type: EDGES.occurred, fromLabel: LABELS.change, toLabel: LABELS.progress, rows: [] },
  ];
  const touched = edges[0] as EdgeSet;
  const occurred = edges[1] as EdgeSet;
  const files = record.files ?? [];
  if (files.length) {
    vertices.push({
      label: LABELS.codeRef,
      properties: VERTEX_PROPERTIES[LABELS.codeRef] as string[],
      rows: files.map(fileCodeRefRow),
    });
    for (const file of files) touched.rows.push({ src: changeId(change.metadata.name), dst: codeRefId(`file:${file}`) });
  }
  if (record.tool || record.note || record.turn) {
    vertices.push({
      label: LABELS.progress,
      properties: VERTEX_PROPERTIES[LABELS.progress] as string[],
      rows: [progressRow(change.metadata.name, index, record)],
    });
    occurred.rows.push({
      src: changeId(change.metadata.name),
      dst: progressId(change.metadata.name, index),
    });
  }
  return { vertices, edges: edges.filter((set) => set.rows.length > 0) };
}

function filterClause(filter: Record<string, Literal>): string {
  const entries = Object.entries(filter);
  if (!entries.length) return "";
  const inner = entries.map(([key, value]) => `${key}: ${cypherLiteral(value)}`).join(", ");
  return ` {${inner}}`;
}

function projection(binding: string, properties: readonly string[]): string {
  return properties.map((property) => `${binding}.${property} AS ${property}`).join(", ");
}
