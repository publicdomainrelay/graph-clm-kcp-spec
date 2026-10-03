// The ports a host implements. The core is pure, so it reaches the world only
// through these three, and every host satisfies them differently: the Node
// adapter with child_process and node:fs, the Claude Code mod with `$.process`
// and `$.fs`. Nothing below this line knows which host it is running in.

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

/** Runs a host command by its argument vector; there is no shell. */
export interface Runner {
  run(argv: readonly string[], options?: RunOptions): Promise<CommandResult>;
}

export interface FileStore {
  read(path: string): Promise<string>;
  write(path: string, text: string): Promise<void>;
  exists(path: string): Promise<boolean>;
  /**
   * Reads every `.md` of `names` in the directories above `dir`, root first,
   * the way the engine reads a CLAUDE.md. A host without an ancestor walk
   * answers the empty list.
   */
  ancestors(request: { names: readonly string[]; of?: string; below?: string }): Promise<{ dir: string; name: string; content: string }[]>;
}

/**
 * The state bridge is the Go CLI: kcp access, the delta authority and the graph
 * writes have one implementation, and every host reaches it the same way — a
 * process call. A host that cannot run a process (a test) substitutes its own.
 */
export interface StateBridge {
  render(context: string): Promise<string>;

  /** Applies a model zone; resolves with the delta the bridge computed. */
  apply(context: string, modelZone: string): Promise<{ delta: unknown; applied: boolean; folded?: string }>;

  report(change: string, event: ProgressRecord): Promise<{ progress: number; recorded: boolean }>;

  /** Reads one object, for a host that wants it without shelling out again. */
  readContext?(context: string): Promise<SystemContext | undefined>;

  readChange?(change: string): Promise<SpecChange | undefined>;
}

/**
 * The graph port. A host that has no Bolt driver of its own leaves it out; the
 * bridge writes the durable half (the status subresource) either way.
 */
export interface GraphWriter {
  writeVertices(set: VertexSet): Promise<void>;
  writeEdges(set: EdgeSet): Promise<void>;
}

export interface Clock {
  now(): Date;
}

export const systemClock: Clock = { now: () => new Date() };
