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
  docPathFromEnv,
  extractReferences,
  fileCodeRefId,
  mergeDeclared,
  parseModelZone,
  renderModelZone,
  renderManagedZone,
  splitContextDoc,
  ClmHost,
  stable,
  summary,
  type Delta,
  type ObservedFacts,
  type SystemContextSpec,
} from "../core/mod.ts";
import { deltaSummaryLines } from "../adapters-node/mod.ts";

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

function methodSpec(): SystemContextSpec {
  return {
    repository: "store",
    upstream: "self",
    intent: "A key store indexed two ways.",
    requirements: [
      { id: "r.list", level: "MUST", text: "List returns the keys.", codeRefs: ["function:Indexer.List"] },
    ],
    interfaces: [
      { name: "Indexer.List", kind: "method", signature: "() []string", file: "store/store.go" },
      { name: "Set.List", kind: "method", signature: "() []string", file: "store/store.go" },
    ],
    codeRefs: ["file:store/store.go"],
  };
}

function methodSpecNext(): SystemContextSpec {
  const next = methodSpec();
  next.interfaces = [
    { name: "Indexer.List", kind: "method", signature: "(limit int) []string", file: "store/store.go" },
    { name: "Set.List", kind: "method", signature: "() []string", file: "store/store.go" },
    { name: "Set.Add", kind: "method", signature: "(item string)", file: "store/store.go" },
  ];
  return next;
}

function methodObserved(): ObservedFacts {
  return {
    files: ["store/store.go"],
    interfaces: [
      { name: "Indexer.List", kind: "method", signature: "() []string", file: "store/store.go", line: 5, codegraphId: "method:ii11" },
      { name: "Set.List", kind: "method", signature: "() []string", file: "store/store.go", line: 15, codegraphId: "method:ss22" },
    ],
    fingerprint: "3".repeat(64),
  };
}

function methodObservedNext(): ObservedFacts {
  const next = methodObserved();
  next.interfaces = [
    { name: "Set.List", kind: "method", signature: "() []string", file: "store/store.go", line: 15, codegraphId: "method:ss22" },
    { name: "Set.Add", kind: "method", signature: "(item string)", file: "store/store.go", line: 22, codegraphId: "method:aa44" },
  ];
  next.fingerprint = "4".repeat(64);
  return next;
}

function marketSpec(): SystemContextSpec {
  return {
    repository: "greenfield-market",
    upstream: "self",
    intent: "A market guest that reports its own network information.",
    interactions: [
      {
        id: "i.report",
        peer: "host",
        initiator: "self",
        channel: "relay",
        carries: ["network-info"],
        purpose: "network-discovery",
        level: "MUST",
      },
      {
        id: "i.no-reach-in",
        peer: "host",
        initiator: "peer",
        channel: "relay",
        carries: ["network-info"],
        purpose: "network-discovery",
        level: "MUST",
        forbidden: true,
      },
    ],
  };
}

function marketSpecNext(): SystemContextSpec {
  const next = marketSpec();
  next.interactions = [
    {
      id: "i.no-reach-in",
      peer: "host",
      initiator: "peer",
      channel: "relay",
      carries: ["network-info"],
      purpose: "network-discovery",
      level: "MUST",
      forbidden: true,
    },
    {
      id: "i.report",
      peer: "host",
      initiator: "self",
      channel: "relay",
      carries: ["network-info"],
      purpose: "network-discovery",
      level: "SHOULD",
    },
  ];
  return next;
}

function golden(name: string): Delta {
  return JSON.parse(readFileSync(new URL(`../../testdata/delta/${name}`, import.meta.url), "utf8")) as Delta;
}

test("the delta mirror matches the Go golden files", () => {
  assert.deepEqual(diff(calcSpec(), subtractSpec()), golden("spec-edit.json"));
  assert.deepEqual(diffObserved(calcObserved(), subtractObserved()), golden("observed-edit.json"));
  assert.deepEqual(diff(methodSpec(), methodSpecNext()), golden("method-edit.json"));
  assert.deepEqual(diffObserved(methodObserved(), methodObservedNext()), golden("method-observed-edit.json"));
  assert.deepEqual(diff(marketSpec(), marketSpecNext()), golden("interactions-edit.json"));
});

test("the model zone carries declared interactions", () => {
  const rendered = renderModelZone("guest", "greenfield-market", marketSpec());
  const parsed = parseModelZone(rendered);
  assert.deepEqual(parsed.interactions, canonicalSpec(marketSpec()).interactions);
  assert.equal(deltaEmpty(diff(marketSpec(), mergeDeclared(marketSpec(), parsed))), true);
  assert.deepEqual(apply(marketSpec(), diff(marketSpec(), marketSpecNext())), canonicalSpec(marketSpecNext()));
});

test("a method keyed by its receiver is one entry of its own", () => {
  const change = diff(methodSpec(), methodSpecNext());
  assert.deepEqual(
    (change.interfaces ?? []).map((entry) => `${entry.op}:${entry.name}`),
    ["changed:Indexer.List", "added:Set.Add"],
  );
  assert.deepEqual(count(change), { added: 1, removed: 0, changed: 1 });
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
  assert.equal(deltaEmpty(diff(calcSpec(), mergeDeclared(calcSpec(), parseModelZone(split.model)))), true);
});

test("references are read from prose and from spelled ids", () => {
  const found = extractReferences("See `calc/calc.go` and function:Add here\n");
  assert.deepEqual(found, ["calc/calc.go", "function:Add"]);
  assert.deepEqual(extractReferences("no references here"), []);
});

test("the context document lives in the state dir, never in the project tree", () => {
  assert.equal(docPathFromEnv({ SPECD_CLM_DOC: "/exact/calc.md" }, "/home/u", "/src/calc", "calc"), "/exact/calc.md");
  assert.equal(docPathFromEnv({ SPECD_CLM_DOC_DIR: "/docs", SPECD_CLM_REPOSITORY: "calc" }, "/home/u", "/src/x", "c"), "/docs/calc/c.md");
  assert.equal(docPathFromEnv({ XDG_STATE_HOME: "/xdg" }, "/home/u", "/src/calc/", "c"), "/xdg/specd/clm/calc/c.md");
  assert.equal(docPathFromEnv({}, "/home/u", "/src/calc", "c"), "/home/u/.local/state/specd/clm/calc/c.md");
  assert.ok(!docPathFromEnv({}, "/home/u", "/src/calc", "c").startsWith("/src/calc"));
});

test("an apply that failed is reported and tried again", async () => {
  const document = renderModelZone("calc", "calc", calcSpec());
  const files = new Map<string, string>();
  let attempts = 0;
  const host = new ClmHost({
    context: "calc",
    repoPath: "/repo",
    docPath: "/state/clm/calc/calc.md",
    files: {
      exists: (path) => Promise.resolve(files.has(path)),
      read: (path) => Promise.resolve(files.get(path) ?? ""),
      write: (path, contents) => {
        files.set(path, contents);
        return Promise.resolve();
      },
      ancestors: () => Promise.resolve([]),
    },
    bridge: {
      render: () => Promise.resolve(document),
      apply: () => {
        attempts += 1;
        return Promise.reject(new Error("the workspace has no such context"));
      },
      report: () => Promise.resolve({ progress: 0, recorded: false }),
    },
  });
  await host.start();
  const path = [...files.keys()][0] as string;
  files.set(path, `${document}\n<!-- a model edit -->\n`);
  const first = await host.finish("turn");
  assert.equal(first?.applied, false);
  assert.match(first?.error ?? "", /no such context/);
  const second = await host.finish("turn");
  assert.match(second?.error ?? "", /no such context/);
  assert.equal(attempts, 2);
});

test("an apply queued behind a running change reports the queue and the summary", async () => {
  const document = renderModelZone("calc", "calc", calcSpec());
  const files = new Map<string, string>([["/state/clm/calc/calc.md", document]]);
  const host = new ClmHost({
    context: "calc",
    repoPath: "/repo",
    docPath: "/state/clm/calc/calc.md",
    files: {
      exists: (path) => Promise.resolve(files.has(path)),
      read: (path) => Promise.resolve(files.get(path) ?? ""),
      write: (path, contents) => {
        files.set(path, contents);
        return Promise.resolve();
      },
      ancestors: () => Promise.resolve([]),
    },
    bridge: {
      render: () => Promise.resolve(document),
      apply: () =>
        Promise.resolve({
          delta: { requirements: [{ op: "removed", id: "r.multiply" }] },
          applied: true,
          queued: "calc-s2c-abc",
          summary: "- r.multiply",
        }),
      report: () => Promise.resolve({ progress: 0, recorded: false }),
    },
  });
  await host.start();
  files.set("/state/clm/calc/calc.md", `${document}\n<!-- the model drops one requirement -->\n`);
  const applied = await host.finish("turn");
  assert.equal(applied?.applied, true);
  assert.equal(applied?.queued, "calc-s2c-abc");
  assert.equal(applied?.summary, "- r.multiply");
});

test("the delta summary keeps only the by-id lines", () => {
  const stderr = [
    "+ r.subtract",
    "~ r.add (text)",
    "- interface Multiply",
    "specctl clm apply: calc applied (+1 ~1 -1), queued behind calc-s2c-abc",
  ].join("\n");
  assert.equal(deltaSummaryLines(stderr), "+ r.subtract\n~ r.add (text)\n- interface Multiply");
  assert.equal(deltaSummaryLines("specctl clm apply: no change: nothing"), "");
});
