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

async function startContainer(vmName: string, userData: string): Promise<string> {
  const command = new Deno.Command("container", {
    args: ["run", "--name", vmName, "--user-data", userData],
  });
  const { stdout } = await command.output();
  return new TextDecoder().decode(stdout).trim();
}

// Called when the guest's vm.onNetwork report arrives at the host.
export function onNetwork(report: { vmName: string; nodeId: string; ticket: string }): string {
  return report.ticket;
}
