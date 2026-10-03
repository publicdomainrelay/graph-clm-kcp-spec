// The Cypher the portable core subset allows. The write statements come from
// clm/core, so pi writes the same rows as the Go writer and the Claude Code
// mod, on the same ids; the reads below carry a limit pi's own tools ask for,
// which the shared builders do not need.

import { cypherLiteral, type Literal } from "../../clm/core/mod.ts";

export {
  edgeCreate as edgeUpsert,
  vertexDelete,
  vertexUpsert,
  type Literal,
} from "../../clm/core/mod.ts";

export function vertexSelect(
  label: string,
  properties: string[],
  filter: Record<string, Literal> = {},
  limit?: number,
): string {
  const tail = limit === undefined ? "" : ` LIMIT ${Math.trunc(limit)}`;
  return `MATCH (n:${label}${filterClause(filter)}) RETURN ${projection("n", properties)}${tail}`;
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
  return `MATCH (a:${fromLabel} {id: ${Math.trunc(fromId)}})-[:${type}]->(b:${toLabel}) RETURN ${projection("b", properties)}${tail}`;
}

function filterClause(filter: Record<string, Literal>): string {
  const entries = Object.entries(filter);
  if (entries.length === 0) return "";
  const inner = entries.map(([key, value]) => `${key}: ${cypherLiteral(value)}`).join(", ");
  return ` {${inner}}`;
}

function projection(binding: string, properties: string[]): string {
  return properties.map((property) => `${binding}.${property} AS ${property}`).join(", ");
}
