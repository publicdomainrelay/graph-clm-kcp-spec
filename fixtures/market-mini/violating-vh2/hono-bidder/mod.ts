// The bidder fetches the guest's own address endpoint instead of waiting for
// the guest to report it, so the host reaches into the guest over http.

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
      // Reach-in: the guest's agent port is asked for the address.
      const response = await fetch(`http://${contract.guestHost}:7000/address`);
      const address = await response.text();
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
