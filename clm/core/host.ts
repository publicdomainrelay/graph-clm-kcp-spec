// What a host does, independently of which host it is. The pi extension and
// the Claude Code mod run the same four steps — render the context document at
// session start, inject it before every model request, report each file a tool
// touched, and apply the model zone when the turn ends — so the steps live
// here, over the ports, and each host only has to say how to reach them.

import { contextDocPath, parseModelZone, splitContextDoc } from "./context-doc.ts";
import { deltaEmpty } from "./delta.ts";
import type { Clock, FileStore, StateBridge } from "./ports.ts";
import { systemClock } from "./ports.ts";
import type { Delta, ProgressRecord, SpecChange, SystemContext } from "./types.ts";

export interface HostOptions {
  bridge: StateBridge;

  files: FileStore;

  /** The managed working tree; the context document lives under it. */
  repoPath: string;

  context: string;

  /** The SpecChange this session is working off, when there is one. */
  change?: string;

  clock?: Clock;
}

export interface StartResult {
  path: string;

  document: string;

  /** Whether the rendered document differs from what was on disk. */
  changed: boolean;
}

export interface ApplyResult {
  delta: Delta;

  applied: boolean;

  folded?: string;

  /**
   * Why the edit did not reach the specification. A host reports it instead of
   * dropping it: an apply that failed and an edit that changed nothing look
   * identical from the outside, and only one of them is the model's doing.
   */
  error?: string;
}

export class ClmHost {
  private readonly options: HostOptions;

  private readonly clock: Clock;

  private turn = 0;

  private appliedModelZone: string | undefined;

  constructor(options: HostOptions) {
    this.options = options;
    this.clock = options.clock ?? systemClock;
  }

  get context(): string {
    return this.options.context;
  }

  get change(): string {
    return this.options.change ?? "";
  }

  /**
   * session start: render the context from kcp and write it to
   * `.specs/context/<name>.md`. A bridge that cannot answer (no workspace, no
   * such context) leaves the session without a document rather than failing it.
   */
  async start(): Promise<StartResult | undefined> {
    let document: string;
    try {
      document = await this.options.bridge.render(this.options.context);
    } catch {
      return undefined;
    }
    const path = contextDocPath(this.options.repoPath, this.options.context);
    const previous = (await this.options.files.exists(path)) ? await this.options.files.read(path) : "";
    this.appliedModelZone = splitContextDoc(document).model;
    if (previous !== document) await this.options.files.write(path, document);
    return { path, document, changed: previous !== document };
  }

  /**
   * before every model request: the context file as one system section. A
   * missing file answers undefined and the section is simply not added.
   */
  async section(): Promise<string | undefined> {
    const path = contextDocPath(this.options.repoPath, this.options.context);
    if (!(await this.options.files.exists(path))) return undefined;
    const document = await this.options.files.read(path);
    return document.trim().length ? document : undefined;
  }

  /** after a tool call: report the files it touched against the running change. */
  async touched(tool: string, files: readonly string[], note?: string): Promise<void> {
    if (!this.change || files.length === 0) return;
    const event: ProgressRecord = { turn: this.turn, tool, files: [...files], at: this.now() };
    if (note) event.note = note;
    try {
      await this.options.bridge.report(this.change, event);
    } catch {
      // A report is an observation; losing one must never fail the work.
    }
  }

  /** end of turn: apply the model zone when it changed, then report the turn. */
  async finish(note?: string): Promise<ApplyResult | undefined> {
    const path = contextDocPath(this.options.repoPath, this.options.context);
    let result: ApplyResult | undefined;
    if (await this.options.files.exists(path)) {
      const document = await this.options.files.read(path);
      const model = splitContextDoc(document).model;
      if (model !== this.appliedModelZone) {
        try {
          const applied = await this.options.bridge.apply(this.options.context, model);
          this.appliedModelZone = model;
          result = { delta: (applied.delta ?? {}) as Delta, applied: applied.applied, folded: applied.folded };
        } catch (error) {
          result = {
            delta: {},
            applied: false,
            error: error instanceof Error ? error.message : String(error),
          };
        }
      }
    }
    if (this.change) {
      const event: ProgressRecord = { turn: this.turn, at: this.now() };
      if (note) event.note = note;
      try {
        await this.options.bridge.report(this.change, event);
      } catch {
        // As above: an observation is best effort.
      }
    }
    this.turn += 1;
    return result;
  }

  private now(): string {
    return this.clock.now().toISOString();
  }
}

/** The context a host works on: SPECD_CLM_CONTEXT, else the environment. */
export function contextFromEnv(env: Record<string, string | undefined>): string {
  return env.SPECD_CLM_CONTEXT ?? "";
}

export function changeFromEnv(env: Record<string, string | undefined>): string {
  return env.SPECD_CLM_CHANGE ?? "";
}

/** The objects a host may read without shelling out again. */
export type { Delta, ProgressRecord, SpecChange, SystemContext };

export { deltaEmpty };
