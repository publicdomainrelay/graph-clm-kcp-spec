import assert from "node:assert/strict";
import { test } from "node:test";
import { specctlBridge } from "../../clm/adapters-node/bridge.ts";
import type { CommandResult, RunOptions } from "../../clm/core/mod.ts";

function recorder(): { calls: string[][]; runner: { run(argv: readonly string[], options?: RunOptions): Promise<CommandResult> } } {
  const calls: string[][] = [];
  return {
    calls,
    runner: {
      run(argv) {
        calls.push([...argv]);
        return Promise.resolve({ exitCode: 0, stdout: "{}", stderr: "applied" });
      },
    },
  };
}

test("the bridge names the workspace the controller put the host in", async () => {
  const { calls, runner } = recorder();
  const bridge = specctlBridge({
    env: {
      SPECD_WORKSPACE: "root:specs-eval",
      SPECD_NAMESPACE: "default",
      KUBECONFIG: "/tmp/kubeconfig",
    },
    runner,
  });
  await bridge.apply("domain", "the model zone");
  assert.deepEqual(calls[0], [
    "specctl",
    "clm",
    "apply",
    "--context",
    "domain",
    "--workspace",
    "root:specs-eval",
    "--namespace",
    "default",
    "--kubeconfig",
    "/tmp/kubeconfig",
  ]);
});

test("a bridge with no workspace in the environment adds no flag", async () => {
  const { calls, runner } = recorder();
  const bridge = specctlBridge({ env: {}, runner });
  await bridge.render("domain");
  assert.deepEqual(calls[0], ["specctl", "clm", "render", "--context", "domain"]);
});

test("an explicit scope wins over the environment", async () => {
  const { calls, runner } = recorder();
  const bridge = specctlBridge({
    env: { SPECD_WORKSPACE: "root:other" },
    workspace: "root:chosen",
    runner,
  });
  await bridge.render("domain");
  assert.deepEqual(calls[0], ["specctl", "clm", "render", "--context", "domain", "--workspace", "root:chosen"]);
});
