// A minimal stand-in for the engine's own declaration file.
//
// Claude Code writes the real one (about 20,000 lines, for the build you are
// running) into `<mod>/.claude-plugin/types/claude-code/index.d.ts` every time
// it loads a mod from a folder you own, and a mod's tsconfig extends the one
// beside it. That copy does not exist until the mod has been loaded once, so
// this file declares the small surface cc-clm-mod uses and lets `tsc -p
// cc-clm-mod` run offline and in CI. It is a subset, never a replacement: when
// the engine has laid its own declarations down, prefer them.
//
// Shape only; the authority is `claude plugin validate` plus the engine's file.

declare module "claude-code" {
  export type HookResult = unknown;

  export interface NextBudget {
    readonly ms: number;
    readonly remainingMs: number;
  }

  export interface Next<E = unknown, O = unknown> {
    (event: E): Promise<O>;
    readonly budget: NextBudget;
    readonly signal: AbortSignal;
    readonly event: string;
  }

  export type Hook<E = any, O = any> = (engine: any, event: E, next: Next<E, O>) => O | Promise<O>;

  export interface Registration {
    catch(handler: Hook): Registration;
  }

  export interface On {
    (pattern: string, hook: Hook): Registration;
    (pattern: string, matcher: Record<string, unknown>, hook: Hook): Registration;
  }

  export interface PluginOptions {
    [key: string]: unknown;
  }

  export type Register = (on: On, options: PluginOptions) => unknown;
}

declare module "claude-code/testing" {
  export const test: (name: string, body: (...args: any[]) => unknown) => void;
  export const expect: any;
  export const describe: (name: string, body: () => void) => void;
  export const mock: any;
}
