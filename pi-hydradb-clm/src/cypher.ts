import { cypherLiteral } from "./ids.ts";

export type Literal = string | number | boolean;

export function vertexUpsert(label: string, properties: string[]): string {
  const assignments = properties.map((property) => `n.${property} = row.${property}`);
  const setClause = [`n:${label}`, ...assignments].join(", ");
  return `UNWIND $rows AS row MERGE (n {id: row.id}) SET ${setClause}`;
}

export function edgeUpsert(type: string, fromLabel: string, toLabel: string): string {
  return `UNWIND $rows AS row MATCH (a:${fromLabel} {id: row.src}), (b:${toLabel} {id: row.dst}) CREATE (a)-[:${type}]->(b)`;
}

export function vertexDelete(): string {
  return "UNWIND $rows AS row MATCH (n {id: row.id}) DETACH DELETE n";
}

function filterClause(filter: Record<string, Literal>): string {
  const entries = Object.entries(filter);
  if (entries.length === 0) return "";
  const inner = entries.map(([key, value]) => `${key}: ${cypherLiteral(value)}`).join(", ");
  return ` {${inner}}`;
}

function projection(properties: string[]): string {
  return properties.map((property) => `n.${property} AS ${property}`).join(", ");
}

export function vertexSelect(
  label: string,
  properties: string[],
  filter: Record<string, Literal> = {},
  limit?: number,
): string {
  const tail = limit === undefined ? "" : ` LIMIT ${Math.trunc(limit)}`;
  return `MATCH (n:${label}${filterClause(filter)}) RETURN ${projection(properties)}${tail}`;
}

export function vertexCount(label: string, filter: Record<string, Literal> = {}): string {
  return `MATCH (n:${label}${filterClause(filter)}) RETURN count(*) AS total`;
}

export function edgeSelect(
  type: string,
  fromLabel: string,
  toLabel: string,
  properties: string[],
  fromId: number,
  limit?: number,
): string {
  const tail = limit === undefined ? "" : ` LIMIT ${Math.trunc(limit)}`;
  const targets = properties.map((property) => `b.${property} AS ${property}`).join(", ");
  return `MATCH (a:${fromLabel} {id: ${Math.trunc(fromId)}})-[:${type}]->(b:${toLabel}) RETURN ${targets}${tail}`;
}
