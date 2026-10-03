import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import {
  apply,
  canonicalSpec,
  changeId,
  codeRefId,
  count,
  deltaEmpty,
  diff,
  diffObserved,
  extractReferences,
  fileCodeRefId,
  mergeDeclared,
  parseModelZone,
  renderModelZone,
  renderManagedZone,
  splitContextDoc,
  stable,
  summary,
  type Delta,
  type ObservedFacts,
  type SystemContextSpec,
} from "../core/mod.ts";

function calcSpec(): SystemContextSpec {
  return {
    repository: "calc",
    upstream: "self",
    intent: "Arithmetic on two integers.",
    requirements: [
      { id: "r.add", level: "MUST", text: "Add returns the sum.", codeRefs: ["file:calc/calc.go"] },
      { id: "r.multiply", level: "MUST", text: "Multiply returns the product.", codeRefs: ["function:Multiply"] },
    ],
    interfaces: [
      { name: "Add", kind: "function", signature: "func Add(a, b int) int", file: "calc/calc.go" },
      { name: "Multiply", kind: "function", signature: "func Multiply(a, b int) int", file: "calc/calc.go" },
    ],
    codeRefs: ["file:calc/calc.go"],
  };
}

function subtractSpec(): SystemContextSpec {
  const next = calcSpec();
  next.requirements = [
    ...(next.requirements ?? []),
    { id: "r.subtract", level: "SHOULD", text: "Subtract returns the difference.", codeRefs: ["function:Subtract"] },
  ];
  next.interfaces = [
    ...(next.interfaces ?? []),
    { name: "Subtract", kind: "function", signature: "func Subtract(a, b int) int", file: "calc/calc.go" },
  ];
  next.codeRefs = [...(next.codeRefs ?? []), "function:Subtract"];
  return next;
}

function calcObserved(): ObservedFacts {
  return {
    files: ["calc/calc.go", "calc/calc_test.go"],
    interfaces: [
      { name: "Add", kind: "function", signature: "func Add(a, b int) int", file: "calc/calc.go", line: 3, codegraphId: "function:aa11" },
      { name: "Multiply", kind: "function", signature: "func Multiply(a, b int) int", file: "calc/calc.go", line: 8, codegraphId: "function:bb22" },
    ],
    fingerprint: "1".repeat(64),
  };
}

function subtractObserved(): ObservedFacts {
  const next = calcObserved();
  next.interfaces = [
    ...(next.interfaces ?? []),
    { name: "Subtract", kind: "function", signature: "func Subtract(a, b int) int", file: "calc/calc.go", line: 13, codegraphId: "function:cc33" },
  ];
  next.fingerprint = "2".repeat(64);
  return next;
}

function golden(name: string): Delta {
  return JSON.parse(readFileSync(new URL(`../../testdata/delta/${name}`, import.meta.url), "utf8")) as Delta;
}

// The golden files are the contract: Go pins them in abc/delta, and this reads
// the same two. A drift in either language fails on its own side.
test("the delta mirror matches the Go golden files", () => {
  assert.deepEqual(diff(calcSpec(), subtractSpec()), golden("spec-edit.json"));
  assert.deepEqual(diffObserved(calcObserved(), subtractObserved()), golden("observed-edit.json"));
});

test("apply is the inverse of diff", () => {
  const from = calcSpec();
  const to = subtractSpec();
  assert.deepEqual(apply(from, diff(from, to)), canonicalSpec(to));
  assert.deepEqual(apply(from, diff(from, from)), canonicalSpec(from));
});

test("an empty delta is not work", () => {
  assert.equal(deltaEmpty(diff(calcSpec(), calcSpec())), true);
  assert.equal(deltaEmpty(diff(calcSpec(), subtractSpec())), false);
  assert.equal(summary(diff(calcSpec(), subtractSpec())), "+3");
  assert.deepEqual(count(diff(calcSpec(), subtractSpec())), { added: 3, removed: 0, changed: 0 });
});

test("reordering a list is not an edit", () => {
  const reordered = calcSpec();
  reordered.interfaces = [...(reordered.interfaces ?? [])].reverse();
  reordered.codeRefs = [...(reordered.codeRefs ?? [])].reverse();
  assert.equal(deltaEmpty(diff(calcSpec(), reordered)), true);
});

test("the graph ids are the ones Go computes", () => {
  const goldenIds = JSON.parse(readFileSync(new URL("../../testdata/ids.json", import.meta.url), "utf8")) as Record<string, number>;
  for (const [key, want] of Object.entries(goldenIds)) {
    assert.equal(stable(key), want, key);
  }
  assert.equal(codeRefId("file:calc/calc.go"), goldenIds["coderef:file:calc/calc.go"]);
  assert.equal(fileCodeRefId("calc/calc.go"), goldenIds["coderef:file:calc/calc.go"]);
  assert.equal(changeId("calc-s2c-abcdef12"), goldenIds["change:calc-s2c-abcdef12"]);
});

test("the model zone round trips and the tool keeps what it owns", () => {
  const rendered = renderModelZone("calc", "calc", calcSpec());
  const parsed = parseModelZone(rendered);
  assert.equal(parsed.intent, "Arithmetic on two integers.");
  assert.equal(parsed.requirements?.length, 2);
  assert.equal(parsed.interfaces?.length, 2);
  assert.equal(parsed.repository, undefined);
  assert.equal(parsed.codeRefs, undefined);
  const merged = mergeDeclared(calcSpec(), parsed);
  assert.deepEqual(merged.codeRefs, ["file:calc/calc.go"]);
  assert.deepEqual(merged.requirements, canonicalSpec(calcSpec()).requirements);
  assert.equal(deltaEmpty(diff(calcSpec(), merged)), true);
});

test("a model zone with no spec block is refused", () => {
  assert.throws(() => parseModelZone("# Context: calc\n\njust prose\n"), /opens no/);
});

test("the managed zone lists refs and the model zone survives a split", () => {
  const model = renderModelZone("calc", "calc", calcSpec());
  const document = `${model}\n${renderManagedZone([
    { codegraphId: "function:Add", kind: "function", name: "Add", filePath: "calc/calc.go" },
  ])}\n`;
  const split = splitContextDoc(document);
  assert.match(split.managed, /function:Add/);
  // The model zone never carries the code refs, so the comparison is against
  // the merge, which is what an apply actually writes.
  assert.equal(deltaEmpty(diff(calcSpec(), mergeDeclared(calcSpec(), parseModelZone(split.model)))), true);
});

test("references are read from prose and from spelled ids", () => {
  const found = extractReferences("See `calc/calc.go` and function:Add here\n");
  assert.deepEqual(found, ["calc/calc.go", "function:Add"]);
  assert.deepEqual(extractReferences("no references here"), []);
});
