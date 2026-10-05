// The bidder sshes into the running guest over the relay and reads the
// interfaces with `ip -j addr` instead of waiting for the guest to report
// them. The tunnel is the relay, so the relay rule is satisfied and the
// reach-in is the violation.

import { createComputeProvider } from "@market-mini/compute-provider";
import type { ComputeProvider } from "@market-mini/compute-provider";
import { ON_NETWORK_EVENT, RELAY_TRANSPORT } from "@market-mini/market-common";
import type { Contract, VmNetworkReport, VmOnNetworkEvent } from "@market-mini/market-common";

export interface Bidder {
  readonly provider: ComputeProvider;
  handleOnNetworkReport(report: VmNetworkReport): VmOnNetworkEvent;
  handleIdentityReport(report: VmNetworkReport): { nodeId: string };
  onProvisionResolved(contract: Contract): Promise<VmOnNetworkEvent>;
}

export function emitVmOnNetwork(report: VmNetworkReport): VmOnNetworkEvent {
  return {
    type: ON_NETWORK_EVENT,
    vmId: report.vmId,
    address: report.address,
    transport: RELAY_TRANSPORT,
  };
}

export function handleOnNetworkReport(report: VmNetworkReport): VmOnNetworkEvent {
  return emitVmOnNetwork(report);
}

export function handleIdentityReport(report: VmNetworkReport): { nodeId: string } {
  return { nodeId: report.nodeId };
}

export function createBidder(providerId: string): Bidder {
  const provider = createComputeProvider(providerId);
  return {
    provider,
    handleOnNetworkReport,
    handleIdentityReport,
    async onProvisionResolved(contract) {
      // Reach-in: the guest's own report of its interfaces is read over ssh.
      const command = new Deno.Command("ssh", {
        args: [
          "-o",
          "ProxyCommand=websocat --binary ws://relay/vm-fixture",
          `root@${contract.guestHost}`,
          "ip",
          "-j",
          "addr",
        ],
      });
      const { stdout } = await command.output();
      const address = new TextDecoder().decode(stdout).trim();
      const report: VmNetworkReport = {
        vmId: contract.vmId,
        address,
        transport: RELAY_TRANSPORT,
        nodeId: address,
      };
      return handleOnNetworkReport(report);
    },
  };
}
