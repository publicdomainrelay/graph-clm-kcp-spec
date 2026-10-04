// The mod's own test, run by `claude plugin test cc-clm-mod`. It exercises the
// decisions: which tool call touched which file, how the context document
// becomes a system section, and what argument vector the state bridge is called
// with. The host access itself (`$.process.run`, `$.fs`) is not reachable from
// the test environment, which is exactly why it lives in adapters.ts and not in
// a decision.

import { expect, test } from "claude-code/testing";

import {
  applyArgv,
  bridgeArgv,
  contextSection,
  parseReport,
  relativePath,
  renderArgv,
  reportArgv,
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
  expect(ARCH_TOOLS.map((tool) => tool.name)).toEqual(["arch_outline", "arch_context", "arch_edit", "arch_changes"]);
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
