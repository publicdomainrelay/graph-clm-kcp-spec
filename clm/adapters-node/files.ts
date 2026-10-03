// The Node implementation of the FileStore port.

import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

import type { FileStore } from "../core/mod.ts";

export function nodeFileStore(): FileStore {
  return {
    async read(path) {
      return readFileSync(path, "utf8");
    },
    async write(path, text) {
      mkdirSync(dirname(path), { recursive: true, mode: 0o755 });
      writeFileSync(path, text, { mode: 0o644 });
    },
    async exists(path) {
      return existsSync(path);
    },
    async ancestors({ names, of, below }) {
      const found: { dir: string; name: string; content: string }[] = [];
      const stop = below ? resolve(below) : undefined;
      let dir = resolve(of ?? process.cwd());
      const chain: string[] = [];
      for (;;) {
        chain.push(dir);
        const parent = dirname(dir);
        if (parent === dir) break;
        if (stop !== undefined && dir === stop) break;
        dir = parent;
      }
      for (const directory of chain.reverse()) {
        for (const name of names) {
          if (name.includes("..") || !name.endsWith(".md")) continue;
          const candidate = join(directory, name);
          if (!existsSync(candidate)) continue;
          found.push({ dir: directory, name, content: readFileSync(candidate, "utf8") });
        }
      }
      return found;
    },
  };
}
