// The bidder registers providers. It emits vm.onNetwork only from the handler
// that receives the guest's inbound report. It never queries the guest for its
// address, so the emitter cannot reach getNodeId.

import { createComputeProvider } from "@market-mini/compute-provider";
import type { ComputeProvider } from "@market-mini/compute-provider";
import { ON_NETWORK_EVENT, RELAY_TRANSPORT } from "@market-mini/market-common";
import type { Contract, VmNetworkReport, VmOnNetworkEvent } from "@market-mini/market-common";

export interface Bidder {
  readonly provider: ComputeProvider;
  handleOnNetworkReport(report: VmNetworkReport): VmOnNetworkEvent;
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

// The guest calls POST /v1/on-network and this is the only source of the event.
export function handleOnNetworkReport(report: VmNetworkReport): VmOnNetworkEvent {
  return emitVmOnNetwork(report);
}

export function createBidder(providerId: string): Bidder {
  const provider = createComputeProvider(providerId);
  return {
    provider,
    handleOnNetworkReport,
    async onProvisionResolved(contract) {
      const address = await provider.whenGuestReports(contract.vmId);
      return emitVmOnNetwork({
        vmId: contract.vmId,
        address,
        transport: RELAY_TRANSPORT,
      });
    },
  };
}
