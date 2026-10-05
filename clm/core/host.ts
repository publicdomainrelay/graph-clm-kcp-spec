import { parseModelZone, splitContextDoc } from "./context-doc.ts";
import { deltaEmpty } from "./delta.ts";
import type { Clock, FileStore, StateBridge } from "./ports.ts";
import { systemClock } from "./ports.ts";
import type { Delta, ProgressRecord, SpecChange, SystemContext } from "./types.ts";

export interface HostOptions {
  bridge: StateBridge;

  files: FileStore;

  repoPath: string;

  docPath: string;

  context: string;

  change?: string;

  clock?: Clock;
}

export interface StartResult {
  path: string;

  document: string;

  changed: boolean;
}

export interface ApplyResult {
  delta: Delta;

  applied: boolean;

  queued?: string;

  summary?: string;

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

  async start(): Promise<StartResult | undefined> {
    let document: string;
    try {
      document = await this.options.bridge.render(this.options.context);
    } catch {
      return undefined;
    }
    const path = this.options.docPath;
    const previous = (await this.options.files.exists(path)) ? await this.options.files.read(path) : "";
    this.appliedModelZone = splitContextDoc(document).model;
    if (previous !== document) await this.options.files.write(path, document);
    return { path, document, changed: previous !== document };
  }

  async section(): Promise<string | undefined> {
    const path = this.options.docPath;
    if (!(await this.options.files.exists(path))) return undefined;
    const document = await this.options.files.read(path);
    return document.trim().length ? document : undefined;
  }

  async touched(tool: string, files: readonly string[], note?: string): Promise<void> {
    if (!this.change || files.length === 0) return;
    const event: ProgressRecord = { turn: this.turn, tool, files: [...files], at: this.now() };
    if (note) event.note = note;
    try {
      await this.options.bridge.report(this.change, event);
    } catch {
    }
  }

  async finish(note?: string): Promise<ApplyResult | undefined> {
    const path = this.options.docPath;
    let result: ApplyResult | undefined;
    if (await this.options.files.exists(path)) {
      const document = await this.options.files.read(path);
      const model = splitContextDoc(document).model;
      if (model !== this.appliedModelZone) {
        try {
          const applied = await this.options.bridge.apply(this.options.context, model);
          this.appliedModelZone = model;
          result = {
            delta: (applied.delta ?? {}) as Delta,
            applied: applied.applied,
            queued: applied.queued,
            summary: applied.summary,
          };
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
      }
    }
    this.turn += 1;
    return result;
  }

  private now(): string {
    return this.clock.now().toISOString();
  }
}

export function contextFromEnv(env: Record<string, string | undefined>): string {
  return env.SPECD_CLM_CONTEXT ?? "";
}

export function changeFromEnv(env: Record<string, string | undefined>): string {
  return env.SPECD_CLM_CHANGE ?? "";
}

export type { Delta, ProgressRecord, SpecChange, SystemContext };

export { deltaEmpty };
