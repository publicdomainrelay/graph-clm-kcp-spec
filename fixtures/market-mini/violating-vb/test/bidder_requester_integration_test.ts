// Drives the bidder and the requester together. The first test sshes to the
// guest address directly; the second test in the same file goes through the
// relay, so a classifier that reads the file instead of the call lends the
// first test the second test's ProxyCommand.

import { createBidder } from "@market-mini/hono-bidder";
import { runComputeContract } from "@market-mini/requester";
import type { Contract } from "@market-mini/market-common";

const contract: Contract = {
  vmId: "vm-fixture",
  providerId: "provider-local",
  guestHost: "10.0.0.7",
  guestPort: 2222,
};

Deno.test("the test sshes to the guest directly", async () => {
  const command = new Deno.Command("ssh", {
    args: ["-p", String(contract.guestPort), `root@${contract.guestHost}`, "true"],
  });
  await command.output();
});

Deno.test("the requester reaches the guest over the relay", async () => {
  const bidder = createBidder("provider-local");
  await bidder.provider.provision(contract);
  const event = bidder.handleOnNetworkReport({
    vmId: contract.vmId,
    address: "relay://vm-fixture",
    transport: "ws-relay",
    nodeId: "iroh-node-fixture",
  });
  if (event.address !== "relay://vm-fixture") {
    throw new Error("guest report was not used for the event");
  }
  const command = new Deno.Command("ssh", {
    args: ["-o", "ProxyCommand=websocat --binary ws://relay/vm-fixture", "root@guest", "true"],
  });
  await command.output();
  const result = await runComputeContract(contract);
  if (typeof result.exitCode !== "number") {
    throw new Error("requester returned no exit code");
  }
});
