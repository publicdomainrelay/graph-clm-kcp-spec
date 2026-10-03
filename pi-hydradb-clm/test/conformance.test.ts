import assert from "node:assert/strict";
import { after, before, describe, test } from "node:test";
import { PORTABLE_CASES } from "../src/conformance.ts";
import type { GraphClient } from "../src/graph.ts";
import { ensureGraph, openGraph } from "./harness.ts";

let graph: GraphClient;
let backend = "";

before(async () => {
  const target = await ensureGraph();
  backend = target.backend;
  graph = await openGraph();
});

after(async () => {
  await graph?.close();
});

function reason(error: unknown): string {
  const text = error instanceof Error ? error.message : String(error);
  return text.replace(/\s+/g, " ").trim().slice(0, 200);
}

describe(`portable Cypher subset on ${process.env.HYDRA_BACKEND ?? "default backend"}`, () => {
  for (const probe of PORTABLE_CASES) {
    test(probe.id, async () => {
      for (const statement of probe.setup ?? []) {
        await graph.run(statement.cypher, statement.params ?? {});
      }
      try {
        const rows = await graph.run(probe.cypher, probe.params ?? {});
        if (probe.minRows !== undefined) {
          assert.ok(
            rows.length >= probe.minRows,
            `[${backend}] ${probe.id} returned ${rows.length} rows, expected at least ${probe.minRows}`,
          );
        }
        if (probe.expectRows !== undefined) {
          assert.equal(
            rows.length,
            probe.expectRows,
            `[${backend}] ${probe.id} returned ${rows.length} rows, expected exactly ${probe.expectRows}`,
          );
        }
      } catch (error) {
        assert.fail(`[${backend}] ${probe.id} rejected: ${reason(error)}`);
      }
    });
  }
});
