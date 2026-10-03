// The vendored core must be the core. A mod cannot import a file outside its
// own folder, so clm/core is bundled into hooks/vendor; this test fails when
// the bundle is stale, which is the failure mode that would otherwise be found
// by a session behaving like an older core.
//
// It runs under `node --test`, not `claude plugin test`: the plugin test
// environment has no filesystem, and a staleness check is a filesystem check.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { coreHash } from "../build.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const vendor = join(here, "..", "hooks", "vendor");
const bundle = readFileSync(join(vendor, "core.js"), "utf8");

test("the vendored core is not stale", () => {
  const recorded = readFileSync(join(vendor, "core.hash"), "utf8").trim();
  assert.equal(recorded, coreHash(), "run `npm run build:mod`");
});

test("the bundle is one ES module the engine can load", () => {
  // A mod imports only its own files and may not use `import()`.
  assert.equal(/^\s*import[\s("]/m.test(bundle), false, "the bundle has an import");
  assert.equal(bundle.includes("import("), false, "the bundle uses a dynamic import");
  assert.equal(bundle.includes('from "node:'), false, "the bundle reaches a node builtin");
  assert.equal(bundle.includes("require("), false, "the bundle requires");
  assert.match(bundle, /^export \{/m, "the bundle exports nothing");
});

test("the bundle carries the pieces both hosts share", () => {
  for (const name of ["ClmHost", "renderModelZone", "parseModelZone", "mergeDeclared", "diff", "liveRows", "MANAGED_BEGIN"]) {
    assert.match(bundle, new RegExp(`\\b${name}\\b`), `${name} is missing from the bundle`);
  }
});

test("the types beside the bundle describe what the mod imports", () => {
  const types = readFileSync(join(vendor, "core.d.ts"), "utf8");
  // A mod is type-checked by the engine's own tsconfig, which does not allow
  // .ts import paths, so this file is self-contained rather than a re-export.
  assert.equal(/from ".*\.ts"/.test(types), false, "the types reach a .ts source");
  for (const name of ["ClmHost", "StateBridge", "FileStore", "Runner", "ProgressRecord"]) {
    assert.match(types, new RegExp(`\\b${name}\\b`), `${name} is missing from the types`);
  }
});
