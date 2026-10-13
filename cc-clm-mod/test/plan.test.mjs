import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const plan = await import(join(here, "..", "hooks", "plan.ts"));

const env = { specctl: "specctl", workspace: "root:specs", namespace: "default" };

const pack =
  "## repository card\n\nrepository: hydradb\n\n" +
  "## task card\n\nseed: file:calc/calc.go\nowner: calc\n" +
  "verify: `go build ./... go vet ./...`\n" +
  "acceptance: `live: go test ./test/e2e`\n\n" +
  "## owner context card\n\ncalc\nintent: Arithmetic on two integers.\n";

test("the owner lookup asks specctl which context owns the path", () => {
  const argv = plan.ownerArgv(env, "/work/tree/calc/calc.go");
  assert.deepEqual(argv.slice(0, 6), ["specctl", "arch", "owner", "/work/tree/calc/calc.go", "-o", "json"]);
  assert.ok(argv.includes("--workspace") && argv.includes("root:specs"));
  assert.ok(argv.includes("--namespace") && argv.includes("default"));
});

test("the owner answer is read, and a path no context owns names no context", () => {
  assert.equal(plan.ownerOf('{\n  "path": "calc/calc.go",\n  "context": "calc"\n}\n'), "calc");
  assert.equal(plan.ownerOf('{\n  "path": "docs/plan.md"\n}\n'), undefined);
  assert.equal(plan.ownerOf("no context owns docs/plan.md\n"), undefined);
  assert.equal(plan.ownerOf(""), undefined);
});

test("the most recently used set holds four contexts, and a fifth evicts the oldest", () => {
  let recent = [];
  for (const context of ["a", "b", "c", "d"]) recent = plan.rememberOwner(recent, context);
  assert.deepEqual(recent, ["d", "c", "b", "a"]);
  recent = plan.rememberOwner(recent, "e");
  assert.deepEqual(recent, ["e", "d", "c", "b"], "the fifth context evicted the oldest");
  assert.equal(plan.OWNER_CONTEXT_LIMIT, 4);
});

test("touching a context again moves it to the front without growing the set", () => {
  const recent = plan.rememberOwner(plan.rememberOwner(plan.rememberOwner([], "a"), "b"), "a");
  assert.deepEqual(recent, ["a", "b"]);
  assert.deepEqual(plan.rememberOwner(recent, "  "), ["a", "b"]);
});

test("the owner cards come from the pack of each context, and the cards are stable", () => {
  const argv = plan.packArgv(env, "calc");
  assert.deepEqual(argv.slice(0, 5), ["specctl", "context", "pack", "--context", "calc"]);
  assert.deepEqual(plan.packsSection([pack, "  "])?.id, plan.PACKS_SECTION_ID);
  assert.equal(plan.packsSection(["  ", ""]), undefined);
  assert.equal(plan.packsSection([pack])?.scope, "session");
});

test("a session with no context packs the directory it runs in", () => {
  const argv = plan.packFileArgv(env, "/work/tree/impl/clm");
  assert.deepEqual(argv.slice(0, 5), ["specctl", "context", "pack", "--file", "/work/tree/impl/clm"]);
  assert.ok(argv.includes("--workspace"));
});

test("the realize task card names the change, the context, the verify and the acceptance", () => {
  const card = plan.taskCard("calc-s2c-1", "calc", pack);
  assert.ok(card, "a realize session has a task card");
  assert.equal(card.id, plan.TASK_SECTION_ID);
  assert.equal(card.scope, "session");
  assert.match(card.text, /change: calc-s2c-1/);
  assert.match(card.text, /context: calc/);
  assert.match(card.text, /verify: `go build \.\/\.\.\. go vet \.\/\.\.\.`/);
  assert.match(card.text, /acceptance: `live: go test \.\/test\/e2e`/);
  assert.match(card.text, /Run only the verify and the acceptance of this task\./);
  assert.match(card.text, /Do not run the repository's whole suite\./);
  assert.match(card.text, /Do not commit\./);
  assert.match(card.text, /Do not push\./);
});

test("an ordinary session has no task card", () => {
  assert.equal(plan.taskCard("", "calc", pack), undefined);
  assert.equal(plan.taskCard("  ", "calc", pack), undefined);
});

test("the new tools map their arguments to specctl", () => {
  assert.deepEqual(plan.archInvocation("specctl", `${plan.ARCH_TOOL_PREFIX}arch_owner`, { path: "calc/calc.go" }), {
    argv: ["specctl", "arch", "owner", "calc/calc.go"],
  });
  assert.deepEqual(plan.archInvocation("specctl", `${plan.ARCH_TOOL_PREFIX}arch_neighbors`, { context: "calc" }), {
    argv: ["specctl", "clm", "neighbors", "calc"],
  });
  assert.deepEqual(plan.archInvocation("specctl", `${plan.ARCH_TOOL_PREFIX}arch_search`, { terms: "rate limit" }), {
    argv: ["specctl", "graph", "search", "rate limit"],
  });
  assert.deepEqual(plan.archInvocation("specctl", `${plan.ARCH_TOOL_PREFIX}arch_search`, { terms: "rate limit", limit: 5 }), {
    argv: ["specctl", "graph", "search", "rate limit", "--limit", "5"],
  });
});

test("a new tool call without what it needs is answered, not run", () => {
  assert.equal(typeof plan.archInvocation("specctl", `${plan.ARCH_TOOL_PREFIX}arch_owner`, {}), "string");
  assert.equal(typeof plan.archInvocation("specctl", `${plan.ARCH_TOOL_PREFIX}arch_neighbors`, { context: " " }), "string");
  assert.equal(typeof plan.archInvocation("specctl", `${plan.ARCH_TOOL_PREFIX}arch_search`, {}), "string");
});

test("the mod registers the four arch tools and the three new ones", () => {
  const names = plan.ARCH_TOOLS.map((tool) => tool.name);
  for (const name of [
    "arch_outline",
    "arch_context",
    "arch_edit",
    "arch_changes",
    "arch_plans",
    "arch_plan",
    "arch_findings",
    "arch_release",
    "arch_owner",
    "arch_neighbors",
    "arch_search",
  ]) {
    assert.ok(names.includes(name), `${name} is not registered`);
  }
});
