// The compute provider boots a guest from the cloud-init user_data and awaits
// the guest's own report. getNodeId is an agent query into the running guest;
// it is the reach-in the P-guest-reports policy forbids on the emit path.

import type { Contract } from "@market-mini/market-common";

export interface ComputeProvider {
  readonly providerId: string;
  provision(contract: Contract): Promise<void>;
  whenGuestReports(vmId: string): Promise<string>;
  getNodeId(providerId: string): Promise<string>;
}

export function createComputeProvider(providerId: string): ComputeProvider {
  const waiting = new Map<string, (address: string) => void>();

  return {
    providerId,

    async provision(contract) {
      await Promise.resolve(contract);
    },

    whenGuestReports(vmId) {
      return new Promise<string>((resolve) => {
        waiting.set(vmId, resolve);
      });
    },

    async getNodeId(_providerId) {
      // Dial the guest agent port and read the node id back. The provider is
      // asking the guest, not the guest reporting.
      const connection = await Deno.connect({ hostname: "127.0.0.1", port: 9876 });
      connection.close();
      return "iroh-node-fixture";
    },
  };
}
