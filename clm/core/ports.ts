import type { VertexSet, EdgeSet } from "./rows.ts";
import type { ProgressRecord, SpecChange, SystemContext } from "./types.ts";

export interface CommandResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

export interface RunOptions {
  cwd?: string;
  env?: Record<string, string>;
  stdin?: string;
  timeoutMs?: number;
}

export interface Runner {
  run(argv: readonly string[], options?: RunOptions): Promise<CommandResult>;
}

export interface FileStore {
  read(path: string): Promise<string>;
  write(path: string, text: string): Promise<void>;
  exists(path: string): Promise<boolean>;
  ancestors(request: { names: readonly string[]; of?: string; below?: string }): Promise<{ dir: string; name: string; content: string }[]>;
}

export interface StateBridge {
  render(context: string): Promise<string>;

  apply(context: string, modelZone: string): Promise<{ delta: unknown; applied: boolean; folded?: string }>;

  report(change: string, event: ProgressRecord): Promise<{ progress: number; recorded: boolean }>;

  readContext?(context: string): Promise<SystemContext | undefined>;

  readChange?(change: string): Promise<SpecChange | undefined>;
}

export interface GraphWriter {
  writeVertices(set: VertexSet): Promise<void>;
  writeEdges(set: EdgeSet): Promise<void>;
}

export interface Clock {
  now(): Date;
}

export const systemClock: Clock = { now: () => new Date() };
