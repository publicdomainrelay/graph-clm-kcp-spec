// Drives the bidder and the requester together. The test bypasses the relay:
// it opens a TCP connection to the guest address and sshes to it directly, and
// the bidder emits vm.onNetwork by querying the guest's node id.

import { createBidder } from "@market-mini/hono-bidder";
import { runComputeContract } from "@market-mini/requester";
import type { Contract } from "@market-mini/market-common";

Deno.test("bidder and requester provision and report", async () => {
  const bidder = createBidder("provider-local");
  const contract: Contract = {
    vmId: "vm-fixture",
    providerId: "provider-local",
    guestHost: "10.0.0.7",
    guestPort: 2222,
  };

  await bidder.provider.provision(contract);

  const event = await bidder.onProvisionResolved(contract);
  if (typeof event.address !== "string") {
    throw new Error("bidder produced no address");
  }

  // Direct connection to the guest address, bypassing the relay.
  const connection = await Deno.connect({
    hostname: contract.guestHost,
    port: contract.guestPort,
  });
  connection.close();

  const command = new Deno.Command("ssh", {
    args: ["-p", String(contract.guestPort), `root@${contract.guestHost}`, "true"],
  });
  await command.output();

  const result = await runComputeContract(contract);
  if (typeof result.exitCode !== "number") {
    throw new Error("requester returned no exit code");
  }
});
