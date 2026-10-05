// The bidder registers providers. When a provisioning resolves it reaches into
// the guest with computeProvider.getNodeId and emits vm.onNetwork from that
// path, so the emitter's reachable set includes the agent query. There is no
// inbound guest report handler.

import { createComputeProvider } from "@market-mini/compute-provider";
import type { ComputeProvider } from "@market-mini/compute-provider";
import { ON_NETWORK_EVENT, RELAY_TRANSPORT } from "@market-mini/market-common";
import type { Contract, VmOnNetworkEvent } from "@market-mini/market-common";

export interface Bidder {
  readonly provider: ComputeProvider;
  onProvisionResolved(contract: Contract): Promise<VmOnNetworkEvent>;
}

export async function emitVmOnNetwork(
  provider: ComputeProvider,
  providerId: string,
  vmId: string,
): Promise<VmOnNetworkEvent> {
  // Reach-in: the provider queries the guest agent for its node id.
  const nodeId = await provider.getNodeId(providerId);
  return {
    type: ON_NETWORK_EVENT,
    vmId,
    address: nodeId,
    transport: RELAY_TRANSPORT,
  };
}

export function createBidder(providerId: string): Bidder {
  const provider = createComputeProvider(providerId);
  return {
    provider,
    async onProvisionResolved(contract) {
      return emitVmOnNetwork(provider, contract.providerId, contract.vmId);
    },
  };
}
