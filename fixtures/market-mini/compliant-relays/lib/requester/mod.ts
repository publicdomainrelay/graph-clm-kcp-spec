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
  const command = new Deno.Command("ssh", { args: buildSshArgs(transport) });
  const { code } = await command.output();
  await Promise.resolve(contract);
  return { exitCode: code };
}

export function relayViaJumpHost(guest: string): Deno.Command {
  return new Deno.Command("ssh", {
    args: ["-J", "relay.example.org", `root@${guest}`, "true"],
  });
}

export function relayViaProxyJump(guest: string): Deno.Command {
  return new Deno.Command("ssh", {
    args: ["-o", "ProxyJump=relay.example.org", `root@${guest}`, "true"],
  });
}

export function relayViaSocksHop(guest: string): Deno.Command {
  return new Deno.Command("ssh", {
    args: ["-o", "ProxyCommand=nc -X 5 -x 127.0.0.1:1080 %h %p", `root@${guest}`, "true"],
  });
}

export function relayViaJumpCommand(guest: string): Deno.Command {
  return new Deno.Command("ssh", {
    args: ["-o", "ProxyCommand=ssh -W %h:%p jumphost.example.org", `root@${guest}`, "true"],
  });
}

export function relayViaConfigAlias(): Deno.Command {
  return new Deno.Command("ssh", {
    args: ["-F", "./test/ssh_config", "guest-vm", "true"],
  });
}

export function sshToGitHub(): Deno.Command {
  return new Deno.Command("ssh", {
    args: ["-T", "git@github.com"],
  });
}
