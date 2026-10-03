// The Node implementation of the Runner port: a host command by its argument
// vector, no shell, the whole output read. It is the same transport the Go side
// uses to call a model, so a host and a controller invoke a process alike.

import { spawn } from "node:child_process";

import type { CommandResult, RunOptions, Runner } from "../core/mod.ts";

const DEFAULT_TIMEOUT_MS = 30_000;

export function nodeRunner(): Runner {
  return {
    run(argv, options = {}) {
      return run(argv, options);
    },
  };
}

function run(argv: readonly string[], options: RunOptions): Promise<CommandResult> {
  return new Promise((resolve, reject) => {
    if (argv.length === 0) {
      reject(new Error("runner: an argument vector is required"));
      return;
    }
    const child = spawn(argv[0] as string, argv.slice(1), {
      cwd: options.cwd,
      env: options.env ? { ...process.env, ...options.env } : process.env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const stdout: string[] = [];
    const stderr: string[] = [];
    const timeout = setTimeout(() => {
      child.kill("SIGKILL");
      reject(new Error(`runner: ${argv[0]} did not finish within ${options.timeoutMs ?? DEFAULT_TIMEOUT_MS} ms`));
    }, options.timeoutMs ?? DEFAULT_TIMEOUT_MS);
    child.stdout.on("data", (chunk: Buffer) => stdout.push(chunk.toString("utf8")));
    child.stderr.on("data", (chunk: Buffer) => stderr.push(chunk.toString("utf8")));
    child.on("error", (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    child.on("close", (code) => {
      clearTimeout(timeout);
      resolve({ exitCode: code ?? 1, stdout: stdout.join(""), stderr: stderr.join("") });
    });
    if (options.stdin !== undefined) child.stdin.end(options.stdin);
    else child.stdin.end();
  });
}
