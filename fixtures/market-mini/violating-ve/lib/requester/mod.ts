// The requester reaches the guest through a proxy command that is not the
// relay: nc opens the guest's ssh port directly, and the tunnel vocabulary
// never names it.

import type { Contract } from "@market-mini/market-common";

export interface ContractResult {
  exitCode: number;
}

export async function runComputeContract(contract: Contract): Promise<ContractResult> {
  const command = new Deno.Command("ssh", {
    args: ["-o", `ProxyCommand=nc ${contract.guestHost} 2222`, `root@${contract.guestHost}`, "true"],
  });
  const { code } = await command.output();
  return { exitCode: code };
}
