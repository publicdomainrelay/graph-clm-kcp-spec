// Drives the bidder and the requester together. Every ssh goes through the
// relay: the requester builds ProxyCommand from the transport the RFP
// cloud-init deploys. This test never dials a guest address directly.

import { createBidder } from "@market-mini/hono-bidder";
import { runComputeContract } from "@market-mini/requester";
import type { Contract } from "@market-mini/market-common";

Deno.test("bidder and requester provision over the relay", async () => {
  const bidder = createBidder("provider-local");
  const contract: Contract = {
    vmId: "vm-fixture",
    providerId: "provider-local",
    guestHost: "10.0.0.7",
    guestPort: 2222,
  };

  await bidder.provider.provision(contract);

  const report = {
    vmId: contract.vmId,
    address: "relay://vm-fixture",
    transport: "ws-relay",
    nodeId: "iroh-node-fixture",
  };

  const event = bidder.handleOnNetworkReport(report);
  if (event.address !== "relay://vm-fixture") {
    throw new Error("guest report was not used for the event");
  }

  const identity = bidder.handleIdentityReport(report);
  if (identity.nodeId !== "iroh-node-fixture") {
    throw new Error("guest report was not used for the identity");
  }

  const result = await runComputeContract(contract);
  if (typeof result.exitCode !== "number") {
    throw new Error("requester returned no exit code");
  }
});
