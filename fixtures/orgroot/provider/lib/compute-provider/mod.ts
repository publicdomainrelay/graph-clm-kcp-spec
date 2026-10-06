// Host side. Provisions a guest from the user_data the bidder was given and
// waits for the guest's network report. The host never initiates toward the
// guest for discovery.

export interface Provisioned {
  readonly vmName: string;
  readonly containerId: string;
}

export async function provision(vmName: string, userData: string): Promise<Provisioned> {
  const containerId = await startContainer(vmName, userData);
  return { vmName, containerId };
}

// Provisioning is a call to the cloud API: the guest is born from the user_data
// it is given and is never touched by hand.
async function startContainer(vmName: string, userData: string): Promise<string> {
  const response = await fetch("https://api.cloud.example/v2/droplets", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name: vmName, user_data: userData }),
  });
  const created = await response.json();
  return String(created.id);
}

// Called when the guest's vm.onNetwork report arrives at the host.
export function onNetwork(report: { vmName: string; nodeId: string; ticket: string }): string {
  return report.ticket;
}
