// Guest side. cloud-init user_data installs the relay subscriber; the guest
// then reports its own network information to the host, the host never asks.

export interface NetworkReport {
  readonly vmName: string;
  readonly nodeId: string;
  readonly ticket: string;
}

export function buildUserData(vmName: string, relayUrl: string): string {
  return [
    "#cloud-config",
    "runcmd:",
    `  - relay-subscriber --relay ${relayUrl} --name ${vmName}`,
    `  - report-network --event vm.onNetwork --name ${vmName}`,
  ].join("\n");
}

// The guest announces itself once the subscriber is up.
export async function reportNetwork(report: NetworkReport, hostUrl: string): Promise<void> {
  await fetch(`${hostUrl}/xrpc/vm.onNetwork`, {
    method: "POST",
    body: JSON.stringify(report),
  });
}
