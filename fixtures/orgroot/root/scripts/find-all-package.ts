#!/usr/bin/env -S deno run --allow-read
// Print every deno package of the org root, one per line, with its directory.
// Submodules are walked like any other directory: the org root is one tree.

for await (const entry of Deno.readDir(".")) {
  if (!entry.isDirectory || entry.name.startsWith(".")) continue;
  try {
    const manifest = JSON.parse(await Deno.readTextFile(`${entry.name}/deno.json`));
    console.log(`${manifest.name ?? entry.name}\t${entry.name}`);
  } catch {
    // not a package
  }
}
