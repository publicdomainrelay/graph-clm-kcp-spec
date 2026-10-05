// The bidder registers providers. It emits vm.onNetwork and
// vm.registerIdentity only from the handlers that receive the guest's inbound
// report. It never queries the guest for its address or its node id, so the
// emitters cannot reach getNodeId.

import { createComputeProvider } from "@market-mini/compute-provider";
import type { ComputeProvider } from "@market-mini/compute-provider";
import {
  ON_NETWORK_EVENT,
  REGISTER_IDENTITY_EVENT,
  RELAY_TRANSPORT,
} from "@market-mini/market-common";
import type {
  Contract,
  VmIdentityEvent,
  VmNetworkReport,
  VmOnNetworkEvent,
} from "@market-mini/market-common";

export interface Bidder {
  readonly provider: ComputeProvider;
  handleOnNetworkReport(report: VmNetworkReport): VmOnNetworkEvent;
  handleIdentityReport(report: VmNetworkReport): VmIdentityEvent;
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

export function emitRegisterIdentity(report: VmNetworkReport): VmIdentityEvent {
  return {
    type: REGISTER_IDENTITY_EVENT,
    vmId: report.vmId,
    nodeId: report.nodeId,
  };
}

// The guest calls POST /v1/on-network and these are the only sources of the
// events.
export function handleOnNetworkReport(report: VmNetworkReport): VmOnNetworkEvent {
  return emitVmOnNetwork(report);
}

export function handleIdentityReport(report: VmNetworkReport): VmIdentityEvent {
  return emitRegisterIdentity(report);
}

export function createBidder(providerId: string): Bidder {
  const provider = createComputeProvider(providerId);
  return {
    provider,
    handleOnNetworkReport,
    handleIdentityReport,
    async onProvisionResolved(contract) {
      const leases = await Deno.readTextFile("/var/lib/libvirt/dnsmasq/virbr0.status");
      const address = JSON.parse(leases)[0]["ip-address"];
      const report: VmNetworkReport = {
        vmId: contract.vmId,
        address,
        transport: RELAY_TRANSPORT,
        nodeId: address,
      };
      handleIdentityReport(report);
      return handleOnNetworkReport(report);
    },
  };
}
