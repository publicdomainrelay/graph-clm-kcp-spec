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
