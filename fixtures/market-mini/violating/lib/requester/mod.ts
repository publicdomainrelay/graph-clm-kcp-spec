// The requester dials the guest address directly. It opens TCP to the guest
// and runs ssh on its port, with no ProxyCommand and no relay in the path.

import type { Contract } from "@market-mini/market-common";

export interface ContractResult {
  exitCode: number;
}

export async function runComputeContract(contract: Contract): Promise<ContractResult> {
  const connection = await Deno.connect({
    hostname: contract.guestHost,
    port: contract.guestPort,
  });
  connection.close();

  const command = new Deno.Command("ssh", {
    args: ["-p", String(contract.guestPort), `root@${contract.guestHost}`, "true"],
  });
  const { code } = await command.output();
  return { exitCode: code };
}
