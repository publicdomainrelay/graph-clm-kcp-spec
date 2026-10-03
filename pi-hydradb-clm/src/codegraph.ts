import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { isCodegraphId, type CodeRefRecord } from "./context-doc.ts";

export interface CodegraphNode {
  id: string;
  kind: string;
  name: string;
  qualifiedName: string;
  filePath: string;
}

export const CODEGRAPH_DIR = ".codegraph";
export const CODEGRAPH_DB = "codegraph.db";

export function findCodegraphDb(startDir: string): string | null {
  let dir = resolve(startDir);
  for (;;) {
    const candidate = join(dir, CODEGRAPH_DIR, CODEGRAPH_DB);
    if (existsSync(candidate)) return candidate;
    const parent = dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
}

export function codegraphFilePathId(projectRoot: string, path: string): string {
  return `file:${relative(projectRoot, resolve(path)).split("\\").join("/")}`;
}

interface RawNode {
  id: string;
  kind: string;
  name: string;
  qualified_name?: string;
  qualifiedName?: string;
  file_path?: string;
  filePath?: string;
}

function toNode(raw: RawNode): CodegraphNode {
  return {
    id: raw.id,
    kind: raw.kind,
    name: raw.name,
    qualifiedName: raw.qualified_name ?? raw.qualifiedName ?? raw.name,
    filePath: raw.file_path ?? raw.filePath ?? "",
  };
}

const SELECT_COLUMNS = "id, kind, name, qualified_name, file_path";

type SqliteStatement = { all(...params: unknown[]): unknown[] };
type SqliteDatabase = {
  prepare(sql: string): SqliteStatement;
  close(): void;
};

async function loadSqlite(): Promise<new (path: string) => SqliteDatabase> {
  const mod = (await import("node:sqlite")) as unknown as {
    DatabaseSync: new (path: string) => SqliteDatabase;
  };
  return mod.DatabaseSync;
}

function looksLikePath(reference: string): boolean {
  return reference.startsWith("/") || reference.startsWith("./") || reference.includes("/");
}

export class CodegraphResolver {
  private db: SqliteDatabase | null = null;
  private readonly cache = new Map<string, CodegraphNode[]>();
  private sqliteFailed = false;
  /** References dropped because a bare name matched symbols in several files. */
  readonly ambiguous = new Map<string, CodegraphNode[]>();

  private constructor(
    private readonly dbPath: string | null,
    private readonly projectRoot: string,
  ) {}

  static open(cwd: string): CodegraphResolver {
    const dbPath = findCodegraphDb(cwd);
    return new CodegraphResolver(dbPath, dbPath ? dirname(dirname(dbPath)) : resolve(cwd));
  }

  get available(): boolean {
    return this.dbPath !== null;
  }

  private async database(): Promise<SqliteDatabase | null> {
    if (this.db !== null) return this.db;
    if (this.dbPath === null || this.sqliteFailed) return null;
    try {
      const DatabaseSync = await loadSqlite();
      this.db = new DatabaseSync(this.dbPath);
      return this.db;
    } catch {
      this.sqliteFailed = true;
      return null;
    }
  }

  private fromCli(reference: string): CodegraphNode[] {
    try {
      const stdout = execFileSync("codegraph", ["query", "-j", "-l", "10", reference], {
        cwd: this.projectRoot,
        encoding: "utf8",
        timeout: 15_000,
        stdio: ["ignore", "pipe", "ignore"],
      });
      const parsed = JSON.parse(stdout) as Array<{ node?: RawNode }>;
      return parsed.flatMap((entry) => (entry.node ? [toNode(entry.node)] : []));
    } catch {
      return [];
    }
  }

  async resolve(reference: string): Promise<CodegraphNode[]> {
    const cached = this.cache.get(reference);
    if (cached) return cached;

    let nodes: CodegraphNode[] = [];
    const db = await this.database();
    if (db) {
      try {
        if (isCodegraphId(reference)) {
          nodes = db
            .prepare(`SELECT ${SELECT_COLUMNS} FROM nodes WHERE id = ?`)
            .all(reference)
            .map((row) => toNode(row as RawNode));
        } else if (looksLikePath(reference)) {
          const asFileId = codegraphFilePathId(this.projectRoot, reference);
          nodes = db
            .prepare(
              `SELECT ${SELECT_COLUMNS} FROM nodes WHERE id = ? OR file_path = ? LIMIT 10`,
            )
            .all(asFileId, relative(this.projectRoot, resolve(reference)).split("\\").join("/"))
            .map((row) => toNode(row as RawNode));
        } else {
          const exact = db
            .prepare(`SELECT ${SELECT_COLUMNS} FROM nodes WHERE qualified_name = ? LIMIT 10`)
            .all(reference)
            .map((row) => toNode(row as RawNode));
          if (exact.length > 0) {
            nodes = exact;
          } else {
            const byName = db
              .prepare(`SELECT ${SELECT_COLUMNS} FROM nodes WHERE name = ? LIMIT 20`)
              .all(reference)
              .map((row) => toNode(row as RawNode));
            const files = new Set(byName.map((node) => node.filePath));
            if (files.size > 1) {
              this.ambiguous.set(reference, byName);
              nodes = [];
            } else {
              nodes = byName;
            }
          }
        }
      } catch {
        nodes = [];
      }
    }
    if (nodes.length === 0 && !this.ambiguous.has(reference)) {
      const viaCli = this.fromCli(reference);
      const files = new Set(viaCli.map((node) => node.filePath));
      if (files.size > 1 && !isCodegraphId(reference) && !looksLikePath(reference)) {
        this.ambiguous.set(reference, viaCli);
      } else {
        nodes = viaCli;
      }
    }

    this.cache.set(reference, nodes);
    return nodes;
  }

  async resolveAll(references: string[]): Promise<CodegraphNode[]> {
    const out: CodegraphNode[] = [];
    const seen = new Set<string>();
    for (const reference of references) {
      for (const node of await this.resolve(reference)) {
        if (seen.has(node.id)) continue;
        seen.add(node.id);
        out.push(node);
      }
    }
    return out;
  }

  close(): void {
    try {
      this.db?.close();
    } catch {
      // best effort
    }
    this.db = null;
  }
}

export function toCodeRefRecord(node: CodegraphNode, sessionId: number): CodeRefRecord {
  return {
    id: sessionId,
    codegraphId: node.id,
    kind: node.kind,
    name: node.qualifiedName || node.name,
    filePath: node.filePath,
  };
}
