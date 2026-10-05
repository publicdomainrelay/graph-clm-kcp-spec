// Shared wire types for the fixture. Pure values, no I/O.

export const RELAY_TRANSPORT = "ws-relay";

export const ON_NETWORK_EVENT = "vm.onNetwork";

export const REGISTER_IDENTITY_EVENT = "vm.registerIdentity";

export const ON_NETWORK_REPORT_PATH = "/v1/on-network";

export interface CloudInitContext {
  vmName: string;
  relaySubdomain: string;
  sshAuthorizedKey: string;
  reportUrl: string;
}

export interface Contract {
  vmId: string;
  providerId: string;
  guestHost: string;
  guestPort: number;
}

// The guest reaches out and reports this. The bidder never builds it itself.
export interface VmNetworkReport {
  vmId: string;
  address: string;
  transport: string;
  nodeId: string;
}

export interface VmOnNetworkEvent {
  type: typeof ON_NETWORK_EVENT;
  vmId: string;
  address: string;
  transport: string;
}

export interface VmIdentityEvent {
  type: typeof REGISTER_IDENTITY_EVENT;
  vmId: string;
  nodeId: string;
}

export function relayServiceName(vmId: string): string {
  return vmId.replace(/[.:]/g, "-");
}
