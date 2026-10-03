import { expect, test } from "claude-code/testing";

import {
  bashPaths,
  bashRelativeEscapes,
  guardDenial,
  guardedTool,
  insideRoot,
  joinLexical,
  place,
  systemPath,
  type PathResolver,
} from "./plan";

const ROOT = "/work/tree";

function resolver(paths: Record<string, string>): PathResolver {
  return async (path) => paths[path];
}

function insideMap(): Record<string, string> {
  return {
    [ROOT]: ROOT,
    [`${ROOT}/calc/calc.go`]: `${ROOT}/calc/calc.go`,
    [`${ROOT}/calc`]: `${ROOT}/calc`,
    [`${ROOT}/calc/new.go`]: `${ROOT}/calc/new.go`,
    [`${ROOT}/calc/`]: `${ROOT}/calc`,
    "/tmp": "/tmp",
    "/etc/passwd": "/etc/passwd",
    "/usr/bin/go": "/usr/bin/go",
    "/home/user/repo/fixtures/greet/scenarios/01-add-farewell.yaml":
      "/home/user/repo/fixtures/greet/scenarios/01-add-farewell.yaml",
  };
}

test("only the file tools carry a path the guard checks", () => {
  for (const tool of ["Read", "Write", "Edit", "MultiEdit", "NotebookEdit", "Grep", "Glob"]) {
    expect(guardedTool(tool)).toBe(true);
  }
  expect(guardedTool("Bash")).toBe(false);
  expect(guardedTool("Task")).toBe(false);
});

test("a path inside the root is allowed", async () => {
  const resolve = resolver(insideMap());
  expect(await guardDenial("Read", { tool: "Read", file_path: `${ROOT}/calc/calc.go` }, ROOT, resolve)).toBeUndefined();
  expect(await guardDenial("Write", { tool: "Write", file_path: `${ROOT}/calc/new.go` }, ROOT, resolve)).toBeUndefined();
  expect(await guardDenial("Glob", { tool: "Glob", path: ROOT }, ROOT, resolve)).toBeUndefined();
});

test("a path outside the root is refused", async () => {
  const resolve = resolver(insideMap());
  const denial = await guardDenial("Read", { tool: "Read", file_path: "/etc/passwd" }, ROOT, resolve);
  expect(denial).toContain("/etc/passwd");
  expect(denial).toContain(ROOT);
  expect(denial).toContain("refusing Read");
  const elsewhere = await guardDenial("Edit", { tool: "Edit", file_path: "/home/user/other.ts" }, ROOT, resolve);
  expect(elsewhere).toContain("outside the containment root");
});

test("the repository's own fixtures and scenarios are refused", async () => {
  const resolve = resolver(insideMap());
  const scenario = "/home/user/repo/fixtures/greet/scenarios/01-add-farewell.yaml";
  const denial = await guardDenial("Read", { tool: "Read", file_path: scenario }, ROOT, resolve);
  expect(denial).toContain(scenario);
  expect(denial).toContain("hidden acceptance tests");
});

test("a Write to a file that is not there yet is placed by its folder", async () => {
  const placed = await place(`${ROOT}/calc/new.go`, resolver(insideMap()));
  expect(placed).toBe(`${ROOT}/calc/new.go`);
  expect(await place("/nowhere/x.go", resolver({ "/nowhere": undefined as unknown as string }))).toBeUndefined();
});

test("the root must resolve before any path can be checked", async () => {
  const denial = await guardDenial("Read", { tool: "Read", file_path: `${ROOT}/a.go` }, ROOT, resolver({}));
  expect(denial).toContain("cannot be resolved");
});

test("a Bash command that stays inside passes", async () => {
  const resolve = resolver(insideMap());
  for (const command of [
    "go test ./...",
    "git status --short",
    "cat calc/calc.go",
    "cat /tmp/build.log",
    "ls /usr/bin",
    "go test ./... > /dev/null",
  ]) {
    expect(await guardDenial("Bash", { tool: "Bash", command }, ROOT, resolve)).toBeUndefined();
  }
});

test("a Bash command that names a path outside is refused", async () => {
  const resolve = resolver(insideMap());
  const scenario = "/home/user/repo/fixtures/greet/scenarios/01-add-farewell.yaml";
  const denial = await guardDenial("Bash", { tool: "Bash", command: `cat ${scenario}` }, ROOT, resolve);
  expect(denial).toContain(scenario);
  const escape = await guardDenial("Bash", { tool: "Bash", command: "cat ../../secrets.txt" }, ROOT, resolve);
  expect(escape).toContain("../../secrets.txt");
});

test("the shell scanner reads paths and leaves other tokens alone", () => {
  expect(bashPaths("cat /etc/hosts; ls /usr/bin | head")).toEqual(["/etc/hosts", "/usr/bin"]);
  expect(bashPaths("curl https://example.com/x")).toEqual([]);
  expect(bashPaths("sed -e 's/\\/a/b/'")).toEqual([]);
  expect(bashPaths("tool --file=/var/log/x")).toEqual(["/var/log/x"]);
  expect(bashPaths("cat ~/notes.txt")).toEqual(["~/notes.txt"]);
  expect(bashRelativeEscapes("cat ../a ../../b ./c")).toEqual(["../a", "../../b"]);
});

test("containment and the system list are lexical", () => {
  expect(insideRoot("/work/tree/a.go", "/work/tree")).toBe(true);
  expect(insideRoot("/work/tree", "/work/tree/")).toBe(true);
  expect(insideRoot("/work/treehouse/a.go", "/work/tree")).toBe(false);
  expect(systemPath("/usr/bin/go")).toBe(true);
  expect(systemPath("/usrlocal/x")).toBe(false);
  expect(joinLexical("/work/tree", "../secrets")).toBe("/work/secrets");
  expect(joinLexical("/work/tree", "calc/../calc/a.go")).toBe("/work/tree/calc/a.go");
});
