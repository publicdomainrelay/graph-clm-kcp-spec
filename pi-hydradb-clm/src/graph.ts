import neo4j from "neo4j-driver";
import {
  edgeSelect,
  edgeUpsert,
  vertexCount,
  vertexDelete,
  vertexSelect,
  vertexUpsert,
  type Literal,
} from "./cypher.ts";

export interface GraphClientOptions {
  boltUrl: string;
  user: string;
  token: string;
  database?: string;
}

type Row = Record<string, unknown>;

interface IntegerLike {
  low: number;
  high: number;
  toNumber?: () => number;
}

function isIntegerLike(value: unknown): value is IntegerLike {
  return (
    typeof value === "object" &&
    value !== null &&
    "low" in value &&
    "high" in value &&
    typeof (value as IntegerLike).low === "number"
  );
}

export function toPlain(value: unknown): unknown {
  if (value === null || value === undefined) return value;
  if (isIntegerLike(value)) {
    return typeof value.toNumber === "function" ? value.toNumber() : value.low;
  }
  if (Array.isArray(value)) return value.map(toPlain);
  if (typeof value === "object") {
    const out: Row = {};
    for (const [key, inner] of Object.entries(value as Row)) out[key] = toPlain(inner);
    return out;
  }
  return value;
}

function toParam(value: unknown): unknown {
  if (typeof value === "number" && Number.isInteger(value)) return neo4j.int(value);
  return value;
}

function toParams(rows: Row[]): Row[] {
  return rows.map((row) => {
    const out: Row = {};
    for (const [key, value] of Object.entries(row)) out[key] = toParam(value);
    return out;
  });
}

function toDeepParam(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(toDeepParam);
  if (value !== null && typeof value === "object") {
    const out: Row = {};
    for (const [key, inner] of Object.entries(value as Row)) out[key] = toDeepParam(inner);
    return out;
  }
  return toParam(value);
}

export function normalizeParams(params: Record<string, unknown>): Record<string, unknown> {
  return toDeepParam(params) as Record<string, unknown>;
}

export class GraphClient {
  private constructor(
    private readonly driver: neo4j.Driver,
    private readonly database: string | undefined,
  ) {}

  static async connect(options: GraphClientOptions, attempts = 3): Promise<GraphClient> {
    let lastError: unknown;
    for (let attempt = 0; attempt < attempts; attempt++) {
      const driver = neo4j.driver(
        options.boltUrl,
        neo4j.auth.basic(options.user, options.token),
      );
      try {
        await driver.verifyConnectivity();
        return new GraphClient(driver, options.database);
      } catch (error) {
        lastError = error;
        await driver.close().catch(() => {});
      }
    }
    throw lastError;
  }

  private async records(query: string, params: Row = {}): Promise<Row[]> {
    const session = this.driver.session(
      this.database ? { database: this.database } : {},
    );
    try {
      const result = await session.run(query, params);
      return result.records.map((record) => toPlain(record.toObject()) as Row);
    } finally {
      await session.close();
    }
  }

  async run(query: string, params: Record<string, unknown> = {}): Promise<Row[]> {
    return await this.records(query, normalizeParams(params));
  }

  async upsertVertices(label: string, rows: Row[], properties: string[]): Promise<void> {
    if (rows.length === 0) return;
    await this.records(vertexUpsert(label, properties), { rows: toParams(rows) });
  }

  async upsertEdges(
    type: string,
    fromLabel: string,
    toLabel: string,
    rows: Row[],
  ): Promise<void> {
    if (rows.length === 0) return;
    await this.records(edgeUpsert(type, fromLabel, toLabel), { rows: toParams(rows) });
  }

  async deleteVertices(ids: number[]): Promise<void> {
    if (ids.length === 0) return;
    await this.records(vertexDelete(), { rows: toParams(ids.map((id) => ({ id }))) });
  }

  async selectVertices(
    label: string,
    properties: string[],
    filter: Record<string, Literal> = {},
    limit?: number,
  ): Promise<Row[]> {
    return await this.records(vertexSelect(label, properties, filter, limit));
  }

  async countVertices(label: string, filter: Record<string, Literal> = {}): Promise<number> {
    const [row] = await this.records(vertexCount(label, filter));
    return Number(row?.total ?? 0);
  }

  async selectNeighbors(
    type: string,
    fromLabel: string,
    toLabel: string,
    properties: string[],
    fromId: number,
    limit?: number,
  ): Promise<Row[]> {
    return await this.records(
      edgeSelect(type, fromLabel, toLabel, properties, fromId, limit),
    );
  }

  async close(): Promise<void> {
    await this.driver.close();
  }
}
