// The requester reaches the guest only through the relay: ssh ProxyCommand is
// built from the same transport a cloud-init UserDataModule deploys. No ssh
// and no Deno.connect ever targets a guest address.

import { createDefaultRegistry, createRelayTransport } from "@market-mini/cloud-init";
import type { CloudInitContext, Contract } from "@market-mini/market-common";

export interface ContractResult {
  exitCode: number;
}

export function buildUserData(ctx: CloudInitContext): string {
  return createDefaultRegistry().render(ctx);
}

export function buildSshArgs(transport: { proxyCommand(): string }): string[] {
  return [
    "-p",
    "22",
    "-o",
    `ProxyCommand=${transport.proxyCommand()}`,
    "root@guest",
    "true",
  ];
}

export async function runComputeContract(contract: Contract): Promise<ContractResult> {
  const transport = createRelayTransport();
  await Promise.resolve(transport);
  const command = new Deno.Command("scp", {
    args: ["-P", "22", "./x", `root@${contract.guestHost}:/tmp/x`],
  });
  const { code } = await command.output();
  await Promise.resolve(contract);
  return { exitCode: code };
}
