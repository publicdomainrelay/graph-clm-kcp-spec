import { expect, test } from "claude-code/testing";

import {
  applyArgv,
  bridgeArgv,
  contextSection,
  deltaSummaryLines,
  ownerArgv,
  ownerOf,
  packArgv,
  packFileArgv,
  packsSection,
  parseReport,
  planItemsArgv,
  relativePath,
  rememberOwner,
  renderArgv,
  reportArgv,
  taskCard,
  touchedPath,
  ARCH_TOOLS,
  ARCH_TOOL_PREFIX,
  archInvocation,
} from "./plan";

test("only a file tool's call names a file", () => {
  expect(touchedPath({ tool: "Read", file_path: "/tree/calc/calc.go" })).toBe("/tree/calc/calc.go");
  expect(touchedPath({ tool: "Edit", file_path: "calc/calc.go" })).toBe("calc/calc.go");
  expect(touchedPath({ tool: "Write", notebook_path: "calc/n.ipynb" })).toBe("calc/n.ipynb");
  expect(touchedPath({ tool: "Bash", command: "ls" })).toBeUndefined();
  expect(touchedPath({ tool: "Read" })).toBeUndefined();
});

test("a path is reported relative to the managed tree", () => {
  expect(relativePath("/work/tree/calc/calc.go", "/work/tree")).toBe("calc/calc.go");
  expect(relativePath("./calc/calc.go", "/work/tree")).toBe("calc/calc.go");
  expect(relativePath("/elsewhere/calc.go", "/work/tree")).toBe("/elsewhere/calc.go");
  expect(relativePath("/work/tree/calc/calc.go", "/work/tree/")).toBe("calc/calc.go");
});

test("the context document becomes one session section", () => {
  const section = contextSection("# Context: calc\n\nAdd adds two integers.\n");
  expect(section?.scope).toBe("session");
  expect(section?.text).toContain("Add adds two integers.");
  expect(contextSection("   \n")).toBeUndefined();
});

test("the injected section leads with the shared context skill", () => {
  const section = contextSection("# Context: calc\n", undefined, "# House rules\n");
  expect(section?.text.startsWith("# House rules")).toBe(true);
  expect(section?.text).toContain("# Context: calc");
  expect(contextSection("# Context: calc\n", undefined, "")?.text).not.toContain("# House rules");
});

test("the workspace is named explicitly, not left to the default", () => {
  const argv = bridgeArgv({ specctl: "/repo/bin/specctl", workspace: "root:tenant", namespace: "team" });
  expect(argv.specctl).toBe("/repo/bin/specctl");
  expect(argv.workspace).toBe("root:tenant");
  expect(argv.namespace).toBe("team");
  expect(bridgeArgv({}).workspace).toBe("root:specs");
});

test("every bridge call carries the workspace and the kubeconfig", () => {
  const env = { specctl: "specctl", workspace: "root:specs", namespace: "default", kubeconfig: "/tmp/k" };
  expect(renderArgv(env, "calc")).toEqual([
    "specctl", "clm", "render", "--context", "calc",
    "--workspace", "root:specs", "--namespace", "default", "--kubeconfig", "/tmp/k",
  ]);
  expect(applyArgv(env, "calc")[2]).toBe("apply");
  const report = reportArgv(env, "calc-s2c-abc", { turn: 2, tool: "Write", files: ["calc/calc.go"] });
  expect(report[2]).toBe("report");
  expect(report[4]).toBe("calc-s2c-abc");
  expect(JSON.parse(report[6] ?? "{}")).toEqual({ turn: 2, tool: "Write", files: ["calc/calc.go"] });
});

test("the report line is read, not guessed", () => {
  expect(parseReport("calc-s2c-abc progress=3 recorded=true\n")).toEqual({ progress: 3, recorded: true });
  expect(parseReport("calc-s2c-abc progress=1 recorded=false\n")).toEqual({ progress: 1, recorded: false });
  expect(parseReport("")).toEqual({ progress: 0, recorded: false });
});

test("the architecture tools map to specctl, and the edit carries the document on stdin", () => {
  expect(ARCH_TOOLS.map((tool) => tool.name)).toEqual([
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
  ]);
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_outline`, {})).toEqual({ argv: ["specctl", "arch", "outline"] });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_context`, { context: "calc" })).toEqual({
    argv: ["specctl", "clm", "render", "--context", "calc"],
  });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_edit`, { context: "calc", document: "# Context: calc\n" })).toEqual({
    argv: ["specctl", "clm", "apply", "--context", "calc"],
    stdin: "# Context: calc\n",
  });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_changes`, {})).toEqual({ argv: ["specctl", "get", "specchanges"] });
});

test("an architecture tool call without what it needs is answered, not run", () => {
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_context`, {})).toBe("string");
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_edit`, { context: "calc", document: "  " })).toBe("string");
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_nothing`, {})).toBe("string");
});

test("the plan and finding tools map to specctl", () => {
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_plans`, {})).toEqual({ argv: ["specctl", "plan", "ls"] });
  const plan = "# Plan: calc\n";
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_plan`, { document: plan })).toEqual({
    argv: ["specctl", "plan", "apply", "-f", "-"],
    stdin: plan,
  });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_findings`, {})).toEqual({ argv: ["specctl", "finding", "ls"] });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_release`, { context: "calc", unit: "Add" })).toEqual({
    argv: ["specctl", "arch", "release", "calc", "--unit", "Add"],
  });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_release`, { context: "calc", unit: "all" })).toEqual({
    argv: ["specctl", "arch", "release", "calc", "--all"],
  });
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_plan`, {})).toBe("string");
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_release`, { context: "calc" })).toBe("string");
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_release`, { unit: "all" })).toBe("string");
});

test("open plan items join the context section", () => {
  const items = planItemsArgv({ specctl: "specctl", workspace: "root:tenant", namespace: "team" }, "calc");
  expect(items.slice(0, 5)).toEqual(["specctl", "plan", "items", "--context", "calc"]);
  expect(items).toContain("--workspace");
  expect(items).toContain("root:tenant");
  expect(items).toContain("--namespace");
  expect(items).toContain("team");
  const section = contextSection("# Context: calc\n", "p.add-multiply  draft\n");
  expect(section?.text).toContain("## Open plan items for this context");
  expect(section?.text).toContain("p.add-multiply  draft");
  expect(contextSection("# Context: calc\n", "  \n")?.text).not.toContain("## Open plan items");
  expect(contextSection("# Context: calc\n", "")?.text).not.toContain("## Open plan items");
});

test("the mod asks which context owns the file it just touched", () => {
  const argv = ownerArgv({ specctl: "specctl", workspace: "root:tenant", namespace: "team" }, "/work/tree/calc/calc.go");
  expect(argv.slice(0, 6)).toEqual(["specctl", "arch", "owner", "/work/tree/calc/calc.go", "-o", "json"]);
  expect(ownerOf('{"path": "calc/calc.go", "context": "calc"}')).toBe("calc");
  expect(ownerOf('{"path": "docs/plan.md"}')).toBeUndefined();
  expect(ownerOf("no context owns docs/plan.md")).toBeUndefined();
});

test("the contexts in use are at most four, most recent first", () => {
  let recent: string[] = [];
  for (const context of ["a", "b", "c", "d", "e"]) recent = rememberOwner(recent, context);
  expect(recent).toEqual(["e", "d", "c", "b"]);
  expect(rememberOwner(recent, "c")).toEqual(["c", "e", "d", "b"]);
});

test("each context in use contributes its pack, and a session with no context its directory's", () => {
  const env = { specctl: "specctl", workspace: "root:specs", namespace: "default" };
  expect(packArgv(env, "calc").slice(0, 5)).toEqual(["specctl", "context", "pack", "--context", "calc"]);
  expect(packFileArgv(env, "/work/tree/impl/clm").slice(0, 5)).toEqual(["specctl", "context", "pack", "--file", "/work/tree/impl/clm"]);
  expect(packsSection(["pack one", " "])?.text).toBe("pack one");
  expect(packsSection([" "])).toBeUndefined();
});

test("a realize session carries a task card, an ordinary one does not", () => {
  const pack = "verify: `go build ./...`\nacceptance: `live: go test ./test/e2e`\n";
  const card = taskCard("calc-s2c-1", "calc", pack);
  expect(card?.text).toContain("change: calc-s2c-1");
  expect(card?.text).toContain("verify: `go build ./...`");
  expect(card?.text).toContain("acceptance: `live: go test ./test/e2e`");
  expect(card?.text).toContain("Do not run the repository's whole suite.");
  expect(taskCard("", "calc", pack)).toBeUndefined();
});

test("the owner, neighbor and search tools map to specctl", () => {
  const owner = archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_owner`, { path: "calc/calc.go" });
  expect(owner).toEqual({ argv: ["specctl", "arch", "owner", "calc/calc.go"] });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_neighbors`, { context: "calc" })).toEqual({
    argv: ["specctl", "clm", "neighbors", "calc"],
  });
  expect(archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_search`, { terms: "rate limit", limit: 4 })).toEqual({
    argv: ["specctl", "graph", "search", "rate limit", "--limit", "4"],
  });
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_owner`, {})).toBe("string");
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_neighbors`, {})).toBe("string");
  expect(typeof archInvocation("specctl", `${ARCH_TOOL_PREFIX}arch_search`, { terms: "  " })).toBe("string");
});

test("the delta summary keeps only the lines the apply prints by id", () => {
  const stderr = [
    "- r.multiply",
    "+ interface Subtract",
    "specctl clm apply: calc applied (+1 -1), queued behind calc-s2c-abc",
  ].join("\n");
  expect(deltaSummaryLines(stderr)).toBe("- r.multiply\n+ interface Subtract");
  expect(deltaSummaryLines("specctl clm apply: no change: the model zone says what the spec already says")).toBe("");
});
